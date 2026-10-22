// 本文件用独立临时Redis验证助手跨实例事务、TTL、回放游标及撤权历史隔离，不接触真实Redis或模型。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// reviewAssistantFixture 仅组装助手所需的合成依赖，不启动全站邮件或定时任务，便于独立竞态检测。
func reviewAssistantFixture(t *testing.T) (*Server, string, CandidateLibraryScope) {
	t.Helper()
	base, candidate, _, _ := resumeVectorsFixture(t)
	service := NewAssistantService(base.candidates, base.wallet, nil, base.search, base.store)
	server := &Server{candidates: base.candidates, aiWallet: base.wallet, assistant: service}
	return server, "vector-test-session", CandidateLibraryScope{TenantID: candidate.TenantID, UserEmail: candidate.UserEmail, IsAdmin: true}
}

// TestAssistantRedisCASDiscardsAbortedChanges 让另一实例精确打断WATCH事务，验证未提交的Runs不会混入下一快照。
func TestAssistantRedisCASDiscardsAbortedChanges(t *testing.T) {
	client := vectorRedisForTest(t)
	first, other := &redisAssistantStore{client: client}, &redisAssistantStore{client: client}
	ctx := context.Background()
	key := "assistant-cas-test"
	_, err := first.Create(ctx, key, assistantRecord{ID: "cas", Owner: "owner", TenantID: "team", ExpiresAt: time.Now().Add(time.Hour), Runs: map[string]*assistantRun{}})
	if err != nil {
		t.Fatal(err)
	}
	attempts := 0
	_, err = first.Update(ctx, key, func(record *assistantRecord) error {
		attempts++
		if attempts == 1 {
			record.Runs["uncommitted"] = &assistantRun{ID: "uncommitted", RequestID: "request", Status: "queued"}
			_, err := other.Update(ctx, key, func(actual *assistantRecord) error {
				actual.Messages = append(actual.Messages, AIMsg{Role: "user", Content: "另一实例真实提交"})
				return nil
			})
			return err
		}
		if record.Runs["uncommitted"] != nil {
			return errors.New("失败事务的幽灵任务污染了真实快照")
		}
		record.Messages = append(record.Messages, AIMsg{Role: "user", Content: "本实例重新提交"})
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("WATCH重试结果=%v attempts=%d", err, attempts)
	}
	actual, err := other.Get(ctx, key)
	if err != nil || len(actual.Runs) != 0 || len(actual.Messages) != 2 {
		t.Fatalf("事务内容异常=%+v err=%v", actual, err)
	}
}

// TestAssistantRedisReplayAndTTL 验证多个实例并发写事件不丢不重，读和输出不续期，另一个实例按游标回放。
func TestAssistantRedisReplayAndTTL(t *testing.T) {
	server, token, scope := reviewAssistantFixture(t)
	client := vectorRedisForTest(t)
	server.assistant.store = &redisAssistantStore{client: client}
	other := NewAssistantService(server.candidates, server.aiWallet, server.headhunter, server.assistant.search, server.assistant.vectorStore)
	other.store = &redisAssistantStore{client: client}
	key := assistantKey(scope, "redis-replay")
	runID := "redis-replay.run"
	expires := time.Now().Add(40 * time.Minute)
	_, err := server.assistant.store.Create(context.Background(), key, assistantRecord{ID: "redis-replay", Owner: scope.UserEmail, TenantID: scope.TenantID, ExpiresAt: expires, Runs: map[string]*assistantRun{runID: {ID: runID, Status: "running", References: map[string]int64{}}}})
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	problems := make(chan error, 4)
	for writer := 0; writer < 4; writer++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			service := server.assistant
			if index%2 == 1 {
				service = other
			}
			for number := 0; number < 8; number++ {
				var err error
				for attempt := 0; attempt < 16; attempt++ {
					err = service.appendEvent(scope, key, runID, "summary", map[string]any{"text": fmt.Sprintf("%d/%d", index, number)})
					if err == nil {
						break
					}
				}
				if err != nil {
					problems <- err
					return
				}
			}
		}(writer)
	}
	group.Wait()
	close(problems)
	for err := range problems {
		t.Fatal(err)
	}
	if err = other.appendEvent(scope, key, runID, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	record, err := server.assistant.store.Get(context.Background(), key)
	if err != nil || !record.ExpiresAt.Equal(expires) || len(record.Runs[runID].Events) != 33 {
		t.Fatalf("事件或TTL异常 err=%v", err)
	}
	for i, event := range record.Runs[runID].Events {
		if event.ID != strconv.Itoa(i+1) {
			t.Fatal("事件编号丢失或重复")
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/assistant/runs/"+runID+"/events?after=8", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	other.Route(response, request)
	if response.Code != 200 {
		t.Fatalf("回放状态=%d %s", response.Code, response.Body.String())
	}
	next := 9
	for _, line := range strings.Split(response.Body.String(), "\n") {
		if strings.HasPrefix(line, "id: ") {
			if line != "id: "+strconv.Itoa(next) {
				t.Fatal("跨实例回放不连续")
			}
			next++
		}
	}
	if next != 34 {
		t.Fatalf("回放末尾=%d", next)
	}
	again, _ := other.store.Get(context.Background(), key)
	if !again.ExpiresAt.Equal(expires) {
		t.Fatal("订阅读取续期了会话")
	}
}

// TestAssistantRedisUserMessageRenewsOnlyOnce 验证新消息续两小时，跨实例同请求重试不续期也不重复调用模型。
func TestAssistantRedisUserMessageRenewsOnlyOnce(t *testing.T) {
	server, token, scope := reviewAssistantFixture(t)
	client := vectorRedisForTest(t)
	first := server.assistant
	first.store = &redisAssistantStore{client: client}
	second := NewAssistantService(server.candidates, server.aiWallet, server.headhunter, first.search, first.vectorStore)
	second.store = &redisAssistantStore{client: client}
	key := assistantKey(scope, "redis-send")
	_, err := first.store.Create(context.Background(), key, assistantRecord{ID: "redis-send", Owner: scope.UserEmail, TenantID: scope.TenantID, ExpiresAt: time.Now().Add(time.Hour), Runs: map[string]*assistantRun{}})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate) }) })
	var calls atomic.Int32
	model := func(ctx context.Context, _ CandidateLibraryScope, _ []AIMsg, emit func(assistantModelEvent) error) error {
		calls.Add(1)
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := emit(assistantModelEvent{Type: "text", Text: "请告诉我岗位名称"}); err != nil {
			return err
		}
		return emit(assistantModelEvent{Type: "done"})
	}
	first.modelCall, second.modelCall = model, model
	post := func(service *AssistantService) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/assistant/sessions/redis-send/messages", strings.NewReader(`{"content":"找简历","request_id":"same-request"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		service.Route(response, request)
		return response
	}
	response := post(first)
	if response.Code != 202 {
		t.Fatalf("新消息状态=%d %s", response.Code, response.Body.String())
	}
	initial, _ := first.store.Get(context.Background(), key)
	if time.Until(initial.ExpiresAt) < 119*time.Minute {
		t.Fatal("新用户消息没有续期两小时")
	}
	response = post(second)
	if response.Code != 202 {
		t.Fatal("跨实例消息无法恢复原任务")
	}
	duplicate, _ := second.store.Get(context.Background(), key)
	if !duplicate.ExpiresAt.Equal(initial.ExpiresAt) || len(duplicate.Runs) != 1 {
		t.Fatal("重复请求改变了任务或截止时间")
	}
	release.Do(func() { close(gate) })
	run := waitAssistantRun(t, first, scope, "redis-send", initial.ActiveRun)
	if run.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("付费任务重复或未完成: %s/%d", run.Status, calls.Load())
	}
}

