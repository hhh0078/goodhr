// Package localdb 负责测试本地岗位运行数据库能力。
package localdb

import (
	"encoding/json"
	"testing"

	"goodhr5/local-agent-go/internal/config"
)

// TestPositionLogCandidateFlow 验证岗位运行、日志和候选人的基本读写流程。
func TestPositionLogCandidateFlow(t *testing.T) {
	db := openTestDB(t)
	position, err := db.CreatePosition(map[string]any{
		"name":        "测试岗位运行",
		"platform_id": "boss",
		"match_limit": 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if position.ID == "" || position.Status != "pending" {
		t.Fatalf("unexpected position: %+v", position)
	}
	updated, err := db.UpdatePositionStatus(position.ID, "running")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "running" {
		t.Fatalf("status = %s", updated.Status)
	}
	if _, err := db.AddPositionLog(position.ID, "info", "开始岗位运行"); err != nil {
		t.Fatal(err)
	}
	logs, err := db.ListPositionLogs(position.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Message != "开始岗位运行" {
		t.Fatalf("logs = %+v", logs)
	}
	candidate, err := db.SaveCandidate(position.ID, map[string]any{"name": "候选人A", "status": "scanned"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate["id"] == "" {
		t.Fatalf("candidate missing id: %+v", candidate)
	}
	candidates, err := db.ListCandidates(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates len = %d", len(candidates))
	}
}

// TestSettingsRecordsFlow 验证本地设置和运行记录读写流程。
func TestSettingsRecordsFlow(t *testing.T) {
	db := openTestDB(t)
	settings, err := db.SaveSettings(map[string]any{"browser_download_dir": "/tmp/goodhr-downloads"})
	if err != nil {
		t.Fatal(err)
	}
	if settings["browser_download_dir"] != "/tmp/goodhr-downloads" {
		t.Fatalf("settings = %+v", settings)
	}

	download, err := db.SaveDownload(map[string]any{
		"position_id": "position-1",
		"url":         "https://example.com/a.pdf",
		"file_path":   "/tmp/a.pdf",
		"file_name":   "a.pdf",
		"mime_type":   "application/pdf",
		"size":        json.Number("12"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if download.FileName != "a.pdf" || download.Size != 12 {
		t.Fatalf("download = %+v", download)
	}
	downloads, err := db.ListDownloads("position-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(downloads) != 1 {
		t.Fatalf("downloads len = %d", len(downloads))
	}

}

// TestMigratePositionScannedCountsRepairsLegacyValueOnce 验证旧版扫描统计只补偿一次跳过和失败人数。
func TestMigratePositionScannedCountsRepairsLegacyValueOnce(t *testing.T) {
	db := openTestDB(t)
	position, err := db.CreatePosition(map[string]any{"name": "旧版统计岗位", "platform_id": "boss"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`
UPDATE local_positions
SET scanned_count=53, greeted_count=53, skipped_count=9, failed_count=0
WHERE id=?
`, position.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`DELETE FROM local_meta WHERE key='position_scanned_count_semantics_v2'`); err != nil {
		t.Fatal(err)
	}
	if err := db.migratePositionScannedCounts(); err != nil {
		t.Fatal(err)
	}
	if err := db.migratePositionScannedCounts(); err != nil {
		t.Fatal(err)
	}
	updated, err := db.GetPosition(position.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ScannedCount != 62 || updated.GreetedCount != 53 || updated.SkippedCount != 9 || updated.FailedCount != 0 {
		t.Fatalf("position counts = %+v", updated)
	}
}

// openTestDB 创建测试数据库。
// t 为测试对象。
func openTestDB(t *testing.T) *DB {
	t.Helper()
	cfg := &config.Config{DataDir: t.TempDir()}
	db, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
