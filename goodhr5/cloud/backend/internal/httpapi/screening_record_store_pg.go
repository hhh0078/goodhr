// 本文件以PostgreSQL事务保存筛选快照，重复核对当前岗位所有者和租户，并在数据库执行全部分页与范围筛选。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresScreeningRecordStore 使用独立业务表，不修改原始简历、候选人触达或岗位日志。
type PostgresScreeningRecordStore struct{ db *sql.DB }

// NewPostgresScreeningRecordStore 注入既有数据库连接，不创建后台任务或额外连接池。
func NewPostgresScreeningRecordStore(db *sql.DB) *PostgresScreeningRecordStore {
	return &PostgresScreeningRecordStore{db: db}
}

// screeningPositionOwner 在事务内显式校验租户和本人岗位，锁定所有者以避免并发换租户或转交岗位。
func screeningPositionOwner(ctx context.Context, tx *sql.Tx, scope ScreeningScope) (string, error) {
	if !screeningScopeValid(scope) || !screeningUUIDPattern.MatchString(scope.TenantID) || !screeningUUIDPattern.MatchString(scope.PositionID) {
		return "", ErrNotFound
	}
	var ownerID string
	err := tx.QueryRowContext(ctx, `SELECT u.id::text FROM positions p JOIN users u ON u.id=p.user_id WHERE p.id=$1::uuid AND u.tenant_id=$2::uuid AND lower(u.email)=lower($3) AND u.status='active' FOR SHARE OF p,u`, scope.PositionID, scope.TenantID, scope.UserEmail).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return ownerID, err
}

