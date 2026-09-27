// Package storage 文件作用：持久保存等待上传的浏览器退出事件，并关联当前运行岗位。
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// BrowserEvent 表示一次已脱敏、可重试上传的浏览器退出事件。
type BrowserEvent struct {
	EventID    string          `json:"event_id"`
	OwnerID    string          `json:"-"`
	PositionID string          `json:"position_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

// SaveBrowserEvent 写入待补传事件，并限制本机最多保留每个账号的一千条。
func (s *Store) SaveBrowserEvent(ctx context.Context, event BrowserEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO browser_events(event_id,owner_id,position_id,payload,created_at) VALUES(?,?,?,?,?)`,
		event.EventID, event.OwnerID, event.PositionID, string(event.Payload), event.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM browser_events WHERE owner_id=? AND event_id NOT IN (SELECT event_id FROM browser_events WHERE owner_id=? ORDER BY created_at DESC, event_id DESC LIMIT 1000)`, event.OwnerID, event.OwnerID)
	return err
}

// ClaimUnownedBrowserEvents 把启动后尚未绑定账号的事件归给首次成功绑定的账号。
func (s *Store) ClaimUnownedBrowserEvents(ctx context.Context, ownerID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE browser_events SET owner_id=? WHERE owner_id=''`, ownerID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM browser_events WHERE owner_id=? AND event_id NOT IN (SELECT event_id FROM browser_events WHERE owner_id=? ORDER BY created_at DESC,event_id DESC LIMIT 1000)`, ownerID, ownerID)
	return err
}

// PendingBrowserEvents 读取当前账号待上传的事件，每次最多二百条。
func (s *Store) PendingBrowserEvents(ctx context.Context, ownerID string) ([]BrowserEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,position_id,payload,created_at FROM browser_events WHERE owner_id=? ORDER BY created_at ASC,event_id ASC LIMIT 200`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BrowserEvent, 0, 200)
	for rows.Next() {
		var item BrowserEvent
		var payload, createdAt string
		if err := rows.Scan(&item.EventID, &item.PositionID, &payload, &createdAt); err != nil {
			return nil, err
		}
		item.OwnerID = ownerID
		item.Payload = json.RawMessage(payload)
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// DeleteBrowserEvents 删除云端已经确认接收的事件。
func (s *Store) DeleteBrowserEvents(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM browser_events WHERE event_id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

// RunningPositionID 读取当前运行任务所属的岗位编号。
func (s *Store) RunningPositionID(ctx context.Context) string {
	var positionID string
	_ = s.db.QueryRowContext(ctx, `SELECT position_id FROM task_runs WHERE status='running' ORDER BY started_at DESC LIMIT 1`).Scan(&positionID)
	return positionID
}
