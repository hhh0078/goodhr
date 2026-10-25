// Package storage 文件作用：事务保存两轮筛选完整快照及递增待传版本，上传回执只确认实际发送版本。
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/screening"
)

// ScreeningRecord 在本地附加已验证账号和岗位归属，这些字段不混入云端POST正文。
type ScreeningRecord struct {
	OwnerID    string
	PositionID string
	Snapshot   screening.Snapshot
}

// CreateScreeningRecord 在外部判定调用前持久保存独立尝试；失败时调用方不得继续执行未留档判定。
func (s *Store) CreateScreeningRecord(ctx context.Context, item ScreeningRecord) error {
	if item.OwnerID == "" || item.PositionID == "" || item.Snapshot.AttemptID == "" {
		return errors.New("筛选记录缺少账号、岗位或本次编号")
	}
	item.Snapshot.Revision = 1
	raw, err := json.Marshal(item.Snapshot)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO screening_records(attempt_id,owner_id,position_id,task_id,revision,payload_json,completed,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, item.Snapshot.AttemptID, item.OwnerID, item.PositionID, item.Snapshot.TaskID, 1, string(raw), item.Snapshot.Completed, item.Snapshot.OccurredAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// UpdateScreeningRecord 在事务内读取并修改最新完整快照，每次真实变化生成新版本，防止并发丢失轮次。
func (s *Store) UpdateScreeningRecord(ctx context.Context, attemptID string, change func(*screening.Snapshot) error) (screening.Snapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return screening.Snapshot{}, err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM screening_records WHERE attempt_id=?`, attemptID).Scan(&raw); err != nil {
		return screening.Snapshot{}, err
	}
	var item screening.Snapshot
	if err = json.Unmarshal([]byte(raw), &item); err != nil {
		return item, err
	}
	before, _ := json.Marshal(item)
	if err = change(&item); err != nil {
		return item, err
	}
	after, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	if string(before) == string(after) {
		return item, tx.Commit()
	}
	item.Revision++
	after, err = json.Marshal(item)
	if err != nil {
		return item, err
	}
	if len(after) > 256<<10 {
		return item, errors.New("筛选记录超过上传大小，请缩短判定原因")
	}
	_, err = tx.ExecContext(ctx, `UPDATE screening_records SET revision=?,payload_json=?,completed=?,retry_after=0,updated_at=? WHERE attempt_id=?`, item.Revision, string(after), item.Completed, time.Now().UTC().Format(time.RFC3339Nano), attemptID)
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}

// ScreeningRecord 读取一份本地完整快照，仅供内部流程和恢复检查，不向本地HTTP暴露跨账号数据。
func (s *Store) ScreeningRecord(ctx context.Context, attemptID string) (ScreeningRecord, error) {
	var item ScreeningRecord
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT owner_id,position_id,payload_json FROM screening_records WHERE attempt_id=?`, attemptID).Scan(&item.OwnerID, &item.PositionID, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &item.Snapshot)
	}
	return item, err
}

// PendingScreeningRecords 仅取当前已验证账号的待传最新版本，不领取其他账号或未到重试时间的记录。
func (s *Store) PendingScreeningRecords(ctx context.Context, ownerID string, limit int) ([]ScreeningRecord, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, errors.New("没有可用的筛选记录账号")
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `SELECT owner_id,position_id,payload_json FROM screening_records WHERE owner_id=? AND revision>acked_revision AND retry_after<=? ORDER BY updated_at,attempt_id LIMIT ?`, ownerID, time.Now().UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ScreeningRecord{}
	for rows.Next() {
		var item ScreeningRecord
		var raw string
		if err = rows.Scan(&item.OwnerID, &item.PositionID, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &item.Snapshot); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AckScreeningRecord 只确认本次真正发送的版本；上传期间产生的新版本仍保持待传。
func (s *Store) AckScreeningRecord(ctx context.Context, ownerID, attemptID string, sentRevision int64) error {
	if sentRevision <= 0 {
		return errors.New("云端没有确认筛选版本")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE screening_records SET acked_revision=MAX(acked_revision,MIN(?,revision)),retry_after=0 WHERE owner_id=? AND attempt_id=?`, sentRevision, ownerID, attemptID)
	return err
}

// DeferScreeningRecord 对失败的发送版本延后重试；期间若产生新版本，不能用旧失败回执延迟它。
func (s *Store) DeferScreeningRecord(ctx context.Context, ownerID, attemptID string, sentRevision int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE screening_records SET retry_after=? WHERE owner_id=? AND attempt_id=? AND revision=?`, time.Now().Add(15*time.Second).UnixMilli(), ownerID, attemptID, sentRevision)
	return err
}

// FinishScreeningTask 明确结束指定任务仍未结束的尝试；taskID为空时仅用于程序启动恢复旧快照。
func (s *Store) FinishScreeningTask(ctx context.Context, taskID, reason string) error {
	query := `SELECT attempt_id FROM screening_records WHERE completed=0`
	args := []any{}
	if taskID != "" {
		query += ` AND task_id=?`
		args = append(args, taskID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = s.UpdateScreeningRecord(ctx, id, func(item *screening.Snapshot) error { screening.CloseInterrupted(item, reason); return nil }); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("结束筛选记录失败：%w", err)
		}
	}
	return nil
}
