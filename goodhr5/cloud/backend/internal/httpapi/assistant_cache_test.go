// 本文件验证长期本地缓存的引用校验独立于Redis任务，拒绝跨账号、删除及旧版本资料且不续期。
package httpapi

import (
	"context"
	"net/http"
	"testing"
)

// TestAssistantCachedReferences 验证当前资料有效，旧版本及跨账号资料失效，Redis过期后仍可单独核对。
func TestAssistantCachedReferences(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	candidate, err := server.assistant.candidates.store.SaveCandidateProfile(CandidateProfileInput{TenantID: scope.TenantID, UserEmail: scope.UserEmail, PlatformID: "manual", PlatformCandidateID: "cached-reference", CandidateName: "缓存合成人选"})
	if err != nil {
		t.Fatal(err)
	}
	createAssistantForTest(t, routes, token, "cache-check")
	key := assistantKey(scope, "cache-check")
	before, err := server.assistant.store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"references": []assistantCachedReference{{candidate.ID, candidate.RowVersion}, {candidate.ID, candidate.RowVersion + 1}, {"deleted-candidate", 1}}}
	status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/references/validate", input)
	if status != 200 || len(data["invalid"].([]any)) != 2 {
		t.Fatalf("当前/旧版本引用校验错误: %d %v", status, data)
	}
	after, err := server.assistant.store.Get(context.Background(), key)
	if err != nil || !before.ExpiresAt.Equal(after.ExpiresAt) {
		t.Fatal("历史核对意外续期")
	}
	other := loginForTest(t, routes, "cached-other@example.com")
	status, data = candidateTestRequest(t, routes, other, http.MethodPost, "/api/assistant/references/validate", map[string]any{"references": []assistantCachedReference{{candidate.ID, candidate.RowVersion}}})
	if status != 200 || len(data["invalid"].([]any)) != 1 {
		t.Fatal("历史校验沿用了其他账号权限")
	}
	server.assistant.store = nil
	status, data = candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/references/validate", map[string]any{"references": []assistantCachedReference{{candidate.ID, candidate.RowVersion}}})
	if status != 200 || len(data["invalid"].([]any)) != 0 {
		t.Fatal("历史引用校验错误依赖了任务缓存")
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/references/validate", map[string]any{"references": []assistantCachedReference{{candidate.ID, 0}}})
	if status != 400 {
		t.Fatal("无效版本被接受")
	}
}
