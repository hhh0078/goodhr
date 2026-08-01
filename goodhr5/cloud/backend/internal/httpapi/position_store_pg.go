// 本文件负责提供岗位配置的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PostgresPositionStore 使用 PostgreSQL 持久化岗位配置。
type PostgresPositionStore struct {
	db *sql.DB
}

// NewPostgresPositionStore 创建 PostgreSQL 岗位配置存储。
func NewPostgresPositionStore(db *sql.DB) *PostgresPositionStore {
	return &PostgresPositionStore{db: db}
}

// ListPositions 列出 PostgreSQL 中当前用户的岗位配置。
func (s *PostgresPositionStore) ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT p.id, COALESCE(p.platform_id, 'boss'), p.name, p.keywords, p.exclude_keywords, p.description, p.greet_message, p.is_and_mode,
		       p.common_config, p.ai_config, p.keyword_config, p.match_limit, p.status, p.scanned_count,
		       p.daily_greeted_count, p.daily_greeted_date::text, p.skipped_count, p.failed_count, p.enable_sound, p.enable_thinking,
		       p.created_at, p.updated_at, p.started_at, p.finished_at
		FROM positions p
		INNER JOIN users u ON u.id = p.user_id
		WHERE u.email = $1
		ORDER BY p.updated_at DESC, p.created_at DESC
		`,
		userEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Position, 0)
	for rows.Next() {
		var item Position
		var keywordsJSON []byte
		var excludeKeywordsJSON []byte
		var commonConfigJSON []byte
		var aiConfigJSON []byte
		var keywordConfigJSON []byte
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.PlatformID,
			&item.Name,
			&keywordsJSON,
			&excludeKeywordsJSON,
			&item.Description,
			&item.GreetMessage,
			&item.IsAndMode,
			&commonConfigJSON,
			&aiConfigJSON,
			&keywordConfigJSON,
			&item.MatchLimit,
			&item.Status,
			&item.ScannedCount,
			&item.DailyGreetedCount,
			&item.DailyGreetedDate,
			&item.SkippedCount,
			&item.FailedCount,
			&item.EnableSound,
			&item.EnableThinking,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.StartedAt,
			&item.FinishedAt,
		); err != nil {
			return nil, err
		}
		if err := decodeStringArray(keywordsJSON, &item.Keywords); err != nil {
			return nil, err
		}
		if err := decodeStringArray(excludeKeywordsJSON, &item.ExcludeKeywords); err != nil {
			return nil, err
		}
		if err := decodeObject(commonConfigJSON, &item.CommonConfig); err != nil {
			return nil, err
		}
		if err := decodeObject(aiConfigJSON, &item.AIConfig); err != nil {
			return nil, err
		}
		if err := decodeObject(keywordConfigJSON, &item.KeywordConfig); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SavePosition 保存 PostgreSQL 中的岗位配置。
func (s *PostgresPositionStore) SavePosition(position Position) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, position.UserEmail)
	if err != nil {
		return Position{}, err
	}

	keywordsJSON, err := json.Marshal(position.Keywords)
	if err != nil {
		return Position{}, err
	}
	excludeKeywordsJSON, err := json.Marshal(position.ExcludeKeywords)
	if err != nil {
		return Position{}, err
	}
	commonConfigJSON, err := json.Marshal(nonNilMap(position.CommonConfig))
	if err != nil {
		return Position{}, err
	}
	aiConfigJSON, err := json.Marshal(nonNilMap(position.AIConfig))
	if err != nil {
		return Position{}, err
	}
	keywordConfigJSON, err := json.Marshal(nonNilMap(position.KeywordConfig))
	if err != nil {
		return Position{}, err
	}

	var saved Position
	saved.UserEmail = position.UserEmail
	var row *sql.Row
	if position.ID == "" {
		row = s.db.QueryRowContext(
			ctx,
			`
			INSERT INTO positions (user_id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config, match_limit, enable_sound, enable_thinking)
			VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, $9::jsonb, $10::jsonb, $11::jsonb, $12, $13, $14)
			RETURNING id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config,
			          match_limit, status, scanned_count, daily_greeted_count, daily_greeted_date::text, skipped_count, failed_count, enable_sound, enable_thinking,
			          created_at, updated_at, started_at, finished_at
			`,
			userID,
			position.PlatformID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
			string(commonConfigJSON),
			string(aiConfigJSON),
			string(keywordConfigJSON),
			position.MatchLimit,
			position.EnableSound,
			position.EnableThinking,
		)
	} else {
		row = s.db.QueryRowContext(
			ctx,
			`
			UPDATE positions
			SET
				platform_id = $3,
				name = $4,
				keywords = $5::jsonb,
				exclude_keywords = $6::jsonb,
				description = $7,
				greet_message = $8,
				is_and_mode = $9,
				common_config = $10::jsonb,
				ai_config = $11::jsonb,
				keyword_config = $12::jsonb,
				match_limit = $13,
				enable_sound = $14,
				enable_thinking = $15,
				updated_at = now()
			WHERE id = $1 AND user_id = $2
			RETURNING id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config,
			          match_limit, status, scanned_count, daily_greeted_count, daily_greeted_date::text, skipped_count, failed_count, enable_sound, enable_thinking,
			          created_at, updated_at, started_at, finished_at
			`,
			position.ID,
			userID,
			position.PlatformID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
			string(commonConfigJSON),
			string(aiConfigJSON),
			string(keywordConfigJSON),
			position.MatchLimit,
			position.EnableSound,
			position.EnableThinking,
		)
	}

	var savedKeywordsJSON []byte
	var savedExcludeKeywordsJSON []byte
	var savedCommonConfigJSON []byte
	var savedAIConfigJSON []byte
	var savedKeywordConfigJSON []byte
	err = row.Scan(
		&saved.ID,
		&saved.PlatformID,
		&saved.Name,
		&savedKeywordsJSON,
		&savedExcludeKeywordsJSON,
		&saved.Description,
		&saved.GreetMessage,
		&saved.IsAndMode,
		&savedCommonConfigJSON,
		&savedAIConfigJSON,
		&savedKeywordConfigJSON,
		&saved.MatchLimit,
		&saved.Status,
		&saved.ScannedCount,
		&saved.DailyGreetedCount,
		&saved.DailyGreetedDate,
		&saved.SkippedCount,
		&saved.FailedCount,
		&saved.EnableSound,
		&saved.EnableThinking,
		&saved.CreatedAt,
		&saved.UpdatedAt,
		&saved.StartedAt,
		&saved.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}

	if err := decodeStringArray(savedKeywordsJSON, &saved.Keywords); err != nil {
		return Position{}, err
	}
	if err := decodeStringArray(savedExcludeKeywordsJSON, &saved.ExcludeKeywords); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedCommonConfigJSON, &saved.CommonConfig); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedAIConfigJSON, &saved.AIConfig); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedKeywordConfigJSON, &saved.KeywordConfig); err != nil {
		return Position{}, err
	}
	return saved, nil
}

// PositionByID 读取 PostgreSQL 中当前用户的单个岗位配置。
func (s *PostgresPositionStore) PositionByID(tenantID, userEmail, positionID string, isAdmin bool) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item Position
	var rawKeywords, rawExclude []byte
	var rawCommonConfig, rawAIConfig, rawKeywordConfig []byte
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT p.id, COALESCE(p.platform_id, 'boss'), p.name, CAST(p.keywords AS text), CAST(p.exclude_keywords AS text),
		       p.description, p.greet_message, p.is_and_mode, CAST(p.common_config AS text), CAST(p.ai_config AS text), CAST(p.keyword_config AS text),
		       p.match_limit, p.status, p.scanned_count, p.daily_greeted_count, p.daily_greeted_date::text,
		       p.skipped_count, p.failed_count, p.enable_sound, p.enable_thinking, p.created_at, p.updated_at, p.started_at, p.finished_at
		FROM positions p
		JOIN users u ON p.user_id = u.id
		WHERE u.email = $1 AND p.id = $2
		`,
		userEmail, positionID,
	).Scan(
		&item.ID, &item.PlatformID, &item.Name, &rawKeywords, &rawExclude,
		&item.Description, &item.GreetMessage, &item.IsAndMode,
		&rawCommonConfig, &rawAIConfig, &rawKeywordConfig,
		&item.MatchLimit, &item.Status, &item.ScannedCount,
		&item.DailyGreetedCount, &item.DailyGreetedDate, &item.SkippedCount, &item.FailedCount,
		&item.EnableSound, &item.EnableThinking, &item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.FinishedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}

	item.UserEmail = userEmail
	_ = decodeStringArray(rawKeywords, &item.Keywords)
	_ = decodeStringArray(rawExclude, &item.ExcludeKeywords)
	_ = decodeObject(rawCommonConfig, &item.CommonConfig)
	_ = decodeObject(rawAIConfig, &item.AIConfig)
	_ = decodeObject(rawKeywordConfig, &item.KeywordConfig)
	return item, nil
}

