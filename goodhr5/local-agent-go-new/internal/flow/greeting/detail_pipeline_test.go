// Package greeting 文件作用：回归验证图片简历不会被空 DOM 拦截，关键词必读详情且截图失败不会误发招呼。
package greeting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// imageDetailRuntime 提供没有网页文字的图片简历，记录详情读取、关闭和打招呼调用。
type imageDetailRuntime struct {
	model.Runtime
	domReads, closes, greetings int
}

// ScrollToCandidate 模拟卡片已经完整进入可见区域。
func (r *imageDetailRuntime) ScrollToCandidate(context.Context, model.Browser, model.Config, model.Candidate) error {
	return nil
}

// OpenCandidateDetail 模拟成功打开只包含图片的简历详情。
func (r *imageDetailRuntime) OpenCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) error {
	return nil
}

// ExtractCandidateDetail 记录不应发生的 DOM 读取并返回空正文错误。
func (r *imageDetailRuntime) ExtractCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) (model.CandidateDetail, error) {
	r.domReads++
	return model.CandidateDetail{}, errors.New("图片简历没有 DOM 正文")
}

// CleanCandidateDetailText 模拟平台正文清理。
func (r *imageDetailRuntime) CleanCandidateDetailText(text string) string { return text }

// CloseCandidateDetail 记录失败后的详情清理。
func (r *imageDetailRuntime) CloseCandidateDetail(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.closes++
	return nil
}

// GreetCandidate 记录不应在读取失败后发生的发送。
func (r *imageDetailRuntime) GreetCandidate(context.Context, model.Browser, model.Config, model.Candidate, model.GreetRequest) error {
	r.greetings++
	return nil
}

// TestImageDetailReachesScreenshotWithoutDOM 验证 OCR 和 AI 看图直接进入截图，截图失败必须关闭详情且不能计为招呼成功。
func TestImageDetailReachesScreenshotWithoutDOM(t *testing.T) {
	for _, mode := range []string{"ocr", "ai"} {
		t.Run(mode, func(t *testing.T) {
			captures := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/api/v1/element/screenshot-long" {
					t.Errorf("意外请求：%s", request.URL.Path)
				}
				captures++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"SCREENSHOT_FAILED","message":"测试截图失败"}}`))
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
			flow := Flow{Browser: client.New(server.URL), Store: store, ScreenshotsDir: t.TempDir()}
			runtime := &imageDetailRuntime{}
			stats := shared.Stats{}
			err = flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{}, &stats)
			if err == nil || !strings.Contains(err.Error(), "测试截图失败") {
				t.Fatalf("应报告截图阶段错误，实际：%v", err)
			}
			if captures != 1 || runtime.domReads != 0 || runtime.closes != 1 || runtime.greetings != 0 || stats.Succeeded != 0 {
				t.Fatalf("流程不符：截图=%d DOM=%d 关闭=%d 发送=%d 统计=%+v", captures, runtime.domReads, runtime.closes, runtime.greetings, stats)
			}
		})
	}
}

// TestKeywordRequiresDetailWithoutLegacyMode 验证旧岗位未配置详情模式时，详情关键词仍能触发完整读取。
func TestKeywordRequiresDetailWithoutLegacyMode(t *testing.T) {
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{Keywords: []string{"销售"}, CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword"}}}
	if !needsCandidateDetail(prepared) {
		t.Fatal("关键词任务必须读取详情，不能只看列表卡片")
	}
}
