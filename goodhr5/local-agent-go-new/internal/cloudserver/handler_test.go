// Package cloudserver 本文件通过两个真实 HTTP 服务验证代理认证、云任务领取、设备回执、跨设备隔离和版本提交。
package cloudserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

type stateEndpointFixture struct {
	beforeCommit func([]storage.AutoReplyStateChange) error
	mu           sync.Mutex
	version      int64
	data         map[string]storage.AutoReplyStateChange
}

// handle 模拟实际云 API 契约，检查所有状态请求均携带服务凭证和已认证租户。
func (f *stateEndpointFixture) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/api/auth/me":
		output(w, 200, map[string]any{"user": map[string]string{"id": "user"}})
	case "/api/auto-reply/agent/positions/p/snapshot":
		output(w, 200, cloud.AutoReplyPositionSnapshot{Position: cloud.AutoReplyPosition{ID: "p", PlatformID: "liepin"}, Config: cloud.PositionAutoReplyConfig{PositionID: "p", Enabled: true}, CompanyProfile: cloud.CompanyProfile{ID: "co"}, Subscription: cloud.AutoReplySubscription{Active: true, AllowAutoReply: true}})
	case "/api/config/effective-ai":
		output(w, 200, map[string]any{"config": cloud.AIConfig{APIKey: "fixture", Model: "fixture", BaseURL: "http://127.0.0.1"}})
	case "/api/positions/p":
		output(w, 200, map[string]any{"position": cloud.PositionSnapshot{ID: "p", PlatformID: "liepin"}})
	case "/api/auto-reply/agent/positions":
		output(w, 200, map[string]any{"positions": []any{}})
	case "/internal/auto-reply/state/load", "/internal/auto-reply/state/commit":
		if r.Header.Get("Authorization") != "Bearer 1234567890abcdef" {
			http.Error(w, "forbidden", 403)
			return
		}
		var req struct {
			Tenant   string                         `json:"tenant_id"`
			User     string                         `json:"user_id"`
			Expected *int64                         `json:"expected_version"`
			Known    *int64                         `json:"known_version"`
			Changes  []storage.AutoReplyStateChange `json:"changes"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Tenant != "tenant" || req.User != "user" {
			http.Error(w, "scope", 403)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if req.Expected == nil {
			if req.Known != nil && *req.Known == f.version {
				w.WriteHeader(304)
				return
			}
			snapshot := storage.AutoReplyStateSnapshot{Version: f.version}
			for _, v := range f.data {
				snapshot.Entities = append(snapshot.Entities, v)
			}
			output(w, 200, snapshot)
			return
		}
		if *req.Expected != f.version {
			http.Error(w, "conflict", 409)
			return
		}
		if f.beforeCommit != nil {
			if err := f.beforeCommit(req.Changes); err != nil {
				http.Error(w, "commit refused", 409)
				return
			}
		}
		for _, v := range req.Changes {
			key := v.Kind + "|" + v.ID
			if v.Deleted {
				delete(f.data, key)
			} else {
				f.data[key] = v
			}
		}
		f.version++
		output(w, 200, map[string]int64{"version": f.version})
	default:
		http.NotFound(w, r)
	}
}

// TestRuntimeRequiresPGAndDeviceAck 检查启动由云端接管，真实命令只交给授权设备，回执完成后云端留下审计。
func TestRuntimeRequiresPGAndDeviceAck(t *testing.T) {
	f := &stateEndpointFixture{data: map[string]storage.AutoReplyStateChange{}}
	backend := httptest.NewServer(http.HandlerFunc(f.handle))
	defer backend.Close()
	service, err := New(backend.URL, "1234567890abcdef", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := httptest.NewServer(service)
	defer server.Close()
	post := func(action, secret, device string, in any) (int, map[string]any) {
		body, _ := json.Marshal(in)
		r, _ := http.NewRequest("POST", server.URL+"/api/auto-reply/runtime/"+action, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer usertoken")
		r.Header.Set("X-GoodHR-Runtime-Tenant", "tenant")
		r.Header.Set("X-GoodHR-Runtime-User", "user")
		r.Header.Set("X-GoodHR-Runtime-Service", secret)
		r.Header.Set("X-GoodHR-Machine-ID", device)
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, out
	}
	cred := "1234567890abcdef"
	start := map[string]string{"task_id": "fixture", "position_id": "p", "platform_id": "liepin", "platform_account_id": "account"}
	if code, _ := post("start", "bad", "machine", start); code != 403 {
		t.Fatalf("missing proxy guard %d", code)
	}
	code, out := post("start", cred, "machine", start)
	if code != 200 {
		t.Fatal(code, out)
	}
	id := out["run_id"].(string)
	if id == "" {
		t.Fatal("missing run")
	}
	if code, out = post("start", cred, "machine", start); code != 200 || out["run_id"] != id {
		t.Fatalf("duplicate start %+v", out)
	}
	start["platform_account_id"] = shared.AutoReplyAccountKey("liepin", "account")
	if code, out = post("start", cred, "machine", start); code != 200 || out["run_id"] != id {
		t.Fatal("规范账号与标签启动了第二份任务", code, out)
	}
	if accountKey("liepin", "account") != accountKey("liepin", start["platform_account_id"]) {
		t.Fatal("账号别名没有共用租约")
	}
	if code, _ = post("start", cred, "other", start); code != 409 {
		t.Fatal("account lease bypass", code)
	}
	if code, _ = post("next", cred, "other", map[string]string{"run_id": id}); code != 404 {
		t.Fatal("other device received command", code)
	}
	code, out = post("next", cred, "machine", map[string]string{"run_id": id})
	if code != 200 {
		t.Fatal(code, out)
	}
	cmd := out["command"].(map[string]any)
	if cmd["action"] != "initialize" {
		t.Fatalf("unexpected first command %+v", cmd)
	}
	code, out = post("complete", cred, "machine", map[string]any{"run_id": id, "result": replybridge.Result{ID: cmd["id"].(string), Value: json.RawMessage(`{}`)}})
	if code != 200 {
		t.Fatal(code, out)
	}
	// 无会话归属的运行审计只保留阶段和计数，具体候选可识别数据不能脱离候选清理范围。
	service.mu.Lock()
	currentRun := service.sessions[scopeKey(runScope{"tenant", "user", "machine"})]
	service.mu.Unlock()
	currentRun.mu.Lock()
	currentRun.analysis = shared.AnalysisStatus{Kind: "auto_reply", Phase: "running", Stage: "sync", CandidateName: "private-fixture-candidate", Reason: "private-fixture-reason", ProcessedCount: 1}
	currentRun.mu.Unlock()
	if err := currentRun.updateLease(context.Background(), "heartbeat"); err != nil {
		t.Fatal(err)
	}
	_, progress := post("status", cred, "machine", map[string]string{"run_id": id})
	if analysis, _ := progress["analysis"].(map[string]any); analysis["stage"] != "sync" || analysis["reason"] != "private-fixture-reason" {
		t.Fatal("无法查看当前真实进度", progress)
	}
	f.mu.Lock()
	durableProgress := string(f.data["runtime_runs|run:"+id].Payload)
	f.mu.Unlock()
	if strings.Contains(durableProgress, "private-fixture-") || !strings.Contains(durableProgress, `"stage":"sync"`) {
		t.Fatal("跨候选运行账本透出了候选资料或丢失了阶段")
	}
	if code, _ = post("stop", cred, "machine", map[string]string{"run_id": id}); code != 200 {
		t.Fatal("stop failed", code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out = post("status", cred, "machine", map[string]string{"run_id": id})
		if out["running"] == false {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if out["status"] != "stopped" {
		t.Fatal("runner failed to stop", out)
	}
	// 通过真实设备完成接口制造一次初始化失败，检查云状态与持久账本均返回同一安全提示。
	code, out = post("start", cred, "machine", start)
	if code != 200 {
		t.Fatal(code, out)
	}
	failedID := out["run_id"].(string)
	_, out = post("next", cred, "machine", map[string]string{"run_id": failedID})
	cmd = out["command"].(map[string]any)
	sensitive := "fixture-token=https://private.invalid"
	code, out = post("complete", cred, "machine", map[string]any{"run_id": failedID, "result": replybridge.Result{ID: cmd["id"].(string), Code: "ACTION_FAILED", Error: sensitive}})
	if code != 200 {
		t.Fatal(code, out)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, out = post("status", cred, "machine", map[string]string{"run_id": failedID})
		if out["running"] == false {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	wantError := safeRunFailure("initialize_auto_reply_page")
	if out["status"] != "failed" || out["error_message"] != wantError || out["current_step"] != "initialize_auto_reply_page" {
		t.Fatal("初始化失败缺少安全提示", out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var failedRecord runLease
	if err := json.Unmarshal(f.data["runtime_runs|run:"+failedID].Payload, &failedRecord); err != nil || failedRecord.Status != "failed" || failedRecord.ErrorMessage != wantError {
		t.Fatal("持久运行记录没有保留失败详情", failedRecord, err)
	}
	seen := false
	for _, r := range f.data {
		if strings.Contains(string(r.Payload), sensitive) {
			t.Fatal("技术报错泄露到运行记录")
		}
		if r.Kind == "runtime_commands" && strings.Contains(string(r.Payload), `"status":"completed"`) {
			var times commandState
			if err := json.Unmarshal(r.Payload, &times); err != nil || times.IssuedAt.IsZero() || times.StartedAt.Before(times.IssuedAt) || times.UpdatedAt.Before(times.StartedAt) {
				t.Fatal("命令的领取和完成时间不可核对", err)
			}
			seen = true
		}
	}
	if !seen {
		t.Fatal("missing persistent device receipt")
	}
}
