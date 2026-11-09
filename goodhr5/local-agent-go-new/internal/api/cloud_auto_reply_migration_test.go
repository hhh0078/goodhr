// Package api 本文件验证迁移收据未确认、并发修改和跨账号路径不能导致本机资料丢失。
package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestCloudAutoReplyMigrationReceiptAndConcurrentChange 模拟断线后的相同归档重试、写入冲突和最终清理。
func TestCloudAutoReplyMigrationReceiptAndConcurrentChange(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const user = "legacy-account"
	owner := "auto_reply:v1:" + migrationHash(user)
	const oldMessage = "这段旧日志只应在业务迁移成功后清理"
	if _, err = db.SaveTaskLog(ctx, storage.TaskLog{PositionID: "legacy-position", Flow: "analysis", Step: "analysis.auto_reply.sync", Message: oldMessage}); err != nil {
		t.Fatal(err)
	}
	content := []byte("old attachment")
	digest := migrationHash(string(content))
	f := migrationFile{RelativePath: migrationHash(owner) + "/attachments/" + digest + ".pdf", SizeBytes: int64(len(content)), SHA256: digest}
	dest := filepath.Join(dir, "auto-reply", filepath.FromSlash(f.RelativePath))
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err = db.AutoReplyTransaction(ctx, owner, func(tx *storage.AutoReplyTx) error { return tx.Put("attachments", "file1", f) }); err != nil {
		t.Fatal(err)
	}
	n := 0
	headers := []string{}
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			io.WriteString(w, `{"user":{"id":"legacy-account"}}`)
			return
		}
		if r.URL.Path != "/api/auto-reply/agent/migration/import" || r.Header.Get("X-GoodHR-Machine-ID") != "mac-test" {
			http.Error(w, "binding", 403)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		header := r.Header.Get("X-GoodHR-Archive-SHA256")
		headers = append(headers, header)
		if migrationHash(string(raw)) != header {
			t.Error("归档摘要不一致")
		}
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil || len(zr.File) != 3 {
			t.Error("归档内容缺失")
			return
		}
		n++
		accepted := 1
		switch n {
		case 1:
			header = "wrong-receipt"
		case 2:
			if err = db.AutoReplyTransaction(ctx, owner, func(tx *storage.AutoReplyTx) error {
				return tx.Put("candidates", "new-record", map[string]string{"id": "new-record"})
			}); err != nil {
				t.Error(err)
			}
		case 3:
			accepted = 2
		}
		json.NewEncoder(w).Encode(migrationReceipt{Digest: header, Accepted: accepted, FileCount: 1})
	}))
	defer svc.Close()
	s := Server{cfg: config.Config{DataDir: dir, CloudURL: svc.URL}, store: db, extensionToken: "session", extensionUserID: user}
	cred := cloud.AgentCredentials{Token: "session", MachineID: "mac-test"}
	for attempt := range 2 {
		if err = s.migrateCloudAutoReply(ctx, cred); err == nil {
			t.Fatal("不完整收据或并发更新不应清理")
		}
		if _, err = os.Stat(dest); err != nil {
			t.Fatal("附件在获完整收据前丢失", err)
		}
		logs, e := db.ListPositionLogs(ctx, "legacy-position", 10)
		if e != nil || len(logs) != 1 || logs[0].Message != oldMessage {
			t.Fatal("业务迁移未成功，旧日志不应提前清理", e)
		}
		backups, err := filepath.Glob(filepath.Join(dir, ".reply-migration", migrationHash(owner), "*.zip"))
		if err != nil || len(backups) != 1 {
			t.Fatal("失败必须保留私有备份", backups, err)
		}
		if attempt == 0 {
			// 上次失败保留的私有 ZIP 即使损坏，重试也应重新核验并上传完整源资料。
			if err = os.WriteFile(backups[0], []byte("损坏的旧备份"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if headers[0] != headers[1] {
		t.Fatal("相同源资料重试需要同一摘要")
	}
	if err = s.migrateCloudAutoReply(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("已上传附件未清理")
	}
	logs, e := db.ListPositionLogs(ctx, "legacy-position", 10)
	if e != nil || len(logs) != 1 || logs[0].Message == oldMessage {
		t.Fatal("业务迁移成功后应只保留技术摘要", e)
	}
	rows, err := db.ExportAutoReplyOwner(ctx, owner)
	if err != nil || len(rows) != 0 {
		t.Fatal("迁移完成后仍有业务行", len(rows), err)
	}
	if _, err = os.Stat(filepath.Join(dir, ".reply-migration", migrationHash(owner))); !os.IsNotExist(err) {
		t.Fatal("迁移备份未完成清理")
	}
	if err = s.migrateCloudAutoReply(ctx, cred); err != nil || n != 3 {
		t.Fatal("空账号不应再次上传", err, n)
	}
}

// TestMigrationFileRejectsForeignAndUnsafePaths 验证可移植路径转换、跨账号和上级目录拒绝。
func TestMigrationFileRejectsForeignAndUnsafePaths(t *testing.T) {
	const owner = "account"
	hash := migrationHash("x")
	f := migrationFile{SHA256: hash, SizeBytes: 1, RelativePath: migrationHash(owner) + "\\attachments\\" + hash + ".pdf"}
	if _, err := prepareMigrationFile(f, owner); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"/tmp/" + hash + ".pdf", "../" + hash + ".pdf", migrationHash("other") + "/attachments/" + hash + ".pdf"} {
		f.RelativePath = bad
		if _, err := prepareMigrationFile(f, owner); err == nil {
			t.Fatal("接受了非受管路径")
		}
	}
}

// TestFirstAgentBindMigratesLegacyState 通过真实绑定处理函数，核验首次登录自动迁移且不用再点启动。
func TestFirstAgentBindMigratesLegacyState(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner := "auto_reply:v1:" + migrationHash("first-login")
	if err = db.AutoReplyTransaction(context.Background(), owner, func(tx *storage.AutoReplyTx) error {
		return tx.Put("candidates", "first", map[string]string{"id": "first"})
	}); err != nil {
		t.Fatal(err)
	}
	uploaded := make(chan struct{}, 1)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents/bind":
			io.WriteString(w, `{"ok":true,"agent":{"machine_id":"goodhr-device-v1-unit","bind_status":"active"}}`)
		case "/api/auth/me":
			io.WriteString(w, `{"user":{"id":"first-login","email":"unit@example.test"}}`)
		case "/api/auto-reply/agent/migration/import":
			io.Copy(io.Discard, r.Body)
			json.NewEncoder(w).Encode(migrationReceipt{Digest: r.Header.Get("X-GoodHR-Archive-SHA256"), Accepted: 1})
			uploaded <- struct{}{}
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	s := Server{cfg: config.Config{CloudURL: remote.URL, DataDir: dir, Port: 43129}, store: db, cloud: cloud.New(remote.URL), deviceID: func() (string, error) { return "goodhr-device-v1-unit", nil }}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/session/bind", bytes.NewBufferString(`{"token":"first-login-token"}`))
	rw := httptest.NewRecorder()
	s.handleAgentBind(rw, r)
	if rw.Code != 200 {
		t.Fatal("首次绑定失败", rw.Code)
	}
	select {
	case <-uploaded:
	case <-time.After(3 * time.Second):
		t.Fatal("首次绑定没有启动迁移")
	}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if s.cloudMigrationProgress().Status == "completed" {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("首次绑定后没有达到迁移完成状态", s.cloudMigrationProgress())
}
