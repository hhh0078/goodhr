// Package greeting 文件作用：验证实际两轮采集、未消费并发预筛、零分、关键词、错误及提前评分耗时，不操作招聘网站。
package greeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// screeningFlowFixture 创建真实SQLite与已验证身份，明确关闭结构化简历输出也必须保存筛选记录。
func screeningFlowFixture(t *testing.T, url string) (*Flow, shared.PreparedTask) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "screening.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	disabled := false
	prepared := shared.PreparedTask{Request: shared.StartRequest{TaskID: "screening-task", Token: "memory-only-token", EnableGreet: &disabled}, Session: cloud.UserSession{UserID: "owner", Email: "owner@example.test", LoggedIn: true}, Position: cloud.PositionSnapshot{ID: "position", PlatformID: "boss", RequiresAI: true, MatchLimit: 1, CommonConfig: cloud.PositionCommonConfig{ModeDefault: "ai", DetailMode: "dom", OutputStructuredResume: false}, AI: cloud.AIConfig{BaseURL: url, APIKey: "fixture", Model: "fixture"}}}
	return &Flow{Store: store, AI: ai.New()}, prepared
}

// screeningJSONReply 输出合法合成模型结果，保留长原因供截断回归。
func screeningJSONReply(w http.ResponseWriter, score float64, reason string) {
	content, _ := json.Marshal(map[string]any{"score": score, "reason": reason})
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": string(content)}}}})
}

// screeningSSEChunk 发送一个真实流式分片，便于将评分可用与完整响应结束分开控制。
func screeningSSEChunk(w http.ResponseWriter, content string) {
	chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": content}}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
	w.(http.Flusher).Flush()
}

// TestScreeningUnconsumedPreviewsPersist 验证五个已实际执行但无人消费的并发预筛全部留存，并明确未执行二轮原因。
func TestScreeningUnconsumedPreviewsPersist(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		screeningJSONReply(w, 90, "卡片满足要求，等待详情")
	}))
	defer server.Close()
	flow, prepared := screeningFlowFixture(t, server.URL)
	candidates := []model.Candidate{}
	for i := 0; i < 5; i++ {
		candidates = append(candidates, model.Candidate{Index: i, Fingerprint: fmt.Sprint("card-", i), Name: fmt.Sprint("人选", i)})
	}
	_, done := flow.candidatePreviewsWithDone(context.Background(), prepared, candidates)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("并发预筛没有及时保存")
	}
	flow.finalizeScreeningTask(context.Background(), prepared, shared.Stats{Succeeded: 1}, nil)
	items, err := flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	if err != nil || len(items) != 5 || calls.Load() != 5 {
		t.Fatalf("未消费预筛丢失: records=%d calls=%d err=%v", len(items), calls.Load(), err)
	}
	ids := map[string]bool{}
	for _, item := range items {
		snapshot := item.Snapshot
		if ids[snapshot.AttemptID] {
			t.Fatal("不同人选复用了尝试编号")
		}
		ids[snapshot.AttemptID] = true
		if !snapshot.Completed || snapshot.First.Status != "passed" || snapshot.First.Score == nil || *snapshot.First.Score != 90 || snapshot.First.DurationMS == nil || snapshot.Second.Status != "not_executed" || snapshot.Second.DurationMS != nil || !strings.Contains(snapshot.Second.Reason, "上限") {
			t.Fatalf("并发结果或结束原因错误: %+v", snapshot)
		}
	}
}

