// Package storage 本文件提供自动回复实体行的事务读写，所有查询限定已认证账号且使用索引。
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AutoReplyRow 是单条本地业务实体，不包含整个账号的快照。
type AutoReplyRow struct {
	ID       string
	Data     json.RawMessage
	Revision int64
}

// AutoReplyFilter 是经过白名单校验的等值索引条件。
type AutoReplyFilter struct{ Field, Value string }

// AutoReplyTx 在一个短事务中访问指定账号的实体；不得在回调内请求网络。
type AutoReplyTx struct {
	tx    *sql.Tx
	ctx   context.Context
	owner string
}

var autoReplyFields = map[string]string{
	"proposal_receipts": "status", "candidates": "phone", "identities": "platform_id account_id platform_candidate_id candidate_id", "conversations": "platform_id account_id thread_id identity_id candidate_id position_id", "messages": "conversation_id message_key direction", "confirmations": "candidate_id conversation_id position_id source_type condition_id", "attachments": "candidate_id conversation_id", "runs": "conversation_id", "tools": "run_id", "suggestions": "conversation_id position_id", "reports": "", "recommendations": "status", "outbox": "status", "cloud_links": "", "sends": "conversation_id message_key status",
}

// AutoReplyTransaction 原子执行一次业务变更，SQLite负责串行写入而不是客户端全账号锁。
func (s *Store) AutoReplyTransaction(ctx context.Context, owner string, action func(*AutoReplyTx) error) error {
	if strings.TrimSpace(owner) == "" {
		return fmt.Errorf("本地自动回复缺少账号归属")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = action(&AutoReplyTx{tx: tx, ctx: ctx, owner: owner}); err != nil {
		return err
	}
	return tx.Commit()
}

// autoReplyTable 仅允许访问内置业务表，禁止把调用参数拼成任意SQL标识符。
func autoReplyTable(kind string) (string, error) {
	if _, ok := autoReplyFields[kind]; !ok {
		return "", fmt.Errorf("未知自动回复实体类型：%s", kind)
	}
	return "auto_reply_" + kind, nil
}

// Get 按账号与稳定ID读取单个实体，缺失时返回found=false。
func (t *AutoReplyTx) Get(kind, id string) (row AutoReplyRow, found bool, err error) {
	table, err := autoReplyTable(kind)
	if err != nil {
		return row, false, err
	}
	row.ID = id
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT payload_json,revision FROM "+table+" WHERE owner_id=? AND id=?", t.owner, id).Scan(&raw, &row.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	row.Data = json.RawMessage(raw)
	return row, err == nil, err
}

// List 按少量索引条件读取实体，返回顺序始终是首次写入顺序。
func (t *AutoReplyTx) List(kind string, filters ...AutoReplyFilter) ([]AutoReplyRow, error) {
	table, err := autoReplyTable(kind)
	if err != nil {
		return nil, err
	}
	query := "SELECT id,payload_json,revision FROM " + table + " WHERE owner_id=?"
	args := []any{t.owner}
	for _, f := range filters {
		if !strings.Contains(" "+autoReplyFields[kind]+" ", " "+f.Field+" ") {
			return nil, fmt.Errorf("不支持的自动回复筛选字段：%s", f.Field)
		}
		query += " AND " + f.Field + "=?"
		args = append(args, f.Value)
	}
	order := " ORDER BY sequence"
	if kind == "messages" {
		order = " ORDER BY message_order,sequence"
	}
	rows, err := t.tx.QueryContext(t.ctx, query+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutoReplyRow{}
	for rows.Next() {
		var row AutoReplyRow
		var raw string
		if err = rows.Scan(&row.ID, &raw, &row.Revision); err != nil {
			return nil, err
		}
		row.Data = json.RawMessage(raw)
		out = append(out, row)
	}
	return out, rows.Err()
}

// Put 仅替换一条实体并递增版本，消息重复同步由调用方保留原记录。
func (t *AutoReplyTx) Put(kind, id string, value any) error {
	table, err := autoReplyTable(kind)
	if err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("本地实体缺少稳定编号")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO "+table+"(owner_id,id,payload_json,updated_at) VALUES(?,?,?,?) ON CONFLICT(owner_id,id) DO UPDATE SET payload_json=excluded.payload_json,revision="+table+".revision+1,updated_at=excluded.updated_at", t.owner, id, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	if err == nil && kind == "messages" {
		_, err = t.tx.ExecContext(t.ctx, "UPDATE auto_reply_messages SET message_order=sequence WHERE owner_id=? AND id=? AND message_order=0", t.owner, id)
	}
	return err
}

// Delete 删除当前账号的一条明确实体，不影响其他账号或实体。
func (t *AutoReplyTx) Delete(kind, id string) error {
	table, err := autoReplyTable(kind)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "DELETE FROM "+table+" WHERE owner_id=? AND id=?", t.owner, id)
	return err
}

