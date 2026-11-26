// Package httpapi 文件作用：用隔离PostgreSQL和真实HTTP处理器验证服务器AI请求账本投影、公开分享、覆盖上传、清空水位及账号隔离，不访问模型或生产数据。
package httpapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
	"goodhr5/local-agent-go-new/cloudruntime"
)

const runtimeAIPositionForTest = "10000000-0000-4000-8000-000000000001"
const runtimeAITenantForTest = "20000000-0000-4000-8000-000000000001"
const runtimeAIOwnerForTest = "30000000-0000-4000-8000-000000000001"
const runtimeAIEmailForTest = "runtime-ai-log@example.com"
const runtimeAIServiceTokenForTest = "synthetic-runtime-service-token"

// runtimeAILogItem 保存岗位列表和公开分享共同返回的事件，便于逐字段核对真实账本元数据。
type runtimeAILogItem struct {
	ID    string                  `json:"id"`
	Event positionDiagnosticEvent `json:"event"`
}

// TestRuntimeAIRequestErrorDetailProjection 验证投影只保留新的安全错误字段，正文显示一次具体原因，旧账本兼容且非法字节计数被隔离。
func TestRuntimeAIRequestErrorDetailProjection(t *testing.T) {
	at := time.Now().UTC().Format(time.RFC3339Nano)
	base := cloudruntime.AIRequestAttempt{Layer: "client", Phase: "end", ClientRequestID: "airq_detail", AnalysisGroupID: "aigr_detail", ClientAttempt: 1, MaxAttempts: 3, Operation: "AI 请求", Provider: "external", StartedAt: at, FinishedAt: at, Result: "failed", Reason: "http_status", HTTPStatus: 503, ErrorDetail: "模型容量暂不足", ErrorCode: "ServiceUnavailable", ErrorDetailTruncated: true, ResponseBytes: 5678}
	for _, mode := range []string{"detail", "legacy", "unsafe_text", "negative_bytes"} {
		t.Run(mode, func(t *testing.T) {
			event := base
			switch mode {
			case "legacy":
				event.ErrorDetail, event.ErrorCode, event.ErrorDetailTruncated, event.ResponseBytes = "", "", false, 0
			case "unsafe_text":
				event.ErrorDetail += ` api_key="PRIVATE_KEY"`
			case "negative_bytes":
				event.ResponseBytes = -1
			}
			raw, _ := json.Marshal(map[string]any{"title": "ai_request_attempt", "task_id": "task", "position_id": runtimeAIPositionForTest, "occurred_at": at, "description": "PRIVATE_DESCRIPTION", "ai_request_attempt": event})
			item, ok := aiRequestPositionLog("event", runtimeAIEmailForTest, runtimeAIPositionForTest, raw)
			if mode == "negative_bytes" {
				if ok {
					t.Fatal("非法响应字节没有被隔离")
				}
				return
			}
			var record positionDiagnosticEvent
			var output struct {
				Attempt cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
			}
			if !ok || json.Unmarshal([]byte(item.Message), &record) != nil || json.Unmarshal(record.Output, &output) != nil || !reflect.DeepEqual(output.Attempt, cloudruntime.NormalizeAIRequestAttempt(event)) {
				t.Fatal("投影丢失安全错误详情字段或不兼容旧事件")
			}
			if strings.Contains(item.Message, "PRIVATE_") || record.ErrorMessage != record.Message || mode != "legacy" && strings.Count(record.ErrorMessage, "模型容量暂不足") != 1 {
				t.Fatal("投影泄露附加正文或没有显示一次具体原因")
			}
		})
	}
}