// UpdatePositionStatus 更新 PostgreSQL 岗位当前运行状态。
// positionID 为岗位 ID，status 为新的运行状态。
func (s *PostgresPositionStore) UpdatePositionStatus(positionID, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE positions
		SET status=$2,
		    started_at=CASE WHEN $2='running' THEN now() ELSE started_at END,
		    finished_at=CASE WHEN $2 IN ('completed','stopped','failed') THEN now() WHEN $2='running' THEN NULL ELSE finished_at END,
		    updated_at=now()
		WHERE id=$1`, positionID, status)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimPositionStart 在账号级事务锁内检查运行冲突并把目标岗位原子更新为运行中。
// userEmail 为岗位所属账号，positionID 为本次申请启动的岗位编号。
func (s *PostgresPositionStore) ClaimPositionStart(userEmail, positionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, userEmail); err != nil {
		return err
	}
	var runningID string
	err = tx.QueryRowContext(ctx, `
		SELECT p.id
		FROM positions p
		INNER JOIN users u ON u.id = p.user_id
		WHERE u.email=$1 AND p.status='running'
		LIMIT 1`, userEmail).Scan(&runningID)
	if err == nil {
		return ErrPositionAlreadyRunning
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE positions p
		SET status='running', started_at=now(), finished_at=NULL, updated_at=now()
		FROM users u
		WHERE p.id=$1 AND p.user_id=u.id AND u.email=$2`, positionID, userEmail)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// FinishPositionRun 幂等保存 PostgreSQL 岗位结束状态并累加本次打招呼数量。
