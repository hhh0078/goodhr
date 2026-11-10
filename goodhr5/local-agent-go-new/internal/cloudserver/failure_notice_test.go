// Package cloudserver 验证服务器步骤故障经过实际账本后，能由原设备补领安全小票。
package cloudserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// TestContactListBusinessFailureHasSafeAudit 验证成功扫描之后的业务合同错误以实际列表阶段入库，且只保留固定安全原因。
func TestContactListBusinessFailureHasSafeAudit(t *testing.T) {
	r, fixture, _, _, _, _ := serverGoalFixture(t)
	r.currentStep = "initialize_auto_reply_page"
	logger := &runLogger{run: r}
	cause := failurediag.Wrap(errors.New("PRIVATE-CANDIDATE https://private.invalid?token=PRIVATE-KEY"), failurediag.Detail{Source: "联系人列表", Operation: "核对变化", Reason: "联系人稳定编号或变化摘要缺失", Impact: "本轮没有打开候选人或发送消息"})
	logger.Step("task", "auto_reply", "review_contact_list", "failed", time.Now(), cause)
	if r.currentStep != "review_contact_list" || safeRunFailure(r.currentStep) == safeRunFailure("initialize_auto_reply_page") {
		t.Fatalf("列表合同错误仍指向页面初始化：%s", r.currentStep)
	}
	found := false
	for key, entry := range fixture.data {
		if strings.HasPrefix(key, "runtime_events|") {
			raw := string(entry.Payload)
			if strings.Contains(raw, "review_contact_list") && strings.Contains(raw, "联系人稳定编号或变化摘要缺失") && strings.Contains(raw, "failure_diagnostics") {
				found = true
			}
			if strings.Contains(raw, "PRIVATE-") || strings.Contains(raw, "private.invalid") {
				t.Fatal("技术错误原文进入用户审计")
			}
		}
	}
	if !found {
		t.Fatal("列表业务失败没有生成可审计安全事件")
	}
}

// TestFailureNoticeStoredBeforeReplay 核对真实服务器事件、序号、重复补领和旧设备兼容性。
func TestFailureNoticeStoredBeforeReplay(t *testing.T) {
	r, fixture, conv, _, _, _ := serverGoalFixture(t)
	logger := &runLogger{run: r}
	logger.BindAutoReplyConversation(conv.ID, "p")
	reason := failurediag.Detail{Source: "内置 AI 代理", Reason: "等待响应头超时", HTTPStatus: 504}
	logger.Step("task", "auto_reply", "ai", "failed", time.Now(), failurediag.Wrap(errors.New("private-candidate-secret"), reason))
	now := int64(0)
	pay := r.nextPayload(nil, false, &now)
	raw, e := json.Marshal(pay)
	if e != nil || !strings.Contains(string(raw), "等待响应头超时") || strings.Contains(string(raw), "private-candidate") || strings.Contains(string(raw), conv.ID) {
		t.Fatalf("补领内容越界或缺失：%s", raw)
	}
	db := fixture.data["runtime_events|failure:run:00000000000000000001"]
	if !strings.Contains(string(db.Payload), "failure_diagnostics") || strings.Contains(string(db.Payload), "private-candidate") {
		t.Fatalf("权威事件不正确：%s", db.Payload)
	}
	now = 1
	if _, ok := r.nextPayload(nil, false, &now)["failures"]; ok {
		t.Fatal("已收到的小票被重复投递")
	}
	if _, ok := r.nextPayload(nil, false, nil)["failures"]; ok {
		t.Fatal("旧客户端收到扩展字段")
	}
}

// TestFailureNoticeScopedOrigin 核对释放会话后的来源和无法归属岗位的全局故障，后台警告保留严重级别。
func TestFailureNoticeScopedOrigin(t *testing.T) {
	r, _, conv, _, _, _ := serverGoalFixture(t)
	r.position = "startup"
	logger := &runLogger{run: r}
	logger.BindAutoReplyConversation(conv.ID, "other")
	d := failurediag.Detail{Source: "内置 AI 代理", Reason: "等待响应头超时"}
	cause := failurediag.Wrap(errors.New("internal"), d)
	logger.Step("task", "auto_reply", "server_run_stopped", "failed", time.Time{}, cause)
	after := int64(0)
	if _, ok := r.nextPayload(nil, false, &after)["failures"]; ok {
		t.Fatal("岗位未知的全局超时被发给了岗位")
	}
	logger.BindAutoReplyConversation("", "")
	logger.Step("task", "auto_reply", "server_run_stopped", "failed", time.Time{}, &auto_reply.ScopedFailure{Cause: cause, Position: "actual", Conversation: conv.ID})
	logger.ReportAnalysis("task", shared.AnalysisStatus{Stage: "ai", Phase: "warning", PositionID: "background", FailureDiagnostics: []failurediag.Detail{d}})
	tickets := r.nextPayload(nil, false, &after)["failures"].([]cloud.AutoReplyFailureNotice)
	if len(tickets) != 2 || tickets[0].PositionID != "actual" || tickets[1].PositionID != "background" || tickets[1].Status != "warning" {
		t.Fatalf("岗位或严重级别错误：%+v", tickets)
	}
}

// TestRestartedRunReplaysOnlyDiagnostics 验证无内存会话时仅向受信事务服务查询匹配的设备，历史浏览器命令不会重新派发。
func TestRestartedRunReplaysOnlyDiagnostics(t *testing.T) {
	run := strings.Repeat("d", 32)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/auto-reply/state/failures" || r.Header.Get("Authorization") != "Bearer 1234567890abcdef" {
			t.Errorf("错误恢复入口 %s", r.URL.Path)
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["tenant_id"] != "tenant" || in["user_id"] != "user" || in["run_id"] != run || in["after_failure"] != float64(0) {
			t.Error("恢复请求缺少真实范围")
		}
		if in["machine_id"] != "machine" {
			_ = json.NewEncoder(w).Encode(map[string]any{"found": false})
			return
		}
		_, _ = w.Write([]byte(`{"found":true,"failures":[{"sequence":1,"position_id":"p","step":"ai","status":"error","failure_diagnostics":[{"source":"AI","reason":"timeout"}]}]}`))
	}))
	defer backend.Close()
	svc, err := New(backend.URL, "1234567890abcdef", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, machine := range []string{"machine", "other"} {
		req := httptest.NewRequest("POST", "/api/auto-reply/runtime/next", strings.NewReader(`{"run_id":"`+run+`","after_failure":0}`))
		req.Header.Set("X-GoodHR-Runtime-Service", "1234567890abcdef")
		req.Header.Set("X-GoodHR-Runtime-Tenant", "tenant")
		req.Header.Set("X-GoodHR-Runtime-User", "user")
		req.Header.Set("X-GoodHR-Machine-ID", machine)
		req.Header.Set("Authorization", "Bearer user")
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, req)
		if machine == "machine" {
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"command":null`) || !strings.Contains(w.Body.String(), `"reason":"timeout"`) {
				t.Fatal(w.Code, w.Body.String())
			}
		} else if w.Code != 404 || strings.Contains(w.Body.String(), "timeout") {
			t.Fatal("其他设备能补领", w.Code)
		}
	}
}
