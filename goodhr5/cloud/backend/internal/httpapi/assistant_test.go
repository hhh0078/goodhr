// 本文件验证助手严格JSON、五次修复、独立任务、消息幂等、事件续传与两小时只由新消息续期。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// assistantFixture 创建完全合成的内置AI账号，不连接生产数据或付费模型。
func assistantFixture(t *testing.T) (*Server, http.Handler, string, CandidateLibraryScope) {
	t.Helper()
	t.Setenv("GOODHR_PG_DSN", "")
	t.Setenv("GOODHR_REDIS_ADDR", "")
	t.Setenv("GOODHR_SMTP_HOST", "")
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	server := mustNewServer(t)
	routes := server.Routes()
	email := "assistant@example.com"
	token := loginForTest(t, routes, email)
	saveBuiltinAIConfigForTest(t, server.systemConfigs, builtinAIPolicyConfigForTest())
	if _, err := server.aiWallet.ConfigureUserBuiltinAI(email); err != nil {
		t.Fatal(err)
	}
	if _, err := server.aiWallet.wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: 100000, Category: "manual_adjust", Reason: "合成测试余额"}); err != nil {
		t.Fatal(err)
	}
	tenant, err := server.tenants.store.GetOrCreateTenant(email)
	if err != nil {
		t.Fatal(err)
	}
	return server, routes, token, CandidateLibraryScope{TenantID: tenant.ID, UserEmail: email, IsAdmin: true}
}

// createAssistantForTest 通过正式HTTP合同创建一个本地会话。
func createAssistantForTest(t *testing.T, routes http.Handler, token, id string) {
	t.Helper()
	status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions", map[string]any{"id": id, "history": []any{}})
	if status != 200 {
		t.Fatalf("创建会话失败: %d", status)
	}
}

// waitAssistantRun 等待合成后台任务到达终态，超时给出真实状态而不是只检查200。
func waitAssistantRun(t *testing.T, s *AssistantService, scope CandidateLibraryScope, sessionID, runID string) assistantRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		record, err := s.store.Get(context.Background(), assistantKey(scope, sessionID))
		if err != nil {
			t.Fatal(err)
		}
		run := record.Runs[runID]
		if run != nil && (run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled") {
			return *run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("后台任务没有到达终态")
	return assistantRun{}
}

// TestAssistantStrictJSON 拒绝夹杂正文、围栏、重复控制字段、未知字段与拼接对象。
func TestAssistantStrictJSON(t *testing.T) {
	for _, raw := range []string{"解释{\"type\":\"done\"}", "```json\n{\"type\":\"done\"}\n```", `{"type":"done","type":"tool"}`, `{"type":"done","unknown":true}`, `{"type":"done"}{"type":"done"}`, `{"type":"tool","arguments":{"a":1,"a":2}}`} {
		var event assistantModelEvent
		if strictAssistantJSON([]byte(raw), &event) == nil {
			t.Errorf("接受了不符合合同的JSON: %s", raw)
		}
	}
	var event assistantModelEvent
	if err := strictAssistantJSON([]byte(" \n{\"type\":\"done\"}\n"), &event); err != nil {
		t.Fatal(err)
	}
}

// TestAssistantRequestRequiresObject 拒绝null和数组请求，避免空控制对象被误当成有效会话创建。
func TestAssistantRequestRequiresObject(t *testing.T) {
	for _, raw := range []string{"null", "[]", "\"message\""} {
		request := httptest.NewRequest(http.MethodPost, "/api/assistant/sessions", strings.NewReader(raw))
		var input struct {
			ID string `json:"id"`
		}
		if err := readAssistantBody(httptest.NewRecorder(), request, &input); err == nil {
			t.Errorf("非对象请求被接受: %s", raw)
		}
	}
}

// TestAssistantTaskIdempotencyAndResume 验证断开请求不停止后台任务、同消息不重复执行、游标补发不重复。
func TestAssistantTaskIdempotencyAndResume(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	createAssistantForTest(t, routes, token, "session-test")
	gate := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	server.assistant.modelCall = func(ctx context.Context, _ CandidateLibraryScope, _ []AIMsg, emit func(assistantModelEvent) error) error {
		calls.Add(1)
		close(started)
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := emit(assistantModelEvent{Type: "summary", Text: "正在核对岗位"}); err != nil {
			return err
		}
		if err := emit(assistantModelEvent{Type: "text", Text: "请告诉我岗位名称。"}); err != nil {
			return err
		}
		return emit(assistantModelEvent{Type: "done"})
	}
	input := map[string]any{"content": "帮我找简历", "request_id": "request-one"}
	status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions/session-test/messages", input)
	if status != 202 {
		t.Fatalf("任务应立即202: %d %v", status, data)
	}
	runID := data["run_id"].(string)
	<-started
	status, duplicate := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions/session-test/messages", input)
	if status != 202 || duplicate["run_id"] != runID {
		t.Fatal("相同消息没有恢复原任务")
	}
	status, _ = candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions/session-test/messages", map[string]any{"content": "不同内容", "request_id": "request-one"})
	if status != 409 {
		t.Fatal("请求编号被错误复用")
	}
	close(gate)
	run := waitAssistantRun(t, server.assistant, scope, "session-test", runID)
	if run.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("任务未完成或重复执行: %s %d", run.Status, calls.Load())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/assistant/runs/"+runID+"/events?after=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	routes.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Header().Get("Content-Type"), "event-stream") || strings.Contains(res.Body.String(), "id: 1\n") || !strings.Contains(res.Body.String(), `"type":"completed"`) {
		t.Fatalf("事件续传不正确: %d %s", res.Code, res.Body.String())
	}
	status, snapshot := candidateTestRequest(t, routes, token, http.MethodGet, "/api/assistant/sessions/session-test", nil)
	if status != 200 || len(snapshot["runs"].([]any)) != 1 {
		t.Fatal("刷新无法按请求编号恢复任务元信息")
	}
	other := loginForTest(t, routes, "assistant-other@example.com")
	status, _ = candidateTestRequest(t, routes, other, http.MethodGet, "/api/assistant/sessions/session-test", nil)
	if status != 410 {
		t.Fatal("跨账号读到了会话")
	}
}

