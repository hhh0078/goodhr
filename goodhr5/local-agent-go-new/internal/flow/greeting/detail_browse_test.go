// Package greeting 文件作用：验证 DOM 详情先滚动再读取、故障时停止读取并清理详情，截图模式不额外调用 DOM 浏览。
package greeting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

type detailBrowseRuntime struct {
	greetingSettingRuntime
	browseErr error
	order     []string
}

// BrowseCandidateDetail 记录浏览发生在正文读取前，按场景模拟滚轮失败。
func (r *detailBrowseRuntime) BrowseCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.order = append(r.order, "browse")
	return r.browseErr
}

// ExtractCandidateDetail 记录正文读取顺序并复用已有合成简历。
func (r *detailBrowseRuntime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) (model.CandidateDetail, error) {
	r.order = append(r.order, "read")
	return r.greetingSettingRuntime.ExtractCandidateDetail(ctx, browser, cfg, candidate)
}

type detailBrowseLog struct {
	status string
	err    error
}

type detailBrowseLogger struct{ events []detailBrowseLog }

// Step 只收集详情浏览步骤的开始和最终结果。
func (l *detailBrowseLogger) Step(_ string, _ string, step string, status string, _ time.Time, err error) {
	if step == "browse_candidate_detail" {
		l.events = append(l.events, detailBrowseLog{status: status, err: err})
	}
}

// TestDOMDetailBrowsePrecedesRead 验证滚动完成才读取正文，滚动失败不读取、不发送且清理已打开的详情。
func TestDOMDetailBrowsePrecedesRead(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			disabled := false
			prepared := shared.PreparedTask{
				Request:  shared.StartRequest{TaskID: "detail-browse", EnableGreet: &disabled},
				Position: cloud.PositionSnapshot{Keywords: []string{"符合"}, CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "dom"}},
			}
			runtime := &detailBrowseRuntime{}
			if failure {
				runtime.browseErr = errors.New("合成滚轮中断")
			}
			logger := &detailBrowseLogger{}
			flow := Flow{Store: store, Logger: logger}
			stats := shared.Stats{}
			err = flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{Name: "合成候选人"}, &stats)
			wantOrder := []string{"browse", "read"}
			wantStatus := "success"
			if failure {
				wantOrder, wantStatus = []string{"browse"}, "failed"
				if !errors.Is(err, runtime.browseErr) || stats.Failed != 1 {
					t.Fatalf("滚轮失败没有原样传播和计数：err=%v stats=%+v", err, stats)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtime.order, wantOrder) || runtime.closes != 1 || runtime.greetings != 0 {
				t.Fatalf("详情顺序或清理错误：order=%v closes=%d greetings=%d", runtime.order, runtime.closes, runtime.greetings)
			}
			if len(logger.events) != 2 || logger.events[0].status != "start" || logger.events[1].status != wantStatus || !errors.Is(logger.events[1].err, runtime.browseErr) {
				t.Fatalf("详情滚动日志没有完整首尾：%+v", logger.events)
			}
		})
	}
}

// TestScreenshotModesSkipDOMBrowse 验证 OCR 和 AI 看图仍由长截图负责滚动，避免额外 DOM 浏览改变截图起点。
func TestScreenshotModesSkipDOMBrowse(t *testing.T) {
	for _, mode := range []string{"ocr", "ai"} {
		t.Run(mode, func(t *testing.T) {
			captures := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/api/v1/element/screenshot-long" {
					t.Errorf("意外浏览器请求：%s", request.URL.Path)
				}
				captures++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"SCREENSHOT_FAILED","message":"合成截图结束"}}`))
			}))
			defer server.Close()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			prepared := shared.PreparedTask{
				Position: cloud.PositionSnapshot{RequiresOCR: mode == "ocr", RequiresAI: mode == "ai", CommonConfig: cloud.PositionCommonConfig{ModeDefault: "ai", DetailMode: mode}},
				Platform: model.Config{Selectors: map[string]contract.SelectorSpec{"candidate.detail": {}}},
			}
			logger := &detailBrowseLogger{}
			flow := Flow{Browser: client.New(server.URL), Store: store, Logger: logger, ScreenshotsDir: t.TempDir()}
			runtime := &detailBrowseRuntime{}
			stats := shared.Stats{}
			err = flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{}, &stats)
			if err == nil || !strings.Contains(err.Error(), "合成截图结束") {
				t.Fatalf("没有进入原长截图路径：%v", err)
			}
			if captures != 1 || len(runtime.order) != 0 || len(logger.events) != 0 || runtime.closes != 1 || runtime.greetings != 0 {
				t.Fatalf("截图模式误执行 DOM 滚动或读取：captures=%d order=%v logs=%v closes=%d greetings=%d", captures, runtime.order, logger.events, runtime.closes, runtime.greetings)
			}
		})
	}
}
