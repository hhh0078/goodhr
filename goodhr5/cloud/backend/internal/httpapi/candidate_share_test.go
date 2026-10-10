// 本文件验证简历分享的真实路由权限、匿名白名单资料、长期链接、撤销和删除失效。
package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBuiltinCandidateShareHTTP 验证分享路由独立拒绝示例身份，即使存在错误的资料或历史分享映射也不能匿名公开。
func TestBuiltinCandidateShareHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "builtin-share-test@example.com")
	status, created := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "合成分享边界资料", "phone": "13800138000"})
	if status != http.StatusOK {
		t.Fatalf("create status=%d", status)
	}
	realID := created["candidate"].(map[string]any)["id"].(string)
	store := server.candidates.store.(*MemoryCandidateStore)
	store.mu.Lock()
	item := store.profiles[realID]
	item.ID = builtinCandidateID
	store.profiles[builtinCandidateID] = item
	store.mu.Unlock()
	path := "/api/candidates/" + builtinCandidateID + "/share"
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		status, _ = candidateTestRequest(t, routes, "", method, path, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous %s status=%d", method, status)
		}
		status, _ = candidateTestRequest(t, routes, token, method, path, nil)
		if status != http.StatusForbidden {
			t.Fatalf("builtin %s status=%d", method, status)
		}
	}
	secret := strings.Repeat("b", 64)
	digest := sha256.Sum256([]byte(secret))
	shareStore := server.candidateShares.store.(*MemoryCandidateShareStore)
	shareStore.mu.Lock()
	if len(shareStore.items) != 0 {
		t.Fatal("builtin share was written")
	}
	shareStore.mu.Unlock()
	if err := shareStore.SaveCandidateShare(CandidateShare{TokenHash: hex.EncodeToString(digest[:]), TenantID: item.TenantID, CandidateID: builtinCandidateID, UserEmail: item.UserEmail, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	status, public := candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/resume-shares/"+secret, nil)
	if status != http.StatusNotFound || public["candidate"] != nil {
		t.Fatalf("builtin leaked through stored share status=%d result=%v", status, public)
	}
	for _, endpoint := range []string{"", "/notes", "/recommendations"} {
		status, _ = candidateTestRequest(t, routes, "", http.MethodGet, "/api/candidates/"+builtinCandidateID+endpoint, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous builtin %s status=%d", endpoint, status)
		}
	}
	status, detail := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates/"+builtinCandidateID, nil)
	if status != http.StatusOK || detail["candidate"].(map[string]any)["candidate_name"] != "瓜瓜" {
		t.Fatalf("authenticated builtin status=%d result=%v", status, detail)
	}
}

// TestCandidateShareHTTP 验证分享只能由有权查看简历的网页用户创建，公开内容不包含工作记录。
func TestCandidateShareHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	owner := "share-owner@example.com"
	token := loginForTest(t, routes, owner)
	status, created := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{
		"candidate_name": "分享合成候选人", "phone": "13800138000", "email": "candidate@example.invalid",
		"personal_description": "公开个人优势", "raw_text": "private-raw-resume", "ai_summary": "private-ai-summary",
		"tags": []string{"private-internal-tag"}, "work_experiences": []map[string]any{{"company_name": "公开公司", "content": "公开经历"}},
		"colleague_communications": []map[string]any{{"content": "private-colleague-chat"}},
	})
	if status != http.StatusOK {
		t.Fatalf("create status=%d result=%v", status, created)
	}
	id := created["candidate"].(map[string]any)["id"].(string)
	path := "/api/candidates/" + id + "/share"
	status, _ = candidateTestRequest(t, routes, "", http.MethodPost, path, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous create status=%d", status)
	}
	other := loginForTest(t, routes, "share-other@example.com")
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		status, _ = candidateTestRequest(t, routes, other, method, path, nil)
		if status != http.StatusNotFound {
			t.Fatalf("other team method=%s status=%d", method, status)
		}
	}
	_, plugin := candidateTestRequest(t, routes, token, http.MethodPost, "/api/auth/extension-session", map[string]any{})
	pluginToken, _ := plugin["token"].(string)
	status, _ = candidateTestRequest(t, routes, pluginToken, http.MethodPost, path, nil)
	if pluginToken == "" || status != http.StatusForbidden {
		t.Fatalf("plugin create status=%d", status)
	}
	_, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates/"+id+"/notes", map[string]any{"content": "private-manual-note"})
	store := server.candidates.store.(*MemoryCandidateStore)
	store.mu.Lock()
	item := store.profiles[id]
	item.AIDetailReason, item.PlatformCandidateID, item.PlatformAccountID = "private-ai-reason", "private-platform-id", "private-account-id"
	item.Ext = map[string]any{"unexpected": "private-extension-secret"}
	item.LibraryFields["gender"] = map[string]any{"nested_private": "private-nested-field"}
	store.profiles[id] = item
	store.mu.Unlock()
	status, shared := candidateTestRequest(t, routes, token, http.MethodPost, path, nil)
	if status != http.StatusOK {
		t.Fatalf("share status=%d result=%v", status, shared)
	}
	secret := shared["token"].(string)
	if len(secret) != 64 || strings.Contains(secret, id) {
		t.Fatalf("invalid secret=%s", secret)
	}
	digest := sha256.Sum256([]byte(secret))
	shareStore := server.candidateShares.store.(*MemoryCandidateShareStore)
	if _, err := shareStore.LoadCandidateShare(secret); !errors.Is(err, ErrNotFound) {
		t.Fatal("plain token was stored")
	}
	if _, err := shareStore.LoadCandidateShare(hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	publicPath := "/api/public/resume-shares/" + secret
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, publicPath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("public status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("public response allowed caching")
	}
	for name, value := range map[string]string{"Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow"} {
		if response.Header().Get(name) != value {
			t.Fatalf("header %s=%s", name, response.Header().Get(name))
		}
	}
	for _, private := range []string{owner, "private-raw-resume", "private-ai-summary", "private-internal-tag", "private-colleague-chat", "private-manual-note", "private-ai-reason", "private-platform-id", "private-account-id", "private-extension-secret", "private-nested-field", `"notes"`, `"recommendations"`, `"raw_text"`, `"ai_summary"`, `"user_email"`, `"events"`, `"source_url"`} {
		if bytes.Contains(response.Body.Bytes(), []byte(private)) {
			t.Fatalf("public leak=%s body=%s", private, response.Body.String())
		}
	}
	if !strings.Contains(response.Body.String(), "公开经历") || !strings.Contains(response.Body.String(), "13800138000") {
		t.Fatal("public resume fields missing")
	}
	// 后续资料维护和再次生成不会让先前发出的长期链接失效。
	status, _ = candidateTestRequest(t, routes, token, http.MethodPatch, "/api/candidates/"+id, map[string]any{"row_version": created["candidate"].(map[string]any)["row_version"], "personal_description": "更新后的公开优势"})
	if status != http.StatusOK {
		t.Fatalf("update status=%d", status)
	}
	status, second := candidateTestRequest(t, routes, token, http.MethodPost, path, nil)
	if status != http.StatusOK || second["token"] == secret {
		t.Fatalf("second share=%v", second)
	}
	status, live := candidateTestRequest(t, routes, "", http.MethodGet, publicPath, nil)
	if status != http.StatusOK || live["candidate"].(map[string]any)["personal_description"] != "更新后的公开优势" {
		t.Fatalf("live=%v status=%d", live, status)
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodDelete, path, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke status=%d", status)
	}
	for _, secret := range []string{secret, second["token"].(string)} {
		status, _ = candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/resume-shares/"+secret, nil)
		if status != http.StatusNotFound {
			t.Fatalf("revoked status=%d", status)
		}
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodDelete, path, nil)
	if status != http.StatusOK {
		t.Fatalf("repeated revoke=%d", status)
	}
	_, reopened := candidateTestRequest(t, routes, token, http.MethodPost, path, nil)
	store.mu.Lock()
	delete(store.profiles, id)
	store.mu.Unlock()
	status, _ = candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/resume-shares/"+reopened["token"].(string), nil)
	if status != http.StatusNotFound {
		t.Fatalf("deleted candidate public=%d", status)
	}
	for _, invalid := range []string{"guess", strings.Repeat("z", 64), strings.Repeat("a", 64) + "/extra"} {
		status, _ = candidateTestRequest(t, routes, "", http.MethodGet, "/api/public/resume-shares/"+invalid, nil)
		if status != http.StatusNotFound {
			t.Fatalf("invalid token=%s status=%d", invalid, status)
		}
	}
}