// TestScreeningPreviewFailureAndZero 验证第一轮错误不再漏档，真实零分和完整原因都不会被缺省值吞掉。
func TestScreeningPreviewFailureAndZero(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			reason := strings.Repeat("完整不匹配原因", 30)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failure {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"invalid-json"}}]}`))
					return
				}
				screeningJSONReply(w, 0, reason)
			}))
			defer server.Close()
			flow, prepared := screeningFlowFixture(t, server.URL)
			results, done := flow.candidatePreviewsWithDone(context.Background(), prepared, []model.Candidate{{Fingerprint: "card", Name: "人选"}})
			item := <-results
			<-done
			record, err := flow.Store.ScreeningRecord(context.Background(), item.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			if !record.Snapshot.Completed || record.Snapshot.Second.StartedAt != nil {
				t.Fatal("第一轮未通过仍伪造了二轮")
			}
			first := record.Snapshot.First
			if failure {
				if item.Err == nil || first.Status != "failed" || first.Score != nil || first.Threshold != nil || first.Accepted != nil {
					t.Fatalf("错误伪造成评分: %+v", first)
				}
			} else if first.Status != "rejected" || first.Score == nil || *first.Score != 0 || first.Reason != reason || first.Accepted == nil || *first.Accepted {
				t.Fatalf("零分或完整原因丢失: %+v", first)
			}
		})
	}
}

// TestScreeningSecondRoundReusesAttemptAndEarlyTime 验证第二轮复用首轮编号，零分真实保留且耗时不等待完整结构化尾部。
func TestScreeningSecondRoundReusesAttemptAndEarlyTime(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(gate) })
	var requests atomic.Int32
	fullEnded := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			screeningJSONReply(w, 90, "基础信息通过")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		screeningSSEChunk(w, `{"analysis":{"score":0,"reason":"详情明确不符合，保留真实零分"},"resume":`)
		<-gate
		screeningSSEChunk(w, `null}`)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		close(fullEnded)
	}))
	defer server.Close()
	defer release.Do(func() { close(gate) })
	flow, prepared := screeningFlowFixture(t, server.URL)
	candidate := model.Candidate{Fingerprint: "card", Name: "测试人选"}
	results, done := flow.candidatePreviewsWithDone(context.Background(), prepared, []model.Candidate{candidate})
	preview := <-results
	<-done
	if preview.Err != nil || preview.AttemptID == "" {
		t.Fatal(preview.Err)
	}
	finished := make(chan error, 1)
	go func() {
		finished <- flow.processCandidateAttempt(context.Background(), prepared, &greetingSettingRuntime{}, candidate, &shared.Stats{}, preview.AttemptID)
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("第二轮等待了完整尾部响应")
	}
	record, err := flow.Store.ScreeningRecord(context.Background(), preview.AttemptID)
	if err != nil || record.Snapshot.First.Status != "passed" || record.Snapshot.Second.Status != "rejected" || record.Snapshot.Second.Score == nil || *record.Snapshot.Second.Score != 0 || record.Snapshot.Second.DurationMS == nil || !record.Snapshot.Completed {
		t.Fatalf("两轮记录未正确关联: %+v err=%v", record, err)
	}
	items, _ := flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	if len(items) != 1 {
		t.Fatal("第二轮创建了另一份尝试")
	}
	select {
	case <-fullEnded:
		t.Fatal("测试尾部未真正阻塞")
	default:
	}
	release.Do(func() { close(gate) })
	<-fullEnded
}

// TestScreeningKeywordAndUnexecutedDetail 验证关键词没有伪造AI分数，详情错误不能伪造已调用第二轮。
func TestScreeningKeywordAndUnexecutedDetail(t *testing.T) {
	flow, prepared := screeningFlowFixture(t, "")
	prepared.Position.RequiresAI = false
	prepared.Position.CommonConfig.ModeDefault = "keyword"
	prepared.Position.Keywords = []string{"不存在的关键词"}
	if err := flow.processCandidate(context.Background(), prepared, &greetingSettingRuntime{}, model.Candidate{Fingerprint: "keyword", Name: "关键词人选"}, &shared.Stats{}); err != nil {
		t.Fatal(err)
	}
	items, err := flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	first := items[0].Snapshot.First
	if first.Kind != "keyword" || first.Status != "rejected" || first.Score != nil || first.Threshold != nil || first.DurationMS == nil || items[0].Snapshot.Second.StartedAt != nil {
		t.Fatal("关键词结果混入AI评分或假时间")
	}
	prepared.Position.RequiresAI = true
	prepared.Position.CommonConfig.ModeDefault = "ai"
	prepared.Position.Keywords = nil
	if err = flow.processCandidate(context.Background(), prepared, &greetingSettingRuntime{readErr: errors.New("合成详情无法读取")}, model.Candidate{Fingerprint: "detail", Name: "详情人选"}, &shared.Stats{}); err == nil {
		t.Fatal("预期详情失败")
	}
	items, _ = flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	for _, item := range items {
		if item.Snapshot.CandidateKey == "detail" && (item.Snapshot.Second.Status != "not_executed" || item.Snapshot.Second.StartedAt != nil || !strings.Contains(item.Snapshot.Second.Reason, "详情")) {
			t.Fatal("详情读取失败被记作已执行AI二轮")
		}
	}
}

// screeningBatchRuntime 提供固定候选人批次，用于真正经过招呼上限分支而不操作浏览器。
type screeningBatchRuntime struct {
	greetingSettingRuntime
	candidates []model.Candidate
}

// FindCandidates 返回合成卡片，后续详情、关闭和招呼复用既有内存平台桩。
func (r *screeningBatchRuntime) FindCandidates(context.Context, model.Browser, model.Config, model.CandidateListOptions) ([]model.Candidate, error) {
	return r.candidates, nil
}

// TestScreeningGreetingLimitKeepsInFlightResults 验证真正达到招呼上限时，预先并发调用的五份第一轮仍全部保存。
func TestScreeningGreetingLimitKeepsInFlightResults(t *testing.T) {
	ready := make(chan struct{})
	var readyOnce sync.Once
	defer readyOnce.Do(func() { close(ready) })
	var aiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			_, _ = w.Write([]byte(`{"user":{"id":"owner"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "chat/completions") {
			count := aiCalls.Add(1)
			if count == 5 {
				readyOnce.Do(func() { close(ready) })
			}
			if count <= 5 {
				<-ready
			}
			screeningJSONReply(w, 99, "本轮条件通过")
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	defer readyOnce.Do(func() { close(ready) })
	flow, prepared := screeningFlowFixture(t, server.URL)
	flow.Cloud = cloud.New(server.URL)
	prepared.Request.EnableGreet = nil
	runtime := &screeningBatchRuntime{}
	for i := 0; i < 5; i++ {
		runtime.candidates = append(runtime.candidates, model.Candidate{Index: i, Fingerprint: fmt.Sprint("batch-", i), Name: fmt.Sprint("人选", i)})
	}
	stats := shared.Stats{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := flow.processBatches(ctx, prepared, runtime, nil, &stats)
	flow.finalizeScreeningTask(ctx, prepared, stats, err)
	if err != nil || stats.Succeeded != 1 || aiCalls.Load() != 6 {
		t.Fatalf("招呼上限路径错误: err=%v stats=%+v calls=%d", err, stats, aiCalls.Load())
	}
	items, err := flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	if err != nil || len(items) != 5 {
		t.Fatal("上限返回丢失已调用预筛")
	}
	secondCount := 0
	for _, item := range items {
		if !item.Snapshot.Completed || item.Snapshot.First.Status != "passed" {
			t.Fatal("第一轮结果没有持久保存")
		}
		if item.Snapshot.Second.Status == "passed" {
			secondCount++
		} else if item.Snapshot.Second.Status != "not_executed" || !strings.Contains(item.Snapshot.Second.Reason, "上限") {
			t.Fatal("未进入第二轮的原因错误")
		}
	}
	if secondCount != 1 {
		t.Fatal("二轮记录没有复用对应的首轮尝试")
	}
}

// TestScreeningCancelledPreviewKeepsActualStart 验证已发出请求后取消，仍持久保存本轮取消和真实起止，不伪造分数。
func TestScreeningCancelledPreviewKeepsActualStart(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release }))
	defer server.Close()
	defer once.Do(func() { close(release) })
	flow, prepared := screeningFlowFixture(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	_, done := flow.candidatePreviewsWithDone(ctx, prepared, []model.Candidate{{Fingerprint: "cancelled", Name: "取消人选"}})
	<-entered
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("取消后没有保存第一轮状态")
	}
	items, err := flow.Store.PendingScreeningRecords(context.Background(), "owner", 30)
	if err != nil || len(items) != 1 {
		t.Fatal("取消记录没有落盘")
	}
	round := items[0].Snapshot.First
	if round.Status != "cancelled" || round.StartedAt == nil || round.FinishedAt == nil || round.DurationMS == nil || round.Score != nil || round.Accepted != nil || !items[0].Snapshot.Completed {
		t.Fatalf("取消状态错误: %+v", round)
	}
	once.Do(func() { close(release) })
}