// MigrateAutoReplyOwner 在同一事务导入旧快照与迁移标记，旧设置原文始终保留以便回退核查。
func (s *Store) MigrateAutoReplyOwner(ctx context.Context, owner, legacyKey string, importer func(*AutoReplyTx, []byte) error) error {
	return s.AutoReplyTransaction(ctx, owner, func(t *AutoReplyTx) error {
		var exists int
		err := t.tx.QueryRowContext(ctx, "SELECT 1 FROM auto_reply_migrations WHERE owner_id=?", owner).Scan(&exists)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var raw string
		err = t.tx.QueryRowContext(ctx, "SELECT value FROM local_settings WHERE key=?", legacyKey).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if raw != "" {
			if err = importer(t, []byte(raw)); err != nil {
				return fmt.Errorf("旧自动回复资料迁移未完成，原快照已保留：%w", err)
			}
		}
		sum := sha256.Sum256([]byte(raw))
		_, err = t.tx.ExecContext(ctx, "INSERT INTO auto_reply_migrations(owner_id,source_sha256,migrated_at) VALUES(?,?,?)", owner, hex.EncodeToString(sum[:]), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
}

// PendingMessages 读取最后一条HR消息之后尚未完成的候选人消息，读取水位不代表处理成功。
func (t *AutoReplyTx) PendingMessages(conversationID string) ([]AutoReplyRow, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,payload_json,revision FROM auto_reply_messages WHERE owner_id=? AND conversation_id=? AND direction='candidate' AND processed_status='' AND message_order>COALESCE((SELECT MAX(message_order) FROM auto_reply_messages WHERE owner_id=? AND conversation_id=? AND direction='self' AND processed_status NOT IN ('history_backfill','legacy_unverified')),0) ORDER BY message_order,sequence`, t.owner, conversationID, t.owner, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AutoReplyRow{}
	for rows.Next() {
		var r AutoReplyRow
		var raw string
		if err = rows.Scan(&r.ID, &raw, &r.Revision); err != nil {
			return nil, err
		}
		r.Data = json.RawMessage(raw)
		result = append(result, r)
	}
	return result, rows.Err()
}

// CompleteMessages 原子标记截至指定消息的处理结果，不修改采集水位。
func (t *AutoReplyTx) CompleteMessages(conversationID, messageKey, outcome string) error {
	var seq float64
	if err := t.tx.QueryRowContext(t.ctx, "SELECT message_order FROM auto_reply_messages WHERE owner_id=? AND conversation_id=? AND message_key=?", t.owner, conversationID, messageKey).Scan(&seq); err != nil {
		return fmt.Errorf("待完成消息没有找到：%w", err)
	}
	_, err := t.tx.ExecContext(t.ctx, "UPDATE auto_reply_messages SET processed_status=? WHERE owner_id=? AND conversation_id=? AND message_order<=? AND processed_status=''", outcome, t.owner, conversationID, seq)
	return err
}

// PruneAudits 只裁剪已完成的旧AI审计，保留运行中的任务与其工具记录。
func (t *AutoReplyTx) PruneAudits() error {
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM auto_reply_runs WHERE owner_id=? AND id IN (SELECT id FROM auto_reply_runs WHERE owner_id=? AND json_extract(payload_json,'$.status')!='running' ORDER BY sequence DESC LIMIT -1 OFFSET 1000)`, t.owner, t.owner)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `DELETE FROM auto_reply_tools WHERE owner_id=? AND NOT EXISTS(SELECT 1 FROM auto_reply_runs r WHERE r.owner_id=auto_reply_tools.owner_id AND r.id=auto_reply_tools.run_id)`, t.owner)
	return err
}

// LegacyAutoReplySendResult 仅在岗位与平台均对应时兼容旧任务发送收据，避免跨账号复用历史记录。
func (t *AutoReplyTx) LegacyAutoReplySendResult(platformID, positionID, threadID, replyHash string) (string, error) {
	var result string
	err := t.tx.QueryRowContext(t.ctx, `SELECT r.result FROM conversation_records r JOIN task_runs t ON t.task_id=r.task_id WHERE t.position_id=? AND r.platform_id=? AND r.conversation_key=? AND r.reply_hash=? AND r.result IN ('success','unknown') ORDER BY r.created_at DESC LIMIT 1`, positionID, platformID, threadID, replyHash).Scan(&result)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return result, err
}

// DueAutoReplyRows 有界读取到期后台待办，避免历史已完成任务增加恢复成本。
func (t *AutoReplyTx) DueAutoReplyRows(kind string, limit int) ([]AutoReplyRow, error) {
	if kind != "outbox" && kind != "recommendations" {
		return nil, fmt.Errorf("该实体不是后台待办")
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	table, _ := autoReplyTable(kind)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	query := "SELECT id,payload_json,revision FROM " + table + " WHERE owner_id=? AND status IN ('pending','failed','running') AND COALESCE(json_extract(payload_json,'$.next_attempt_at'),'')<=? AND COALESCE(json_extract(payload_json,'$.lease_until'),'')<=?"
	if kind == "outbox" {
		query += " AND json_extract(payload_json,'$.kind')!='report'"
	}
	query += " ORDER BY sequence LIMIT ?"
	rows, err := t.tx.QueryContext(t.ctx, query, t.owner, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AutoReplyRow{}
	for rows.Next() {
		var row AutoReplyRow
		var raw string
		if err = rows.Scan(&row.ID, &raw, &row.Revision); err != nil {
			return nil, err
		}
		row.Data = json.RawMessage(raw)
		result = append(result, row)
	}
	return result, rows.Err()
}

// PendingConversations 按招聘账号读取尚未完成采集或仍有待答消息的会话，不逐个加载账号历史聊天。
func (t *AutoReplyTx) PendingConversations(platformID, accountID string) ([]AutoReplyRow, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT c.id,c.payload_json,c.revision FROM auto_reply_conversations c
		WHERE c.owner_id=? AND c.platform_id=? AND c.account_id=?
		AND (json_type(c.payload_json,'$.capture_intent')='object' OR EXISTS(
			SELECT 1 FROM auto_reply_messages m WHERE m.owner_id=c.owner_id AND m.conversation_id=c.id
			AND m.direction='candidate' AND m.processed_status=''
			AND m.message_order>COALESCE((SELECT MAX(s.message_order) FROM auto_reply_messages s
				WHERE s.owner_id=c.owner_id AND s.conversation_id=c.id AND s.direction='self'
				AND s.processed_status NOT IN ('history_backfill','legacy_unverified')),0)))
		ORDER BY c.sequence LIMIT 100`, t.owner, platformID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AutoReplyRow{}
	for rows.Next() {
		var row AutoReplyRow
		var raw string
		if err = rows.Scan(&row.ID, &raw, &row.Revision); err != nil {
			return nil, err
		}
		row.Data = json.RawMessage(raw)
		items = append(items, row)
	}
	return items, rows.Err()
}

// MessageOrder 读取一条已知消息的顺序，用于把补采历史插入正确边界。
func (t *AutoReplyTx) MessageOrder(id string) (float64, error) {
	var order float64
	err := t.tx.QueryRowContext(t.ctx, "SELECT message_order FROM auto_reply_messages WHERE owner_id=? AND id=?", t.owner, id).Scan(&order)
	return order, err
}

// MarkHistoricalMessage 保留历史原文但不自动回复，并可把补采消息放回已知边界之前。
func (t *AutoReplyTx) MarkHistoricalMessage(id, status string, order float64) error {
	if order != 0 || status == "history_backfill" {
		_, err := t.tx.ExecContext(t.ctx, "UPDATE auto_reply_messages SET processed_status=?,message_order=? WHERE owner_id=? AND id=?", status, order, t.owner, id)
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, "UPDATE auto_reply_messages SET processed_status=? WHERE owner_id=? AND id=?", status, t.owner, id)
	return err
}

// PendingProposalReceipts 通过状态索引有界读取已提交判断的后续工作收据。
func (t *AutoReplyTx) PendingProposalReceipts(limit int) ([]AutoReplyRow, error) {
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT id,payload_json,revision FROM auto_reply_proposal_receipts WHERE owner_id=? AND status='pending' AND COALESCE(json_extract(payload_json,'$.next_attempt_at'),'')<=? ORDER BY updated_at,sequence LIMIT ?", t.owner, time.Now().UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AutoReplyRow{}
	for rows.Next() {
		var row AutoReplyRow
		var raw string
		if err = rows.Scan(&row.ID, &raw, &row.Revision); err != nil {
			return nil, err
		}
		row.Data = json.RawMessage(raw)
		items = append(items, row)
	}
	return items, rows.Err()
}

// MessageByKey 按会话及稳定消息键找到原记录，兼容迁移前保留下来的不同消息ID。
func (t *AutoReplyTx) MessageByKey(conversationID, key string) (row AutoReplyRow, found bool, err error) {
	var raw string
	err = t.tx.QueryRowContext(t.ctx, "SELECT id,payload_json,revision FROM auto_reply_messages WHERE owner_id=? AND conversation_id=? AND message_key=?", t.owner, conversationID, key).Scan(&row.ID, &raw, &row.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	row.Data = json.RawMessage(raw)
	return row, err == nil, err
}

// CountBlockedRecommendations 只统计当前账号等待人工处理的推荐，避免状态轮询读取报告正文。
func (t *AutoReplyTx) CountBlockedRecommendations() (count int, err error) {
	err = t.tx.QueryRowContext(t.ctx, "SELECT COUNT(*) FROM auto_reply_recommendations WHERE owner_id=? AND status='blocked'", t.owner).Scan(&count)
	return
}