// runtimeAIChangesForTest 优先消费真实runLogger导出的四条回执，未显式指定时使用明确标注的合成样本。
func runtimeAIChangesForTest(t *testing.T) []AutoReplyStateChange {
	t.Helper()
	if path := os.Getenv("GOODHR_CLOUDSERVER_AI_EVIDENCE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var changes []AutoReplyStateChange
		if json.Unmarshal(raw, &changes) != nil || len(changes) != 4 {
			t.Fatal("真实runLogger回执必须含四条事件")
		}
		t.Log("source=real runLogger ServerAutoReplyTransaction export (four start/end records)")
		return changes
	}
	t.Log("source=synthetic fallback; real runLogger receipt not supplied")
	base := time.Now().UTC().Add(-time.Minute)
	changes := make([]AutoReplyStateChange, 0, 4)
	for i := 0; i < 4; i++ {
		at := base.Add(time.Duration(i) * time.Millisecond)
		a := cloudruntime.AIRequestAttempt{Layer: "client", Phase: "start", ClientRequestID: []string{"airq_first", "airq_first", "airq_second", "airq_second"}[i], AnalysisGroupID: "aigr_fixture", ClientAttempt: int64(i/2 + 1), MaxAttempts: 3, Operation: "候选人基础预分析", Model: "test-model", Provider: "external", StartedAt: at.Format(time.RFC3339Nano), Result: "started", Reason: "request_started"}
		if i%2 == 1 {
			a.Phase = "end"
			a.FinishedAt = at.Format(time.RFC3339Nano)
			a.Result = "success"
			a.Reason = "success"
			a.HTTPStatus = 200
			if i == 1 {
				a.Result = "failed"
				a.Reason = "http_status"
				a.HTTPStatus = 502
				a.WillRetry = true
				a.RetryReason = "http_status"
				a.BackoffMS = 500
				a.ErrorDetail, a.ErrorCode, a.ErrorDetailTruncated, a.ResponseBytes = "模型容量暂不足", "ServiceUnavailable", true, 5678
			}
		}
		p, _ := json.Marshal(map[string]any{"title": "ai_request_attempt", "task_id": "runtime-task", "position_id": runtimeAIPositionForTest, "run_id": "synthetic-run", "occurred_at": at.Format(time.RFC3339Nano), "ai_request_attempt": a})
		changes = append(changes, AutoReplyStateChange{Kind: "runtime_events", ID: []string{"synthetic-start-1", "synthetic-end-1", "synthetic-start-2", "synthetic-end-2"}[i], Payload: p, Revision: 1, Sequence: int64(i + 1), UpdatedAt: at.Format(time.RFC3339Nano)})
	}
	return changes
}

// runtimeAIHTTPFixture 让真实认证与岗位HTTP路由使用同一PG账号岗位，普通日志保留原内存实现以验证覆盖边界。
func runtimeAIHTTPFixture(t *testing.T) (*Server, http.Handler, string, *sql.DB) {
	t.Helper()
	db := autoReplyStateTestDB(t)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO tenants(id) VALUES($1)", []any{runtimeAITenantForTest}},
		{"INSERT INTO users(id,tenant_id,email) VALUES($1,$2,$3)", []any{runtimeAIOwnerForTest, runtimeAITenantForTest, runtimeAIEmailForTest}},
		{"INSERT INTO positions(id,user_id,name) VALUES($1,$2,'AI请求日志测试岗位')", []any{runtimeAIPositionForTest, runtimeAIOwnerForTest}},
	} {
		if _, err := db.Exec(statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, runtimeAIEmailForTest)
	oldPosition := createPositionForTest(t, routes, token)
	positions := server.positions.store.(*MemoryPositionStore)
	positions.mu.Lock()
	position := positions.positions[oldPosition]
	delete(positions.positions, oldPosition)
	position.ID = runtimeAIPositionForTest
	positions.positions[position.ID] = position
	positions.mu.Unlock()
	tenants := server.positionLogs.tenantStore.(*MemoryTenantStore)
	oldTenant, err := tenants.GetOrCreateTenant(runtimeAIEmailForTest)
	if err != nil {
		t.Fatal(err)
	}
	tenants.mu.Lock()
	members := tenants.members[oldTenant.ID]
	delete(tenants.members, oldTenant.ID)
	delete(tenants.tenants, oldTenant.ID)
	oldTenant.ID = runtimeAITenantForTest
	tenants.tenants[oldTenant.ID] = oldTenant
	tenants.members[oldTenant.ID] = members
	tenants.mu.Unlock()
	store := NewPostgresAutoReplyStore(db)
	server.autoReply.store = store
	server.positionLogs.runtimeStore = store
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", runtimeAIServiceTokenForTest)
	return server, routes, token, db
}