// TestAssistantInvalidHistoryNotSentToModel 验证已撤权或已删除候选人的旧工具正文不会从上一轮历史再次流入模型。
func TestAssistantInvalidHistoryNotSentToModel(t *testing.T) {
	server, _, scope := reviewAssistantFixture(t)
	key := assistantKey(scope, "history-access")
	now := time.Now()
	_, err := server.assistant.store.Create(context.Background(), key, assistantRecord{ID: "history-access", Owner: scope.UserEmail, TenantID: scope.TenantID, ExpiresAt: now.Add(assistantTTL), ActiveRun: "history-access.new", Messages: []AIMsg{{Role: "user", Content: "工具执行回执：PRIVATE_OLD_CANDIDATE_DETAIL"}, {Role: "assistant", Content: "PRIVATE_OLD_CANDIDATE_DETAIL"}, {Role: "user", Content: "重新找人"}}, Runs: map[string]*assistantRun{"history-access.old": {ID: "history-access.old", Content: "我需要Go工程师", Status: "completed", References: map[string]int64{"deleted-candidate": 1}, StartedAt: now.Add(-time.Minute)}, "history-access.new": {ID: "history-access.new", Content: "重新找人", Status: "queued", References: map[string]int64{}, StartedAt: now}}})
	if err != nil {
		t.Fatal(err)
	}
	server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, messages []AIMsg, emit func(assistantModelEvent) error) error {
		raw, _ := json.Marshal(messages)
		if strings.Contains(string(raw), "PRIVATE_OLD_CANDIDATE_DETAIL") {
			t.Error("失效候选人通过历史记录泄露给模型")
		}
		if !strings.Contains(string(raw), "我需要Go工程师") {
			t.Error("重建历史丢失真实用户条件")
		}
		if err := emit(assistantModelEvent{Type: "text", Text: "我重新查询岗位"}); err != nil {
			return err
		}
		return emit(assistantModelEvent{Type: "done"})
	}
	if err = server.assistant.runConversation(context.Background(), scope, key, "history-access.new", func(string, map[string]any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	record, _ := server.assistant.store.Get(context.Background(), key)
	raw, _ := json.Marshal(record.Messages)
	if strings.Contains(string(raw), "PRIVATE_OLD_CANDIDATE_DETAIL") {
		t.Fatal("失效工具历史仍留在后续模型上下文")
	}
}

// TestAssistantToolArgumentsStrict 验证工具执行前拒绝重复键、未知字段和错误类型，并把错误及原调用历史回送至第五次截止。
func TestAssistantToolArgumentsStrict(t *testing.T) {
	for _, raw := range []string{`{"query":"Go","query":"Java"}`, `{"query":"Go","tenant_id":"other"}`, `{"query":123}`, `{"query":"Go"}{"id":"other"}`} {
		t.Run(raw, func(t *testing.T) {
			server, _, scope := reviewAssistantFixture(t)
			key := assistantKey(scope, "strict-tool")
			runID := "strict-tool.run"
			_, err := server.assistant.store.Create(context.Background(), key, assistantRecord{ID: "strict-tool", Owner: scope.UserEmail, TenantID: scope.TenantID, ExpiresAt: time.Now().Add(assistantTTL), Runs: map[string]*assistantRun{runID: {ID: runID, Status: "queued", Content: "找岗位", References: map[string]int64{}}}, Messages: []AIMsg{{Role: "user", Content: "找岗位"}}})
			if err != nil {
				t.Fatal(err)
			}
			calls, results, retries := 0, 0, 0
			server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, messages []AIMsg, emit func(assistantModelEvent) error) error {
				calls++
				if calls > 1 {
					body, _ := json.Marshal(messages)
					if !strings.Contains(string(body), "工具执行回执") || !strings.Contains(string(body), "error") {
						t.Error("没有把错误和历史回送给模型")
					}
				}
				return emit(assistantModelEvent{Type: "tool", Name: "search_positions", Arguments: json.RawMessage(raw)})
			}
			err = server.assistant.runConversation(context.Background(), scope, key, runID, func(kind string, data map[string]any) error {
				if kind == "tool_result" {
					results++
					if data["error"] == nil {
						t.Error("非法参数没有错误回执")
					}
				}
				if kind == "retry" {
					retries++
				}
				return nil
			})
			if err == nil || calls != 5 || results != 5 || retries != 4 {
				t.Fatalf("参数执行或重试不符合合同: err=%v calls=%d results=%d retries=%d", err, calls, results, retries)
			}
		})
	}
}

