// Package lifecycle 文件作用：验证各平台正式任务包装器的批次隔离、父级预算、真实请求收集及异常传播。
package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// candidateBatchRuntimeFixture 只替代平台读取入口，正式日志包装和浏览器客户端仍使用产品实现。
type candidateBatchRuntimeFixture struct {
	model.Runtime
	read func(context.Context, model.Browser, model.Config, model.CandidateListOptions) ([]model.Candidate, error)
}

// FindCandidates 把实际包装后的参数交给独立测试场景。
func (f candidateBatchRuntimeFixture) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	return f.read(ctx, browser, cfg, options)
}

// TestCandidateBatchWrapperForAllPlatformConfigs 验证所有平台配置经正式包装后只给本批开启统计，且旧组件无统计也能落库。
func TestCandidateBatchWrapperForAllPlatformConfigs(t *testing.T) {
	for _, platformID := range []string{"boss", "hliepin", "liepin", "zhaopin", "51job"} {
		t.Run(platformID, func(t *testing.T) {
			cfg, err := platform.LoadConfig(platformID)
			if err != nil {
				t.Fatal(err)
			}
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			logger := NewTaskLogger(store, nil)
			logger.SetTaskPosition("batch-task", "batch-position")
			options := model.CandidateListOptions{ReadConcurrency: 3}
			fixture := candidateBatchRuntimeFixture{read: func(ctx context.Context, _ model.Browser, actual model.Config, received model.CandidateListOptions) ([]model.Candidate, error) {
				if !received.CollectReadStats || received.ReadConcurrency != options.ReadConcurrency || actual.ID != cfg.ID {
					t.Fatal("正式读取标记或原调用参数丢失")
				}
				deadline, hasDeadline := ctx.Deadline()
				if platformID == "boss" {
					if !hasDeadline || time.Until(deadline) > 120*time.Second || time.Until(deadline) < 119*time.Second {
						t.Fatal("BOSS 整批读取未应用两分钟配置预算")
					}
				} else if hasDeadline {
					t.Fatal("其他平台被增加了读取预算")
				}
				return []model.Candidate{{Index: 0}}, nil
			}}
			items, err := newLoggedRuntime(fixture, "batch-task", logger).FindCandidates(context.Background(), nil, cfg, options)
			if err != nil || len(items) != 1 || options.CollectReadStats {
				t.Fatalf("返回结果或外层参数被改变：%v %+v", err, options)
			}
			rows, err := store.ListPositionLogs(context.Background(), "batch-position", 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("本批应只保存一条汇总：%d %v", len(rows), err)
			}
			var output candidateListReadLogOutput
			if err := json.Unmarshal(rows[0].Output, &output); err != nil || output.Batch.PlatformID != cfg.ID || output.Batch.UsableCount != 1 || output.Batch.PlannedCount != nil || output.Batch.LastRawCompletedCount != nil || output.Batch.AvgMSPerRawCompleted != nil {
				t.Fatalf("旧组件未提供统计时仍应成功且计数未知：%+v %v", output, err)
			}
		})
	}
}

// TestCandidateBatchWrapperPreservesParentDeadlineAndCancellation 验证更短父级时限、取消信号以及三分钟硬上限都被保留。
func TestCandidateBatchWrapperPreservesParentDeadlineAndCancellation(t *testing.T) {
	for _, scenario := range []string{"parent_deadline", "cancelled", "maximum"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			cfg := model.Config{ID: "fixture", CandidateReadTimeoutMS: 240_000}
			var expected time.Time
			if scenario == "parent_deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
				expected, _ = ctx.Deadline()
			}
			if scenario == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			fixture := candidateBatchRuntimeFixture{read: func(actual context.Context, _ model.Browser, _ model.Config, _ model.CandidateListOptions) ([]model.Candidate, error) {
				deadline, ok := actual.Deadline()
				if !ok {
					t.Fatal("批次预算没有生效")
				}
				if scenario == "parent_deadline" && !deadline.Equal(expected) {
					t.Fatal("更短的父级时限被放宽")
				}
				if scenario == "maximum" && (time.Until(deadline) > 180*time.Second || time.Until(deadline) < 179*time.Second) {
					t.Fatal("批次配置没有限制在三分钟内")
				}
				return nil, actual.Err()
			}}
			_, err := newLoggedRuntime(fixture, "task", nil).FindCandidates(ctx, nil, cfg, model.CandidateListOptions{})
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("父级取消被改变：%v", err)
			}
		})
	}
}