// runtimeAICommitForTest 经实际internalState加载版本并提交，禁止用直接写账本代替真实接收链。
func runtimeAICommitForTest(t *testing.T, routes http.Handler, tenant, user string, changes []AutoReplyStateChange) {
	t.Helper()
	call := func(path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+runtimeAIServiceTokenForTest)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		return w
	}
	loaded := call("/internal/auto-reply/state/load", map[string]any{"tenant_id": tenant, "user_id": user})
	var snapshot AutoReplyStateSnapshot
	if loaded.Code != 200 || json.Unmarshal(loaded.Body.Bytes(), &snapshot) != nil {
		t.Fatalf("state load status=%d", loaded.Code)
	}
	committed := call("/internal/auto-reply/state/commit", map[string]any{"tenant_id": tenant, "user_id": user, "expected_version": snapshot.Version, "changes": changes})
	if committed.Code != 200 {
		t.Fatalf("state commit status=%d body=%s", committed.Code, committed.Body.String())
	}
}

// runtimeAIListForTest 调用真实岗位日志HTTP接口，保留公开事件及稳定ID。
func runtimeAIListForTest(t *testing.T, routes http.Handler, token, suffix string) ([]runtimeAILogItem, bool) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/positions/"+runtimeAIPositionForTest+"/logs"+suffix, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	var body struct {
		Logs []runtimeAILogItem `json:"logs"`
		More bool               `json:"has_more"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	return body.Logs, body.More
}

// runtimeAIShareForTest 创建真实公开分享并读取快照，确保服务层合并而非仅本地列表可见。
func runtimeAIShareForTest(t *testing.T, routes http.Handler, token string) []runtimeAILogItem {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/positions/"+runtimeAIPositionForTest+"/logs/share", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	var share struct {
		Token string `json:"token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &share) != nil {
		t.Fatalf("share status=%d", w.Code)
	}
	public := httptest.NewRecorder()
	routes.ServeHTTP(public, httptest.NewRequest(http.MethodGet, "/api/public/position-log-shares/"+share.Token, nil))
	var body struct {
		Logs []runtimeAILogItem `json:"logs"`
	}
	if public.Code != 200 || json.Unmarshal(public.Body.Bytes(), &body) != nil {
		t.Fatal("public share failed")
	}
	return body.Logs
}

// runtimeAIAssertMetadataForTest 将每条投影与原始runLogger强类型元数据逐值比较，额外正文不能进入分享。
func runtimeAIAssertMetadataForTest(t *testing.T, items []runtimeAILogItem, changes []AutoReplyStateChange) {
	t.Helper()
	want := map[string]cloudruntime.AIRequestAttempt{}
	for _, change := range changes {
		var p struct {
			Attempt cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
		}
		if json.Unmarshal(change.Payload, &p) != nil {
			t.Fatal("bad source")
		}
		want["runtime_ai_"+change.ID] = p.Attempt
	}
	seen := map[string]bool{}
	for _, item := range items {
		expected, ok := want[item.ID]
		if !ok {
			continue
		}
		var p struct {
			Attempt cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
		}
		if json.Unmarshal(item.Event.Output, &p) != nil || !reflect.DeepEqual(p.Attempt, expected) {
			t.Fatal("原始AI请求元数据在岗位日志或分享丢失")
		}
		if item.Event.Message == "" || item.Event.Step != "ai_request_attempt" {
			t.Fatal("缺少可读摘要或阶段")
		}
		seen[item.ID] = true
	}
	if len(seen) != len(changes) {
		t.Fatalf("visible=%d want=%d", len(seen), len(changes))
	}
}

