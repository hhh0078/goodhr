// 文件作用：持久化账号私有消息，事务同步未读数量、递增版本和幂等凭据。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// PostgresMessageStore 以用户状态行锁串行化同一账号的消息变化。
type PostgresMessageStore struct{ db *sql.DB }

// NewPostgresMessageStore 创建PostgreSQL消息存储。
func NewPostgresMessageStore(db *sql.DB) *PostgresMessageStore { return &PostgresMessageStore{db: db} }

// messageTxUser 确认账号并锁定状态行，使新增、已读和版本变化保持原子性。
func messageTxUser(ctx context.Context, tx *sql.Tx, email string) (string, MessageSummary, error) {
	var id string
	var summary MessageSummary
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE email=$1`, email).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return "", summary, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_message_state(user_id) VALUES($1::uuid) ON CONFLICT(user_id) DO NOTHING`, id); err != nil {
		return "", summary, err
	}
	err := tx.QueryRowContext(ctx, `SELECT unread_count,version FROM user_message_state WHERE user_id=$1::uuid FOR UPDATE`, id).Scan(&summary.UnreadCount, &summary.Version)
	return id, summary, err
}

// scanUserMessage 按固定字段扫描消息，保留未读状态的空时间。
func scanUserMessage(row interface{ Scan(...any) error }) (UserMessage, error) {
	var message UserMessage
	var readAt sql.NullTime
	err := row.Scan(&message.ID, &message.Title, &message.Body, &message.Type, &message.ActionURL, &message.CreatedAt, &readAt)
	if readAt.Valid {
		message.ReadAt = &readAt.Time
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return message, err
}

// Push 在同一事务写入消息、幂等凭据及未读计数，重复请求返回首次结果。
func (s *PostgresMessageStore) Push(email string, input MessageInput) (MessagePushResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// 复用账号创建能力，内存会话在开发或测试环境也可补齐用户行。
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return MessagePushResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MessagePushResult{}, err
	}
	defer tx.Rollback()
	userID, summary, err := messageTxUser(ctx, tx, email)
	if err != nil {
		return MessagePushResult{}, err
	}
	fingerprint := messageFingerprint(input)
	var existingID, existingFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT message_id::text,fingerprint FROM user_message_receipts WHERE user_id=$1::uuid AND idempotency_key=$2`, userID, input.IdempotencyKey).Scan(&existingID, &existingFingerprint)
	if err == nil {
		if existingFingerprint != fingerprint {
			return MessagePushResult{}, ErrMessageIdempotency
		}
		message, readErr := scanUserMessage(tx.QueryRowContext(ctx, `SELECT id::text,title,body,message_type,action_url,created_at,read_at FROM messages WHERE user_id=$1::uuid AND id=$2::uuid`, userID, existingID))
		if errors.Is(readErr, ErrNotFound) {
			message = UserMessage{ID: existingID}
		} else if readErr != nil {
			return MessagePushResult{}, readErr
		}
		if err = tx.Commit(); err != nil {
			return MessagePushResult{}, err
		}
		return MessagePushResult{Message: message, Summary: summary}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MessagePushResult{}, err
	}
	message, err := scanUserMessage(tx.QueryRowContext(ctx, `INSERT INTO messages(user_id,title,body,message_type,action_url) VALUES($1::uuid,$2,$3,$4,$5) RETURNING id::text,title,body,message_type,action_url,created_at,read_at`, userID, input.Title, input.Body, input.Type, input.ActionURL))
	if err != nil {
		return MessagePushResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_message_receipts(user_id,idempotency_key,fingerprint,message_id) VALUES($1::uuid,$2,$3,$4::uuid)`, userID, input.IdempotencyKey, fingerprint, message.ID); err != nil {
		return MessagePushResult{}, err
	}
	err = tx.QueryRowContext(ctx, `UPDATE user_message_state SET unread_count=unread_count+1,version=version+1,updated_at=now() WHERE user_id=$1::uuid RETURNING unread_count,version`, userID).Scan(&summary.UnreadCount, &summary.Version)
	if err != nil {
		return MessagePushResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return MessagePushResult{}, err
	}
	return MessagePushResult{Message: message, Created: true, Summary: summary}, nil
}

// Summary 只查账号计数行，消息正文和历史行不参与轮询。
func (s *PostgresMessageStore) Summary(email string) (MessageSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var summary MessageSummary
	err := s.db.QueryRowContext(ctx, `SELECT ms.unread_count,ms.version FROM user_message_state ms JOIN users u ON u.id=ms.user_id WHERE u.email=$1`, email).Scan(&summary.UnreadCount, &summary.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return summary, nil
	}
	return summary, err
}