// Save 串行化同一租户岗位attempt的首次插入与更新，迟到旧版本不覆盖新结果且所有历史版本冲突可识别。
func (s *PostgresScreeningRecordStore) Save(ctx context.Context, scope ScreeningScope, snapshot ScreeningSnapshot) (ScreeningRecord, error) {
	if err := validateScreeningSnapshot(&snapshot); err != nil {
		return ScreeningRecord{}, err
	}
	hash, err := screeningSnapshotHash(snapshot)
	if err != nil {
		return ScreeningRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ScreeningRecord{}, err
	}
	defer tx.Rollback()
	ownerID, err := screeningPositionOwner(ctx, tx, scope)
	if err != nil {
		return ScreeningRecord{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope.TenantID+"/"+scope.PositionID+"/"+snapshot.AttemptID); err != nil {
		return ScreeningRecord{}, err
	}
	var old screeningStoredRecord
	var raw, hashRaw []byte
	var storedOwner string
	err = tx.QueryRowContext(ctx, `SELECT id::text,position_id::text,snapshot,revision_hashes,created_at,updated_at,owner_user_id::text FROM position_screening_records WHERE tenant_id=$1::uuid AND position_id=$2::uuid AND attempt_id=$3::uuid FOR UPDATE`, scope.TenantID, scope.PositionID, snapshot.AttemptID).Scan(&old.Record.ID, &old.Record.PositionID, &raw, &hashRaw, &old.Record.CreatedAt, &old.Record.UpdatedAt, &storedOwner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ScreeningRecord{}, err
	}
	if err == nil {
		if storedOwner != ownerID {
			return ScreeningRecord{}, ErrNotFound
		}
		if err = json.Unmarshal(raw, &old.Record.ScreeningSnapshot); err != nil {
			return ScreeningRecord{}, err
		}
		if err = json.Unmarshal(hashRaw, &old.Hashes); err != nil {
			return ScreeningRecord{}, err
		}
		old.UserEmail = scope.UserEmail
	}
	next, err := screeningApplySnapshot(old, scope, snapshot, hash, time.Now())
	if err != nil {
		return ScreeningRecord{}, err
	}
	raw, err = json.Marshal(next.Record.ScreeningSnapshot)
	if err != nil {
		return ScreeningRecord{}, err
	}
	hashRaw, err = json.Marshal(next.Hashes)
	if err != nil {
		return ScreeningRecord{}, err
	}
	if old.Record.ID == "" {
		err = tx.QueryRowContext(ctx, `INSERT INTO position_screening_records(id,tenant_id,position_id,owner_user_id,attempt_id,revision,occurred_at,snapshot,revision_hashes,first_score,second_score,first_started_at,second_started_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11,$12,$13) RETURNING created_at,updated_at`, next.Record.ID, scope.TenantID, scope.PositionID, ownerID, snapshot.AttemptID, next.Record.Revision, next.Record.OccurredAt, string(raw), string(hashRaw), next.Record.First.Score, next.Record.Second.Score, next.Record.First.StartedAt, next.Record.Second.StartedAt).Scan(&next.Record.CreatedAt, &next.Record.UpdatedAt)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE position_screening_records SET revision=$1,snapshot=$2::jsonb,revision_hashes=$3::jsonb,first_score=$4,second_score=$5,first_started_at=$6,second_started_at=$7,updated_at=$8 WHERE id=$9 AND tenant_id=$10::uuid AND position_id=$11::uuid AND owner_user_id=$12 RETURNING created_at,updated_at`, next.Record.Revision, string(raw), string(hashRaw), next.Record.First.Score, next.Record.Second.Score, next.Record.First.StartedAt, next.Record.Second.StartedAt, next.Record.UpdatedAt, next.Record.ID, scope.TenantID, scope.PositionID, ownerID).Scan(&next.Record.CreatedAt, &next.Record.UpdatedAt)
	}
	if err != nil {
		return ScreeningRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return ScreeningRecord{}, err
	}
	return next.Record, nil
}

// screeningSQLWhere 仅从固定白名单字段构建参数化SQL，分数闭区间和时间左闭右开不会把空值当成零。
func screeningSQLWhere(scope ScreeningScope, ownerID string, query ScreeningRecordQuery) (string, []any) {
	args := []any{scope.TenantID, scope.PositionID, ownerID}
	parts := []string{"r.tenant_id=$1::uuid", "r.position_id=$2::uuid", "r.owner_user_id=$3"}
	for _, item := range []struct {
		column, op string
		value      *float64
	}{{"first_score", ">=", query.FirstScoreMin}, {"first_score", "<=", query.FirstScoreMax}, {"second_score", ">=", query.SecondScoreMin}, {"second_score", "<=", query.SecondScoreMax}} {
		if item.value != nil {
			args = append(args, *item.value)
			parts = append(parts, fmt.Sprintf("r.%s %s $%d", item.column, item.op, len(args)))
		}
	}
	for _, item := range []struct {
		column, op string
		value      *time.Time
	}{{"first_started_at", ">=", query.FirstStartedFrom}, {"first_started_at", "<", query.FirstStartedTo}, {"second_started_at", ">=", query.SecondStartedFrom}, {"second_started_at", "<", query.SecondStartedTo}} {
		if item.value != nil {
			args = append(args, *item.value)
			parts = append(parts, fmt.Sprintf("r.%s %s $%d", item.column, item.op, len(args)))
		}
	}
	return strings.Join(parts, " AND "), args
}

// List 在一致快照事务内计数和固定分页，过滤、稳定排序及LIMIT/OFFSET全部由数据库执行。
func (s *PostgresScreeningRecordStore) List(ctx context.Context, scope ScreeningScope, query ScreeningRecordQuery) (ScreeningRecordList, error) {
	if err := validateScreeningQuery(&query); err != nil {
		return ScreeningRecordList{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ScreeningRecordList{}, err
	}
	defer tx.Rollback()
	ownerID, err := screeningPositionOwner(ctx, tx, scope)
	if err != nil {
		return ScreeningRecordList{}, err
	}
	where, args := screeningSQLWhere(scope, ownerID, query)
	result := ScreeningRecordList{Records: []ScreeningRecord{}, Page: query.Page, PageSize: screeningRecordPageSize}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM position_screening_records r WHERE `+where, args...).Scan(&result.Total); err != nil {
		return ScreeningRecordList{}, err
	}
	args = append(args, screeningRecordPageSize, (query.Page-1)*screeningRecordPageSize)
	rows, err := tx.QueryContext(ctx, `SELECT r.id::text,r.position_id::text,r.snapshot,r.created_at,r.updated_at FROM position_screening_records r WHERE `+where+fmt.Sprintf(` ORDER BY r.occurred_at DESC,r.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return ScreeningRecordList{}, err
	}
	for rows.Next() {
		var item ScreeningRecord
		var raw []byte
		if err = rows.Scan(&item.ID, &item.PositionID, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
			rows.Close()
			return ScreeningRecordList{}, err
		}
		if err = json.Unmarshal(raw, &item.ScreeningSnapshot); err != nil {
			rows.Close()
			return ScreeningRecordList{}, err
		}
		result.Records = append(result.Records, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ScreeningRecordList{}, err
	}
	if err = tx.Commit(); err != nil {
		return ScreeningRecordList{}, err
	}
	return result, nil
}