// TestAutoReplyAIRequestLedgerVisibleSharedAndCleared 验证真实服务器账本经HTTP写PG后可查询分享，覆盖上传不删除，清空只写水位且后续事件仍可见。
func TestAutoReplyAIRequestLedgerVisibleSharedAndCleared(t *testing.T) {
	_, routes, token, db := runtimeAIHTTPFixture(t)
	changes := runtimeAIChangesForTest(t)
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, changes)
	items, _ := runtimeAIListForTest(t, routes, token, "")
	if len(items) != 4 {
		t.Fatalf("initial logs=%d want=4", len(items))
	}
	runtimeAIAssertMetadataForTest(t, items, changes)
	runtimeAIAssertMetadataForTest(t, runtimeAIShareForTest(t, routes, token), changes)
	repeated, _ := runtimeAIListForTest(t, routes, token, "")
	for i := range items {
		if repeated[i].ID != items[i].ID {
			t.Fatal("重复查询产生了新日志ID")
		}
	}
	page1, more := runtimeAIListForTest(t, routes, token, "?limit=2")
	if !more || len(page1) != 2 {
		t.Fatal("分页第一页或has_more错误")
	}
	page2, more2 := runtimeAIListForTest(t, routes, token, "?limit=2&before="+url.QueryEscape(page1[0].Event.Timestamp))
	if more2 || len(page2) != 2 {
		t.Fatal("分页第二页错误")
	}
	runtimeAIAssertMetadataForTest(t, append(page1, page2...), changes)
	ordinary, _ := json.Marshal(map[string]any{"logs": []positionDiagnosticEvent{{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Category: "运行", Message: "普通设备日志", Input: json.RawMessage(`{}`), Output: json.RawMessage(`{}`)}}})
	replace := httptest.NewRequest(http.MethodPut, "/api/positions/"+runtimeAIPositionForTest+"/logs", bytes.NewReader(ordinary))
	replace.Header.Set("Authorization", "Bearer "+token)
	replaced := httptest.NewRecorder()
	routes.ServeHTTP(replaced, replace)
	if replaced.Code != 200 {
		t.Fatalf("replace status=%d", replaced.Code)
	}
	afterReplace, _ := runtimeAIListForTest(t, routes, token, "")
	if len(afterReplace) != 5 {
		t.Fatalf("replace erased ledger projection, logs=%d", len(afterReplace))
	}
	runtimeAIAssertMetadataForTest(t, runtimeAIShareForTest(t, routes, token), changes)
	clear := httptest.NewRequest(http.MethodDelete, "/api/positions/"+runtimeAIPositionForTest+"/logs", nil)
	clear.Header.Set("Authorization", "Bearer "+token)
	cleared := httptest.NewRecorder()
	routes.ServeHTTP(cleared, clear)
	if cleared.Code != 200 {
		t.Fatalf("clear status=%d body=%s", cleared.Code, cleared.Body.String())
	}
	afterClear, _ := runtimeAIListForTest(t, routes, token, "")
	if len(afterClear) != 0 {
		t.Fatal("Clear后旧运行事件重新出现")
	}
	ids := make([]string, 0, 4)
	for _, change := range changes {
		ids = append(ids, change.ID)
	}
	var kept int
	if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='runtime_events' AND id=ANY($3::text[])`, runtimeAITenantForTest, runtimeAIOwnerForTest, pq.Array(ids)).Scan(&kept); err != nil || kept != 4 {
		t.Fatalf("Clear删除权威事件 kept=%d err=%v", kept, err)
	}
	var next map[string]any
	if json.Unmarshal(changes[0].Payload, &next) != nil {
		t.Fatal("bad source")
	}
	at := time.Now().UTC().Add(time.Millisecond)
	next["occurred_at"] = at.Format(time.RFC3339Nano)
	raw, _ := json.Marshal(next)
	newChange := AutoReplyStateChange{Kind: "runtime_events", ID: "after-clear-ai-event", Payload: raw, Revision: 1, Sequence: 100, UpdatedAt: at.Format(time.RFC3339Nano)}
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, []AutoReplyStateChange{newChange})
	newVisible, _ := runtimeAIListForTest(t, routes, token, "")
	if len(newVisible) != 1 || newVisible[0].ID != "runtime_ai_after-clear-ai-event" {
		t.Fatal("清空后的新AI事件不可见")
	}
}

// TestAutoReplyAIRequestProjectionScopesAndInvalidPayload 验证服务端真实owner和tenant联查，不相信payload岗位；坏时间/类型只跳过该事件。
func TestAutoReplyAIRequestProjectionScopesAndInvalidPayload(t *testing.T) {
	server, routes, token, db := runtimeAIHTTPFixture(t)
	changes := runtimeAIChangesForTest(t)
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, changes)
	for _, foreign := range []struct{ tenant, user, email string }{{runtimeAITenantForTest, "30000000-0000-4000-8000-000000000002", "other-runtime@example.com"}, {"20000000-0000-4000-8000-000000000002", "30000000-0000-4000-8000-000000000003", "foreign-runtime@example.com"}} {
		if foreign.tenant != runtimeAITenantForTest {
			if _, err := db.Exec("INSERT INTO tenants(id) VALUES($1)", foreign.tenant); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec("INSERT INTO users(id,tenant_id,email) VALUES($1,$2,$3)", foreign.user, foreign.tenant, foreign.email); err != nil {
			t.Fatal(err)
		}
		runtimeAICommitForTest(t, routes, foreign.tenant, foreign.user, changes)
	}
	bad := make([]AutoReplyStateChange, 0, 2)
	for i, kind := range []string{"bad-time", "bad-metadata"} {
		var p map[string]any
		_ = json.Unmarshal(changes[0].Payload, &p)
		p["description"] = "PRIVATE-NOTICE-DO-NOT-PROJECT"
		p["raw_prompt"] = "PRIVATE-PROMPT"
		if i == 0 {
			p["occurred_at"] = "not-a-timestamp"
		} else {
			p["ai_request_attempt"].(map[string]any)["elapsed_ms"] = -1
		}
		raw, _ := json.Marshal(p)
		bad = append(bad, AutoReplyStateChange{Kind: "runtime_events", ID: kind, Payload: raw, Revision: 1, Sequence: int64(20 + i), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, bad)
	items, _ := runtimeAIListForTest(t, routes, token, "")
	if len(items) != 4 {
		t.Fatalf("forged or bad rows visible count=%d", len(items))
	}
	runtimeAIAssertMetadataForTest(t, runtimeAIShareForTest(t, routes, token), changes)
	foreignToken := loginForTest(t, routes, "other-runtime@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/positions/"+runtimeAIPositionForTest+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+foreignToken)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("跨用户读取status=%d", w.Code)
	}
	foreignClear := httptest.NewRequest(http.MethodDelete, "/api/positions/"+runtimeAIPositionForTest+"/logs", nil)
	foreignClear.Header.Set("Authorization", "Bearer "+foreignToken)
	foreignClearResponse := httptest.NewRecorder()
	routes.ServeHTTP(foreignClearResponse, foreignClear)
	if foreignClearResponse.Code != 404 {
		t.Fatal("原owner-only权限被扩大为跨账号清理")
	}
	var markers int
	if err := db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_entities WHERE kind='runtime_events' AND id=$1`, "ai-log-cutoff:"+runtimeAIPositionForTest).Scan(&markers); err != nil || markers != 0 {
		t.Fatal("未授权清理写入了cutoff")
	}

	if visible, err := server.positionLogs.runtimeStore.aiRequestPositionLogs("20000000-0000-4000-8000-000000000002", runtimeAIEmailForTest, runtimeAIPositionForTest, PositionLogQuery{}); err != nil || len(visible) != 0 {
		t.Fatal("跨租户投影未隔离")
	}
	var valid map[string]any
	_ = json.Unmarshal(changes[0].Payload, &valid)
	valid["description"] = "PRIVATE-NOTICE"
	valid["raw_prompt"] = "PRIVATE-PROMPT"
	valid["resume"] = "PRIVATE-RESUME"
	valid["api_key"] = "PRIVATE-CREDENTIAL"
	valid["ai_request_attempt"].(map[string]any)["extra_prompt"] = "PRIVATE-NESTED"
	raw, _ := json.Marshal(valid)
	projected, ok := aiRequestPositionLog("safe-extra", runtimeAIEmailForTest, runtimeAIPositionForTest, raw)
	if !ok || strings.Contains(projected.Message, "PRIVATE-") {
		t.Fatal("非白名单账本字段进入岗位日志")
	}
}
