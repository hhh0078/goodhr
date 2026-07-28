// Package download 文件作用：由 Go 主动同步 Worker 下载结果，保存本地记录并显示完成提示。
package download

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/files"
	"goodhr5/local-agent-go-new/internal/system/notification"
)

const syncInterval = time.Second

// Monitor 定时同步 Worker 已结束的下载记录。
type Monitor struct {
	Browser   *client.Client
	Store     *storage.Store
	Notifier  *notification.Notifier
	lastError string
}

// Run 持续同步下载结果，直到上下文被取消。
func (m *Monitor) Run(ctx context.Context) {
	m.syncQuietly(ctx)
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.syncQuietly(ctx)
		}
	}
}

// Sync 读取 Worker 下载结果，只持久化 saved 和 failed 终态。
func (m *Monitor) Sync(ctx context.Context) error {
	result, err := m.Browser.Downloads(ctx)
	if err != nil {
		return err
	}
	for index := len(result.Downloads) - 1; index >= 0; index-- {
		item := result.Downloads[index]
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status != "saved" && status != "failed" {
			continue
		}
		inserted, err := m.Store.SaveDownload(ctx, storageDownload(item, status))
		if err != nil {
			return err
		}
		if inserted && status == "saved" {
			m.notifySaved(ctx, item.FilePath)
		}
	}
	return nil
}

// History 返回 SQLite 中按时间倒序保存的下载历史。
func (m *Monitor) History(ctx context.Context) ([]storage.DownloadRecord, error) {
	if m == nil || m.Store == nil {
		return []storage.DownloadRecord{}, nil
	}
	return m.Store.ListDownloads(ctx)
}

// syncQuietly 在 Worker 尚未启动或已经停止时安静等待下一轮。
func (m *Monitor) syncQuietly(ctx context.Context) {
	if m == nil || m.Browser == nil || m.Store == nil {
		return
	}
	if err := m.Sync(ctx); err != nil {
		message := err.Error()
		if ctx.Err() == nil && message != m.lastError {
			log.Printf("[下载同步] 本轮没有同步成功，稍后再试 err=%v", err)
		}
		m.lastError = message
		return
	}
	m.lastError = ""
}

// notifySaved 显示下载提示，并执行用户选择的打开动作。
func (m *Monitor) notifySaved(ctx context.Context, filePath string) {
	if m.Notifier == nil || strings.TrimSpace(filePath) == "" {
		return
	}
	action, err := m.Notifier.NotifyDownload(ctx, filePath)
	if err != nil {
		log.Printf("[下载同步] 下载提示没有显示成功 file_name=%s err=%v", filepath.Base(filePath), err)
		return
	}
	switch action {
	case notification.DownloadOpen:
		err = files.Open(ctx, filePath)
	case notification.DownloadReveal:
		err = files.Reveal(ctx, filePath)
	}
	if err != nil {
		log.Printf("[下载同步] 用户选择的文件动作没有完成 file_name=%s action=%s err=%v", filepath.Base(filePath), action, err)
	}
}

// storageDownload 把 Worker 下载记录转换成本地存储模型。
func storageDownload(item contract.DownloadRecord, status string) storage.DownloadRecord {
	return storage.DownloadRecord{
		ID:                item.ID,
		URL:               item.URL,
		PageURL:           item.PageURL,
		FilePath:          item.FilePath,
		FileName:          item.FileName,
		SuggestedFilename: item.SuggestedFilename,
		Size:              item.Size,
		Status:            status,
		Error:             item.Error,
		CreatedAt:         item.CreatedAt,
	}
}
