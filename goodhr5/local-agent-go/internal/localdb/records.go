// Package localdb 负责管理本地设置、下载记录和截图记录。
package localdb

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Download 表示本地下载记录。
type Download struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	URL       string `json:"url"`
	FilePath  string `json:"file_path"`
	FileName  string `json:"file_name"`
	MimeType  string `json:"mime_type"`
	Size      int    `json:"size"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Screenshot 表示本地截图记录。
type Screenshot struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	FilePath  string `json:"file_path"`
	Label     string `json:"label"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	CreatedAt string `json:"created_at"`
}

// GetSettings 读取全部本地设置。
// 返回 key/value 字典。
func (db *DB) GetSettings() (map[string]any, error) {
	rows, err := db.conn.Query(`SELECT key, value FROM local_settings ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("读取本地设置失败：%w", err)
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			value = raw
		}
		result[key] = value
	}
	return result, rows.Err()
}

// SaveSettings 保存本地设置。
// payload 为设置字典。
func (db *DB) SaveSettings(payload map[string]any) (map[string]any, error) {
	now := nowISO()
	for key, value := range payload {
		if key == "" {
			continue
		}
		raw, _ := json.Marshal(value)
		_, err := db.conn.Exec(`
INSERT INTO local_settings(key, value, updated_at)
VALUES(?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, string(raw), now,
		)
		if err != nil {
			return nil, fmt.Errorf("保存本地设置失败：%w", err)
		}
	}
	return db.GetSettings()
}

// ListDownloads 读取本地下载记录。
// taskID 为空时返回全部记录。
func (db *DB) ListDownloads(taskID string) ([]Download, error) {
	query := `SELECT * FROM local_downloads`
	args := []any{}
	if taskID != "" {
		query += ` WHERE task_id=?`
		args = append(args, taskID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取下载记录失败：%w", err)
	}
	defer rows.Close()
	result := []Download{}
	for rows.Next() {
		var item Download
		err := rows.Scan(
			&item.ID, &item.TaskID, &item.URL, &item.FilePath, &item.FileName,
			&item.MimeType, &item.Size, &item.Status, &item.CreatedAt, &item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// SaveDownload 保存本地下载记录。
// payload 为下载记录参数。
func (db *DB) SaveDownload(payload map[string]any) (Download, error) {
	now := nowISO()
	item := Download{
		ID:        stringOr(payload["id"], uuid.NewString()),
		TaskID:    stringOr(payload["task_id"], ""),
		URL:       stringOr(payload["url"], ""),
		FilePath:  stringOr(payload["file_path"], stringOr(payload["path"], "")),
		FileName:  stringOr(payload["file_name"], stringOr(payload["filename"], "")),
		MimeType:  stringOr(payload["mime_type"], ""),
		Size:      intValue(payload["size"]),
		Status:    stringOr(payload["status"], "saved"),
		CreatedAt: stringOr(payload["created_at"], now),
		UpdatedAt: now,
	}
	_, err := db.conn.Exec(`
INSERT INTO local_downloads (
    id, task_id, url, file_path, file_name, mime_type, size, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    task_id=excluded.task_id,
    url=excluded.url,
    file_path=excluded.file_path,
    file_name=excluded.file_name,
    mime_type=excluded.mime_type,
    size=excluded.size,
    status=excluded.status,
    updated_at=excluded.updated_at`,
		item.ID, item.TaskID, item.URL, item.FilePath, item.FileName,
		item.MimeType, item.Size, item.Status, item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return Download{}, fmt.Errorf("保存下载记录失败：%w", err)
	}
	return item, nil
}

// ListScreenshots 读取本地截图记录。
// taskID 为空时返回全部记录。
func (db *DB) ListScreenshots(taskID string) ([]Screenshot, error) {
	query := `SELECT * FROM local_screenshots`
	args := []any{}
	if taskID != "" {
		query += ` WHERE task_id=?`
		args = append(args, taskID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取截图记录失败：%w", err)
	}
	defer rows.Close()
	result := []Screenshot{}
	for rows.Next() {
		var item Screenshot
		if err := rows.Scan(&item.ID, &item.TaskID, &item.FilePath, &item.Label, &item.Width, &item.Height, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// SaveScreenshot 保存本地截图记录。
// payload 为截图记录参数。
func (db *DB) SaveScreenshot(payload map[string]any) (Screenshot, error) {
	item := Screenshot{
		ID:        stringOr(payload["id"], uuid.NewString()),
		TaskID:    stringOr(payload["task_id"], ""),
		FilePath:  stringOr(payload["file_path"], stringOr(payload["path"], "")),
		Label:     stringOr(payload["label"], ""),
		Width:     intValue(payload["width"]),
		Height:    intValue(payload["height"]),
		CreatedAt: stringOr(payload["created_at"], nowISO()),
	}
	_, err := db.conn.Exec(`
INSERT OR REPLACE INTO local_screenshots (
    id, task_id, file_path, label, width, height, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.TaskID, item.FilePath, item.Label, item.Width, item.Height, item.CreatedAt,
	)
	if err != nil {
		return Screenshot{}, fmt.Errorf("保存截图记录失败：%w", err)
	}
	return item, nil
}
