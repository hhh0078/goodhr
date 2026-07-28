// Package storage 文件作用：验证下载历史首次保存、去重和读取顺序。
package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// TestSaveDownload 验证同一下载只会被首次插入一次。
func TestSaveDownload(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("打开测试数据库失败：%v", err)
	}
	defer store.Close()

	record := DownloadRecord{
		ID:        "download_test",
		FilePath:  filepath.Join(t.TempDir(), "resume.pdf"),
		FileName:  "resume.pdf",
		Status:    "saved",
		CreatedAt: "2026-07-29T01:02:03Z",
	}
	inserted, err := store.SaveDownload(context.Background(), record)
	if err != nil || !inserted {
		t.Fatalf("首次保存下载失败：inserted=%v err=%v", inserted, err)
	}
	inserted, err = store.SaveDownload(context.Background(), record)
	if err != nil || inserted {
		t.Fatalf("重复下载不应再次插入：inserted=%v err=%v", inserted, err)
	}
	records, err := store.ListDownloads(context.Background())
	if err != nil {
		t.Fatalf("读取下载历史失败：%v", err)
	}
	if len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("下载历史不符合预期：%+v", records)
	}
}