// TestAssistantCurrentRunRechecksBeforeModel 验证本轮新引用在下一次模型调用前已撤权时立即停止，不能等正文输出才发现。
func TestAssistantCurrentRunRechecksBeforeModel(t *testing.T) {
	server, _, scope := reviewAssistantFixture(t)
	key, runID := assistantKey(scope, "midrun-access"), "midrun-access.run"
	_, err := server.assistant.store.Create(context.Background(), key, assistantRecord{ID: "midrun-access", Owner: scope.UserEmail, TenantID: scope.TenantID, ExpiresAt: time.Now().Add(assistantTTL), Runs: map[string]*assistantRun{runID: {ID: runID, Status: "running", Content: "继续找人", References: map[string]int64{}}}, Messages: []AIMsg{{Role: "user", Content: "继续找人"}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, _ []AIMsg, _ func(assistantModelEvent) error) error {
		calls++
		_, err := server.assistant.store.Update(context.Background(), key, func(record *assistantRecord) error {
			record.Runs[runID].References["removed-after-tool"] = 1
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return errors.New("合成可修复协议错误")
	}
	err = server.assistant.runConversation(context.Background(), scope, key, runID, func(string, map[string]any) error { return nil })
	var fatal assistantFatalError
	if !errors.As(err, &fatal) || calls != 1 {
		t.Fatalf("本轮撤权后仍向模型送历史: err=%v calls=%d", err, calls)
	}
}

// TestAssistantCancelledRunDoesNotCallModel 验证跨实例取消排队任务后，即使本实例没有取消函数也不再发付费请求。
func TestAssistantCancelledRunDoesNotCallModel(t *testing.T) {
	server, _, scope := reviewAssistantFixture(t)
	key, runID := assistantKey(scope, "cancelled-model"), "cancelled-model.run"
	_, err := server.assistant.store.Create(context.Background(), key, assistantRecord{ID: "cancelled-model", ExpiresAt: time.Now().Add(assistantTTL), Runs: map[string]*assistantRun{runID: {ID: runID, Status: "cancelled", References: map[string]int64{}}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server.assistant.modelCall = func(_ context.Context, _ CandidateLibraryScope, _ []AIMsg, _ func(assistantModelEvent) error) error {
		calls++
		return nil
	}
	err = server.assistant.runConversation(context.Background(), scope, key, runID, func(string, map[string]any) error { return context.Canceled })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("已取消任务仍调用模型: err=%v calls=%d", err, calls)
	}
}