// TestCandidateBatchWrapperCollectsActualRequests 验证每批独立收集真实请求，后续批次不能累计前一批记录。
func TestCandidateBatchWrapperCollectsActualRequests(t *testing.T) {
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request contract.ElementFindAllRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if !request.ReadStats {
			t.Error("正式请求没有开启计数")
		}
		fmt.Fprintf(w, `{"ok":true,"data":[{"index":0,"fields":{}}],"read_stats":{"request_id":%q,"planned_count":1,"raw_completed_count":1,"returned_count":1,"elapsed_ms":4}}`, r.Header.Get("X-Request-ID"))
	}))
	defer worker.Close()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := NewTaskLogger(store, nil)
	logger.SetTaskPosition("request-batch-task", "request-batch-position")
	fixture := candidateBatchRuntimeFixture{read: func(ctx context.Context, browser model.Browser, _ model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
		_, err := browser.FindAll(ctx, contract.ElementFindAllRequest{ReadStats: options.CollectReadStats, DiagnosticAttempt: 1, Fields: map[string]contract.SelectorSpec{"name": {}}})
		return []model.Candidate{{Index: 0}}, err
	}}
	wrapped := newLoggedRuntime(fixture, "request-batch-task", logger)
	for range 2 {
		if _, err := wrapped.FindCandidates(context.Background(), client.New(worker.URL), model.Config{ID: "fixture"}, model.CandidateListOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.ListPositionLogs(context.Background(), "request-batch-position", 10)
	if err != nil || len(rows) != 2 {
		t.Fatalf("每批应独立落库：%d %v", len(rows), err)
	}
	var requestIDs []string
	for _, row := range rows {
		var output candidateListReadLogOutput
		if err := json.Unmarshal(row.Output, &output); err != nil || output.Batch.AttemptCount != 1 || len(output.Batch.Requests) != 1 || output.Batch.LastRawCompletedCount == nil || *output.Batch.LastRawCompletedCount != 1 {
			t.Fatalf("真实请求未独立进入本批：%s %v", row.Output, err)
		}
		requestIDs = append(requestIDs, output.Batch.Requests[0].RequestID)
	}
	if requestIDs[0] == "" || requestIDs[0] == requestIDs[1] {
		t.Fatal("不同批次串用了同一请求")
	}
}

// TestCandidateBatchFormalPlatformFailureSummaries 验证五个真实平台入口经正式包装后，都把同请求的部分完成数写入批次日志，探测不计次数。
func TestCandidateBatchFormalPlatformFailureSummaries(t *testing.T) {
	for _, platformID := range []string{"boss", "hliepin", "liepin", "zhaopin", "51job"} {
		t.Run(platformID, func(t *testing.T) {
			cfg, err := platform.LoadConfig(platformID)
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := platform.RuntimeFor(platformID)
			if err != nil {
				t.Fatal(err)
			}
			worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/element/find-all" {
					t.Errorf("读取批次不应执行其他页面动作：%s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				var request contract.ElementFindAllRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.ProbeOnly || request.ViewportStart != nil || len(request.Fields) == 0 {
					if request.ReadStats {
						t.Error("辅助探测不应开启正式读取计数")
					}
					fmt.Fprint(w, `{"ok":true,"data":[]}`)
					return
				}
				if !request.ReadStats {
					t.Error("正式平台入口没有传递本批统计标记")
				}
				id := r.Header.Get("X-Request-ID")
				fmt.Fprintf(w, `{"ok":false,"error":{"code":"WORKER_UNRESPONSIVE","message":"合成读取超时","step":"queue","details":{"request_id":%q,"reason":"deadline_exceeded","request_progress":{"request_id":%q,"worker_phase":"read_field","item_index":29,"read_stats":{"request_id":%q,"planned_count":5,"raw_completed_count":2,"elapsed_ms":43000}}}}}`, id, id, id)
			}))
			defer worker.Close()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			logger := NewTaskLogger(store, nil)
			logger.SetTaskPosition("formal-platform-task", "formal-platform-position")
			if _, err := newLoggedRuntime(runtime, "formal-platform-task", logger).FindCandidates(context.Background(), client.New(worker.URL), cfg, model.CandidateListOptions{}); err == nil {
				t.Fatal("真实平台入口的读取失败被吞掉")
			}
			rows, err := store.ListPositionLogs(context.Background(), "formal-platform-position", 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("正式平台应保存单条批次汇总：%d %v", len(rows), err)
			}
			var output candidateListReadLogOutput
			if err := json.Unmarshal(rows[0].Output, &output); err != nil || output.Batch.PlatformID != cfg.ID || output.Batch.Status != "timeout" || output.Batch.AttemptCount != 1 || output.Batch.UsableCount != 0 || output.Batch.LastRawCompletedCount == nil || *output.Batch.LastRawCompletedCount != 2 || output.Batch.AvgMSPerRawCompleted != nil {
				t.Fatalf("正式平台统计丢失、混入探测或从行号推算人数：%s %v", rows[0].Output, err)
			}
		})
	}
}

// TestCandidateBatchWrapperKeepsLegacyLoggerAndPanic 验证旧日志器仍接收步骤，批次日志器不会吞掉平台异常。
func TestCandidateBatchWrapperKeepsLegacyLoggerAndPanic(t *testing.T) {
	failure := errors.New("合成读取失败")
	fixture := candidateBatchRuntimeFixture{read: func(context.Context, model.Browser, model.Config, model.CandidateListOptions) ([]model.Candidate, error) {
		return nil, failure
	}}
	legacy := &runtimeLogCollector{}
	if _, err := newLoggedRuntime(fixture, "task", legacy).FindCandidates(context.Background(), nil, model.Config{}, model.CandidateListOptions{}); err != failure || len(legacy.events) != 1 || legacy.events[0] != (runtimeLogEvent{"find_candidates", "failed"}) {
		t.Fatalf("旧日志器或原错误不兼容：%+v %v", legacy.events, err)
	}
	fixture.read = func(context.Context, model.Browser, model.Config, model.CandidateListOptions) ([]model.Candidate, error) {
		panic(failure)
	}
	defer func() {
		if recovered := recover(); recovered != failure {
			t.Fatalf("平台异常被吞掉或改变：%v", recovered)
		}
	}()
	_, _ = newLoggedRuntime(fixture, "task", NewTaskLogger(nil, nil)).FindCandidates(context.Background(), nil, model.Config{}, model.CandidateListOptions{})
}
