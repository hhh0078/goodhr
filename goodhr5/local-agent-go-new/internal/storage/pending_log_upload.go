// Package storage 文件作用：记录需要在下次有登录凭证时补传的岗位日志。
package storage

import (
	"context"
	"strings"
	"time"
)

// MarkPositionLogUploadPending 标记岗位最近日志需要上传，重复标记只更新时间。
func (s *Store) MarkPositionLogUploadPending(ctx context.Context, positionID string) error {
	positionID = strings.TrimSpace(positionID)
	if positionID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO pending_log_uploads(position_id, updated_at) VALUES (?, ?)
		ON CONFLICT(position_id) DO UPDATE SET updated_at = excluded.updated_at`,
		positionID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// ClearPositionLogUploadPending 在云端确认接收后移除待补传标记。
func (s *Store) ClearPositionLogUploadPending(ctx context.Context, positionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_log_uploads WHERE position_id = ?`, strings.TrimSpace(positionID))
	return err
}

// PendingPositionLogUploads 返回尚未确认上传的岗位编号。
func (s *Store) PendingPositionLogUploads(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT position_id FROM pending_log_uploads ORDER BY updated_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var positionID string
		if err := rows.Scan(&positionID); err != nil {
			return nil, err
		}
		items = append(items, positionID)
	}
	return items, rows.Err()
}