// positionID 为岗位 ID，status 为结束状态，greeted 为本次打招呼数量。
func (s *PostgresPositionStore) FinishPositionRun(positionID, status string, greeted int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	today := positionBusinessDate(time.Now())
	result, err := s.db.ExecContext(ctx, `
		UPDATE positions
		SET daily_greeted_count=CASE
		        WHEN status=$2 THEN daily_greeted_count
		        WHEN daily_greeted_date=$4::date THEN daily_greeted_count+GREATEST(0,$3)
		        ELSE GREATEST(0,$3)
		    END,
		    daily_greeted_date=CASE WHEN status=$2 THEN daily_greeted_date ELSE $4::date END,
		    status=$2, finished_at=now(), updated_at=now()
		WHERE id=$1`, positionID, status, greeted, today)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementPositionCounts 累加 PostgreSQL 岗位统计。
// positionID 为岗位 ID，其余参数为本次新增的扫描、跳过和失败数量。
func (s *PostgresPositionStore) IncrementPositionCounts(positionID string, scanned, skipped, failed int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE positions
		SET scanned_count=scanned_count+GREATEST(0,$2),
		    skipped_count=skipped_count+GREATEST(0,$3), failed_count=failed_count+GREATEST(0,$4),
		    updated_at=now()
		WHERE id=$1`, positionID, scanned, skipped, failed)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// SyncPositionCounts 按本地累计值同步 PostgreSQL 岗位统计。
// positionID 为岗位 ID，其余参数为累计扫描、跳过和失败数量。
func (s *PostgresPositionStore) SyncPositionCounts(positionID string, scanned, skipped, failed int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `
		UPDATE positions
		SET scanned_count=GREATEST(scanned_count,$2),
		    skipped_count=GREATEST(skipped_count,$3), failed_count=GREATEST(failed_count,$4),
		    updated_at=now()
		WHERE id=$1`, positionID, scanned, skipped, failed)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// TodayGreetedTotal 汇总 PostgreSQL 所有岗位今天的打招呼数量。
func (s *PostgresPositionStore) TodayGreetedTotal() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var total int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(daily_greeted_count),0)::int FROM positions WHERE daily_greeted_date=$1::date`, positionBusinessDate(time.Now())).Scan(&total)
	return total, err
}

// DeletePosition 删除 PostgreSQL 中当前用户的岗位配置。
func (s *PostgresPositionStore) DeletePosition(userEmail string, positionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := s.db.ExecContext(
		ctx,
		`
		DELETE FROM positions p
		USING users u
		WHERE p.user_id = u.id
		  AND u.email = $1
		  AND p.id = $2
		`,
		userEmail,
		positionID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// decodeStringArray 解析数据库里的 JSON 字符串数组。
func decodeStringArray(value []byte, target *[]string) error {
	if len(value) == 0 {
		*target = []string{}
		return nil
	}
	return json.Unmarshal(value, target)
}

func decodeObject(value []byte, target *map[string]any) error {
	if len(value) == 0 {
		*target = map[string]any{}
		return nil
	}
	return json.Unmarshal(value, target)
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
