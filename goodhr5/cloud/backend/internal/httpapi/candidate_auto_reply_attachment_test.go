// 本文件用真实 PostgreSQL、磁盘原件及网页登录路由验证简历附件的来源、权限、预览与删除失效。
package httpapi

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// replyFileTestTenant 为已登录的模拟成员提供同一团队，无需依赖测试库的付费及团队管理模块。
type replyFileTestTenant struct {
	TenantStore
	id string
}

// GetOrCreateTenant 返回测试指定的团队身份。
func (s replyFileTestTenant) GetOrCreateTenant(email string) (Tenant, error) {
	return Tenant{ID: s.id}, nil
}

// IsTenantAdmin 按普通成员校验，避免管理员权限掩盖其他成员越权。
func (s replyFileTestTenant) IsTenantAdmin(tenant, email string) (bool, error) { return false, nil }

// TestCandidateReplyFileHTTP 验证一条消息多份附件、严格账号和会话归属、私有字节校验、真实 PDF 预览及删后再次请求。
func TestCandidateReplyFileHTTP(t *testing.T) {
	db := autoReplyStateTestDB(t)
	ctx := context.Background()
	var tenant, user, cv string
	read := func(dst any, q string, args ...any) {
		t.Helper()
		if err := db.QueryRow(q, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	read(&tenant, "INSERT INTO tenants DEFAULT VALUES RETURNING id")
	read(&user, "INSERT INTO users(tenant_id,email) VALUES($1,'owner@example.test') RETURNING id", tenant)
	read(&cv, "INSERT INTO candidate_profiles(tenant_id,created_by_user_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,$2,'合成人选','test','one') RETURNING id", tenant, user)
	base := t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	store := NewPostgresAutoReplyStore(db)
	srv := &Server{candidates: &CandidateService{auth: messageTestAuth(t), tenantStore: replyFileTestTenant{id: tenant}, store: NewPostgresCandidateStore(db), autoReply: store}}
	payload := func(kind, id string, m map[string]any) AutoReplyStateChange {
		j, _ := json.Marshal(m)
		return AutoReplyStateChange{Kind: kind, ID: id, Payload: j}
	}
	data := "%PDF-1.4\n测试原件\n%%EOF"
	fileRow := func(id, body, name string) (AutoReplyStateChange, string) {
		t.Helper()
		digest := replyImportHash(body)
		rel := replyImportHash("auto_reply:v1:"+replyImportHash(user)) + "/attachments/" + digest + ".pdf"
		actual := filepath.Join(base, replyImportHash(tenant+"\x00"+user), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(actual), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(actual, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return payload("attachments", id, map[string]any{"conversation_id": "thread", "candidate_id": "local", "source_message_id": "m", "relative_path": rel, "sha256": digest, "size_bytes": len(body), "original_name": name}), actual
	}
	pdf, actual := fileRow("pdf", data, "../人选\r\n简历.pdf")
	other, _ := fileRow("other", "<script>alert(1)</script>", "其它.pdf")
	rows := []AutoReplyStateChange{payload("candidates", "local", map[string]any{}), payload("cloud_links", "candidate:local", map[string]any{"value": cv}), payload("conversations", "thread", map[string]any{"candidate_id": "local"}), payload("messages", "m", map[string]any{"conversation_id": "thread", "direction": "candidate", "message_type": "resume"}), payload("messages", "orphan", map[string]any{"conversation_id": "thread", "direction": "candidate", "message_type": "resume"}), pdf, other}
	if _, err := store.CommitAutoReplyState(ctx, tenant, user, 0, rows); err != nil {
		t.Fatal(err)
	}
	h, err := store.candidateHistory(ctx, CandidateLibraryScope{TenantID: tenant, UserEmail: "owner@example.test"}, cv, nil)
	if err != nil || len(h.Messages) != 2 {
		t.Fatal(err, h)
	}
	dto, err := json.Marshal(h)
	if err != nil || strings.Contains(string(dto), base) || strings.Contains(string(dto), "relative_path") || strings.Contains(string(dto), "sha256") {
		t.Fatal("聊天接口泄露了私有文件地址或摘要", err)
	}
	for _, m := range h.Messages {
		if m.ID == "orphan" && len(m.Attachments) != 0 {
			t.Fatal("相邻消息误挂文件")
		}
		if m.ID == "m" && (len(m.Attachments) != 2 || m.Attachments[1].SourceMessageID != "m") {
			t.Fatal("同一条真实来信的多个附件未返回", m)
		}
	}
	link := "/api/candidates/" + cv + "/auto-reply/attachments/pdf?thread_id=" + url.QueryEscape(h.ThreadID)
	check := func(token, path string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := messageRequest(srv.candidateRoute, http.MethodGet, path, token, "")
		if w.Code != status {
			t.Fatalf("%s want %d got %d %s", token, status, w.Code, w.Body.String())
		}
		return w
	}
	w := check("owner", link+"&inline=1", 200)
	mode, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
	if err != nil || mode != "inline" || params["filename"] != "人选简历.pdf" || w.Header().Get("Content-Type") != "application/pdf" || w.Body.String() != data || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("私有PDF预览不符", w.Header())
	}
	if !strings.HasPrefix(check("owner", link, 200).Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("默认下载响应缺少附件标记")
	}
	bad := check("owner", strings.Replace(link, "/pdf?", "/other?", 1)+"&inline=1", 200)
	if bad.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(bad.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("伪PDF可预览")
	}
	check("", link, 401)
	check("plugin", link, 403)
	check("other", link, 404)
	check("owner", strings.Replace(link, "thread_id=", "thread_id=wrong", 1), 404)
	var otherCV string
	read(&otherCV, "INSERT INTO candidate_profiles(tenant_id,created_by_user_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,$2,'另一人','test','two') RETURNING id", tenant, user)
	check("owner", strings.Replace(link, cv, otherCV, 1), 404)
	if err = os.WriteFile(actual, []byte(strings.Repeat("x", len(data))), 0600); err != nil {
		t.Fatal(err)
	}
	check("owner", link, 503)
	if err = os.Remove(actual); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "other.pdf")
	if err = os.WriteFile(outside, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, actual); err != nil {
		t.Fatal(err)
	}
	check("owner", link, 503)
	if _, err = db.Exec("DELETE FROM candidate_profiles WHERE id=$1", cv); err != nil {
		t.Fatal(err)
	}
	check("owner", link, 404)
}
