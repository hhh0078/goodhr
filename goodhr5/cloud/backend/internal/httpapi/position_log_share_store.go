// 本文件负责在内存、PostgreSQL 和 Redis 包装层保存限时排障日志快照。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SavePositionLogShare 在内存中替换该岗位旧链接并固定当前日志。
func (s *MemoryPositionLogStore) SavePositionLogShare(tokenHash, positionID, userEmail string, snapshot []PositionLog, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, item := range s.shares {
		if item.PositionID == positionID || !item.ExpiresAt.After(s.now()) {
			delete(s.shares, key)
		}
	}
	s.shares[tokenHash] = PositionLogShare{PositionID: positionID, UserEmail: userEmail, Snapshot: append([]PositionLog(nil), snapshot...), ExpiresAt: expiresAt}
	return nil
}

// LoadPositionLogShare 从内存中读取未过期的固定日志快照。
func (s *MemoryPositionLogStore) LoadPositionLogShare(tokenHash string) ([]PositionLog, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.shares[tokenHash]
	if !ok || !item.ExpiresAt.After(s.now()) {
		return nil, time.Time{}, ErrNotFound
	}
	return append([]PositionLog(nil), item.Snapshot...), item.ExpiresAt, nil
}

// SavePositionLogShare 在 PostgreSQL 中只保留该岗位一条有效快照。
func (s *PostgresPositionLogStore) SavePositionLogShare(tokenHash, positionID, userEmail string, snapshot []PositionLog, expiresAt time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	content, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM position_log_shares WHERE expires_at <= now() OR position_id=$1`, positionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO position_log_shares(token_hash,position_id,user_email,snapshot,expires_at) VALUES($1,$2,$3,$4,$5)`, tokenHash, positionID, userEmail, content, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

// LoadPositionLogShare 从 PostgreSQL 读取未过期的日志快照。
func (s *PostgresPositionLogStore) LoadPositionLogShare(tokenHash string) ([]PositionLog, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var content []byte
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT snapshot,expires_at FROM position_log_shares WHERE token_hash=$1 AND expires_at>now()`, tokenHash).Scan(&content, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	var items []PositionLog
	if err := json.Unmarshal(content, &items); err != nil {
		return nil, time.Time{}, err
	}
	return items, expiresAt, nil
}

// SavePositionLogShare 把 Redis 包装层的快照写入持久化存储。
func (s *RedisPositionLogStore) SavePositionLogShare(tokenHash, positionID, userEmail string, snapshot []PositionLog, expiresAt time.Time) error {
	store, ok := s.persistent.(PositionLogShareStore)
	if !ok {
		return errors.New("position log share storage unavailable")
	}
	return store.SavePositionLogShare(tokenHash, positionID, userEmail, snapshot, expiresAt)
}

// LoadPositionLogShare 从 Redis 包装层对应的持久化存储读取快照。
func (s *RedisPositionLogStore) LoadPositionLogShare(tokenHash string) ([]PositionLog, time.Time, error) {
	store, ok := s.persistent.(PositionLogShareStore)
	if !ok {
		return nil, time.Time{}, errors.New("position log share storage unavailable")
	}
	return store.LoadPositionLogShare(tokenHash)
}