// List 从单次查询得到标题分页和总数，不把正文加载进列表响应。
func (s *PostgresMessageStore) List(email string, page, size int) (MessageList, error) {
	page, size = messagePagination(page, size)
	result := MessageList{Messages: []UserMessage{}, Page: page, PageSize: size}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `WITH owned AS (SELECT m.id,m.title,m.message_type,m.action_url,m.created_at,m.read_at FROM messages m JOIN users u ON u.id=m.user_id WHERE u.email=$1), paged AS (SELECT * FROM owned ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3) SELECT counts.total,p.id::text,p.title,p.message_type,p.action_url,p.created_at,p.read_at FROM (SELECT count(*) AS total FROM owned) counts LEFT JOIN paged p ON true ORDER BY p.created_at DESC,p.id DESC`, email, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title, kind, url sql.NullString
		var created, read sql.NullTime
		if err = rows.Scan(&result.Total, &id, &title, &kind, &url, &created, &read); err != nil {
			return result, err
		}
		if !id.Valid {
			continue
		}
		message := UserMessage{ID: id.String, Title: title.String, Type: kind.String, ActionURL: url.String, CreatedAt: created.Time}
		if read.Valid {
			message.ReadAt = &read.Time
		}
		result.Messages = append(result.Messages, message)
	}
	return result, rows.Err()
}

// Read 通过账号及消息编号查询，首次已读和计数更新在同一事务完成。
func (s *PostgresMessageStore) Read(email, id string) (UserMessage, MessageSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserMessage{}, MessageSummary{}, err
	}
	defer tx.Rollback()
	userID, summary, err := messageTxUser(ctx, tx, email)
	if err != nil {
		return UserMessage{}, summary, err
	}
	message, err := scanUserMessage(tx.QueryRowContext(ctx, `SELECT id::text,title,body,message_type,action_url,created_at,read_at FROM messages WHERE user_id=$1::uuid AND id::text=$2`, userID, id))
	if err != nil {
		return UserMessage{}, summary, err
	}
	if message.ReadAt == nil {
		var readAt time.Time
		err = tx.QueryRowContext(ctx, `UPDATE messages SET read_at=now() WHERE user_id=$1::uuid AND id::text=$2 RETURNING read_at`, userID, id).Scan(&readAt)
		if err != nil {
			return UserMessage{}, summary, err
		}
		message.ReadAt = &readAt
		err = tx.QueryRowContext(ctx, `UPDATE user_message_state SET unread_count=GREATEST(unread_count-1,0),version=version+1,updated_at=now() WHERE user_id=$1::uuid RETURNING unread_count,version`, userID).Scan(&summary.UnreadCount, &summary.Version)
		if err != nil {
			return UserMessage{}, summary, err
		}
	}
	if err = tx.Commit(); err != nil {
		return UserMessage{}, summary, err
	}
	return message, summary, nil
}

// CleanupRead 删除截止时间之前的已读正文，并返回受影响账号的新版本供缓存刷新。
func (s *PostgresMessageStore) CleanupRead(cutoff time.Time) ([]MessageUserSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 与推送和已读统一先锁账号状态；多账号清理固定排序，避免两个清理任务交叉等待。
	locked, err := tx.QueryContext(ctx, `SELECT ms.user_id::text FROM user_message_state ms WHERE EXISTS(SELECT 1 FROM messages m WHERE m.user_id=ms.user_id AND m.read_at IS NOT NULL AND m.read_at <= $1) ORDER BY ms.user_id FOR UPDATE OF ms`, cutoff)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for locked.Next() {
		var id string
		if err = locked.Scan(&id); err != nil {
			locked.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = locked.Err()
	locked.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []MessageUserSummary{}, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `WITH deleted AS (DELETE FROM messages WHERE user_id=ANY($2::uuid[]) AND read_at IS NOT NULL AND read_at <= $1 RETURNING user_id), changed AS (UPDATE user_message_state SET version=version+1,updated_at=now() WHERE user_id IN (SELECT user_id FROM deleted) RETURNING user_id,unread_count,version) SELECT u.email,c.unread_count,c.version FROM changed c JOIN users u ON u.id=c.user_id`, cutoff, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	result := []MessageUserSummary{}
	for rows.Next() {
		var item MessageUserSummary
		if err = rows.Scan(&item.UserEmail, &item.Summary.UnreadCount, &item.Summary.Version); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
