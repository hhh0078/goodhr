// 本文件负责使用 PostgreSQL 持久化用户流程快照和流程事件。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// PostgresUserFlowStore 使用 PostgreSQL 保存用户流程状态。
type PostgresUserFlowStore struct {
	db *sql.DB
}

// NewPostgresUserFlowStore 创建 PostgreSQL 用户流程存储。
func NewPostgresUserFlowStore(db *sql.DB) *PostgresUserFlowStore {
	return &PostgresUserFlowStore{db: db}
}

// Get 读取指定用户的流程快照。
func (s *PostgresUserFlowStore) Get(email string) (UserFlowState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return UserFlowState{}, err
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT flow_state FROM users WHERE email=$1`, email).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultUserFlowState(), nil
	}
	if err != nil {
		return UserFlowState{}, err
	}
	return parseUserFlowState(raw)
}

// Record 写入流程事件并在同一事务内刷新用户流程快照。
func (s *PostgresUserFlowStore) Record(email string, update UserFlowUpdate) (UserFlowState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserFlowState{}, err
	}
	defer tx.Rollback()
	userID, err := ensureUserID(ctx, s.db, email)
	if err != nil {
		return UserFlowState{}, err
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT flow_state FROM users WHERE id=$1::uuid FOR UPDATE`, userID).Scan(&raw); err != nil {
		return UserFlowState{}, err
	}
	state, err := parseUserFlowState(raw)
	if err != nil {
		return UserFlowState{}, err
	}
	next, err := applyUserFlowUpdate(state, update)
	if err != nil {
		return UserFlowState{}, err
	}
	nextRaw, err := json.Marshal(next)
	if err != nil {
		return UserFlowState{}, err
	}
	metadataRaw, err := json.Marshal(nonNilMap(update.Metadata))
	if err != nil {
		return UserFlowState{}, err
	}
	occurredAt := update.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET flow_state=$2::jsonb WHERE id=$1::uuid`, userID, string(nextRaw)); err != nil {
		return UserFlowState{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO user_flow_events (user_id, flow_version, event_key, status, reason_code, message, source, position_id, metadata, created_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::uuid, $9::jsonb, $10)
	`, userID, userFlowVersion, update.Step, defaultString(strings.TrimSpace(update.Status), "completed"), strings.TrimSpace(update.ReasonCode), limitUserFlowText(update.Message, 1000), strings.TrimSpace(update.Source), strings.TrimSpace(update.PositionID), string(metadataRaw), occurredAt); err != nil {
		return UserFlowState{}, err
	}
	if err := tx.Commit(); err != nil {
		return UserFlowState{}, err
	}
	return next, nil
}

// parseUserFlowState 解析数据库中的用户流程 JSON。
func parseUserFlowState(raw []byte) (UserFlowState, error) {
	if len(raw) == 0 || string(raw) == "{}" {
		return defaultUserFlowState(), nil
	}
	var state UserFlowState
	if err := json.Unmarshal(raw, &state); err != nil {
		return UserFlowState{}, err
	}
	return normalizeUserFlowState(state), nil
}