// TestAssistantRepairsAtMostFive 验证错误和历史回送后第5次仍失败必须终止，坏正文不进入事件流。
func TestAssistantRepairsAtMostFive(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	createAssistantForTest(t, routes, token, "repair-session")
	var calls atomic.Int32
	var missingHistory atomic.Bool
	server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, messages []AIMsg, _ func(assistantModelEvent) error) error {
		number := calls.Add(1)
		if number > 1 {
			found := false
			for _, message := range messages {
				if strings.Contains(message.Content, "合成格式错误") {
					found = true
				}
			}
			if !found {
				missingHistory.Store(true)
			}
		}
		return errors.New("合成格式错误")
	}
	status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions/repair-session/messages", map[string]any{"content": "找人", "request_id": "repair-request"})
	if status != 202 {
		t.Fatal(status)
	}
	run := waitAssistantRun(t, server.assistant, scope, "repair-session", data["run_id"].(string))
	if run.Status != "failed" || calls.Load() != 5 || missingHistory.Load() {
		t.Fatalf("修复次数或历史不正确: status=%s calls=%d history=%v", run.Status, calls.Load(), missingHistory.Load())
	}
	for _, event := range run.Events {
		if event.Type == "text" {
			t.Fatal("错误文本被当作正文接收")
		}
	}
}

// TestAssistantTTLAndRevocation 验证输出和读取不续期，旧候选人引用不再可见时拒绝回放。
func TestAssistantTTLAndRevocation(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	createAssistantForTest(t, routes, token, "ttl-session")
	key := assistantKey(scope, "ttl-session")
	expires := time.Now().Add(time.Hour)
	_, err := server.assistant.store.Update(context.Background(), key, func(record *assistantRecord) error {
		record.ExpiresAt = expires
		record.Runs["ttl-session.test"] = &assistantRun{ID: "ttl-session.test", Status: "running", References: map[string]int64{}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = server.assistant.appendEvent(scope, key, "ttl-session.test", "phase", map[string]any{"text": "进度"}); err != nil {
		t.Fatal(err)
	}
	record, _ := server.assistant.store.Get(context.Background(), key)
	if !record.ExpiresAt.Equal(expires) {
		t.Fatal("输出意外刷新了有效期")
	}
	_, err = server.assistant.store.Update(context.Background(), key, func(record *assistantRecord) error {
		record.Runs["ttl-session.test"].References["deleted-candidate"] = 1
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/assistant/runs/ttl-session.test/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	routes.ServeHTTP(res, req)
	if res.Code != 403 || strings.Contains(res.Body.String(), `"type":"phase"`) {
		t.Fatal("撤权后仍可回放旧资料")
	}
	memory := server.assistant.store.(*memoryAssistantStore)
	memory.now = func() time.Time { return expires.Add(time.Second) }
	if _, err = memory.Get(context.Background(), key); !errors.Is(err, ErrNotFound) {
		t.Fatal("过期任务仍可读取")
	}
}

// TestAssistantUncertainEmbeddingStopsOuterRetry 验证向量付费结果未知时停止整轮，不重复已经成功的岗位解析。
func TestAssistantUncertainEmbeddingStopsOuterRetry(t *testing.T) {
	server, routes, token, scope := assistantFixture(t)
	server.assistant.search = &recommendationSearchFixture{}
	customer, err := server.headhunter.SaveCustomer(scope, HeadhunterCustomer{Name: "向量中断测试甲方"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.headhunter.SaveJob(scope, HeadhunterJob{CustomerID: customer.ID, Name: "Go工程师", Requirements: "熟悉Go"})
	if err != nil {
		t.Fatal(err)
	}
	var embeddingCalls, parseCalls, conversationCalls atomic.Int32
	server.aiWallet.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/embeddings") {
			embeddingCalls.Add(1)
			return nil, errors.New("合成向量传输中断")
		}
		parseCalls.Add(1)
		return recommendationChatResponse(assistantRequirements{Requirements: []assistantRequirement{{Text: "熟悉Go", Type: "must_have", Source: "熟悉Go", Explicit: true}}}), nil
	})}
	server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, _ []AIMsg, emit func(assistantModelEvent) error) error {
		if conversationCalls.Add(1) == 1 {
			args, _ := json.Marshal(map[string]string{"id": job.ID})
			return emit(assistantModelEvent{Type: "tool", Name: "search_positions", Arguments: args})
		}
		args, _ := json.Marshal(map[string]string{"job_id": job.ID})
		return emit(assistantModelEvent{Type: "tool", Name: "search_candidates", Arguments: args})
	}
	createAssistantForTest(t, routes, token, "uncertain-embedding")
	status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/assistant/sessions/uncertain-embedding/messages", map[string]any{"content": "按Go岗位找简历", "request_id": "uncertain-request"})
	if status != 202 {
		t.Fatalf("创建任务失败: %d %v", status, data)
	}
	run := waitAssistantRun(t, server.assistant, scope, "uncertain-embedding", data["run_id"].(string))
	if run.Status != "failed" || conversationCalls.Load() != 2 || parseCalls.Load() != 1 || embeddingCalls.Load() != 1 {
		t.Fatalf("不确定付费调用被重试: status=%s conversation=%d parse=%d embedding=%d", run.Status, conversationCalls.Load(), parseCalls.Load(), embeddingCalls.Load())
	}
}
