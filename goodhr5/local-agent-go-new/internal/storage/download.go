// Package storage 文件作用：保存和读取不含页面正文的本地浏览器下载记录。
package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DownloadRecord 表示一条已经结束的本地浏览器下载记录。
type DownloadRecord struct {
	ID                string `json:"id"`
	URL               string `json:"url"`
	PageURL           string `json:"page_url"`
	FilePath          string `json:"file_path"`
	FileName          string `json:"file_name"`
	SuggestedFilename string `json:"suggested_filename"`
	Size              int64  `json:"size"`
	Status            string `json:"status"`
	Error             string `json:"error"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

// SaveDownload 保存一条成功或失败的下载记录，返回 true 表示首次保存。
func (s *Store) SaveDownload(ctx context.Context, record DownloadRecord) (bool, error) {
	record.ID = strings.TrimSpace(record.ID)
	record.Status = strings.ToLower(strings.TrimSpace(record.Status))
	if record.ID == "" {
		return false, fmt.Errorf("下载记录编号不能为空")
	}
	if record.Status != "saved" && record.Status != "failed" {
		return false, fmt.Errorf("下载状态必须是 saved 或 failed")
	}
	if record.Status == "saved" && strings.TrimSpace(record.FilePath) == "" {
		return false, fmt.Errorf("成功下载记录缺少文件路径")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if strings.TrimSpace(record.CreatedAt) == "" {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	result, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO download_records (
			id, url, page_url, file_path, file_name, suggested_filename,
			size, status, error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		record.URL,
		record.PageURL,
		record.FilePath,
		record.FileName,
		record.SuggestedFilename,
		record.Size,
		record.Status,
		record.Error,
		record.CreatedAt,
		record.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("保存下载记录失败：%w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("读取下载保存结果失败：%w", err)
	}
	return affected > 0, nil
}

// ListDownloads 按捕获时间倒序读取本地下载历史。
func (s *Store) ListDownloads(ctx context.Context) ([]DownloadRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, page_url, file_path, file_name, suggested_filename,
		       size, status, error, created_at, updated_at
		FROM download_records
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取下载记录失败：%w", err)
	}
	defer rows.Close()

	records := make([]DownloadRecord, 0)
	for rows.Next() {
		var record DownloadRecord
		if err := rows.Scan(
			&record.ID,
			&record.URL,
			&record.PageURL,
			&record.FilePath,
			&record.FileName,
			&record.SuggestedFilename,
			&record.Size,
			&record.Status,
			&record.Error,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("解析下载记录失败：%w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历下载记录失败：%w", err)
	}
	return records, nil
}