// TestCandidateShareTeamMemberHTTP 验证同团队普通成员也不能分享其他入库人的简历。
func TestCandidateShareTeamMemberHTTP(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	owner := "share-team-owner@example.com"
	member := "share-team-member@example.com"
	ownerToken := loginForTest(t, routes, owner)
	memberToken := loginForTest(t, routes, member)
	_, created := candidateTestRequest(t, routes, ownerToken, http.MethodPost, "/api/candidates", map[string]any{"candidate_name": "权限合成简历", "phone": "13900139000"})
	id := created["candidate"].(map[string]any)["id"].(string)
	tenant, err := server.tenants.store.GetOrCreateTenant(owner)
	if err != nil {
		t.Fatal(err)
	}
	invite, _, err := server.tenants.store.InviteMember(tenant.ID, member, "member", owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.tenants.store.AcceptInvitation(invite.ID, member); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		status, _ := candidateTestRequest(t, routes, memberToken, method, "/api/candidates/"+id+"/share", nil)
		if status != http.StatusNotFound {
			t.Fatalf("member method=%s status=%d", method, status)
		}
	}
}

// TestCandidateSharePostgres 验证真实迁移、摘要持久化、作用域撤销及删除简历时的外键级联。
func TestCandidateSharePostgres(t *testing.T) {
	db := candidatePostgresTestDB(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0089_candidate_shares.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var tenantID string
	if err = db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	email := "share-pg-owner@example.com"
	if _, err = db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$2)`, email, tenantID); err != nil {
		t.Fatal(err)
	}
	candidates := NewPostgresCandidateStore(db)
	candidate, err := candidates.SaveCandidateProfile(CandidateProfileInput{TenantID: tenantID, UserEmail: email, PlatformID: "manual", PlatformCandidateID: "share-pg", CandidateName: "分享存储合成简历"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewCandidateShareStore(db)
	share := CandidateShare{TokenHash: strings.Repeat("a", 64), TenantID: tenantID, CandidateID: candidate.ID, UserEmail: email, CreatedAt: time.Now().UTC()}
	if err = store.SaveCandidateShare(share); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadCandidateShare(share.TokenHash)
	if err != nil || loaded.CandidateID != candidate.ID {
		t.Fatalf("load=%+v err=%v", loaded, err)
	}
	if err = store.RevokeCandidateShares(tenantID, "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadCandidateShare(share.TokenHash); err != nil {
		t.Fatal("another candidate revoke changed link")
	}
	if err = store.RevokeCandidateShares(tenantID, candidate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadCandidateShare(share.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoke did not persist")
	}
	if err = store.SaveCandidateShare(share); err != nil {
		t.Fatal(err)
	}
	if _, err = candidates.DeleteTeamCandidates(tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadCandidateShare(share.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatal("delete did not cascade")
	}
	encoded, err := json.Marshal(publicSharedCandidate(candidate))
	if err != nil || !bytes.Contains(encoded, []byte("分享存储合成简历")) {
		t.Fatalf("public=%s err=%v", encoded, err)
	}
}
