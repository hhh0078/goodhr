// Package greeting 文件作用：回归验证图片简历不会被空 DOM 拦截，关键词必读详情且截图失败不会误发招呼。
package greeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// imageDetailRuntime 提供没有网页文字的图片简历，记录详情读取、关闭和打招呼调用。
type imageDetailRuntime struct {
	model.Runtime
	domReads, closes, greetings, conversationCloses int
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

// CloseCandidateConversation 记录成功发送后必须执行的弹层清理。
func (r *imageDetailRuntime) CloseCandidateConversation(context.Context, model.Browser, model.Config, model.Candidate) error {
	r.conversationCloses++
	return nil
}

// TestSuccessfulGreetingAlwaysClosesPanels 验证没有索要资料或追加消息时也关闭发送后的推广弹层。
func TestSuccessfulGreetingAlwaysClosesPanels(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := Flow{Store: store}
	runtime := &imageDetailRuntime{}
	stats := shared.Stats{}
	prepared := shared.PreparedTask{Position: cloud.PositionSnapshot{CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword"}}}
	if err := flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{}, &stats); err != nil {
		t.Fatal(err)
	}
	if runtime.greetings != 1 || runtime.conversationCloses != 1 || stats.Succeeded != 1 {
		t.Fatalf("成功发送必须清理一次且保留成功计数：发送=%d 清理=%d 统计=%+v", runtime.greetings, runtime.conversationCloses, stats)
	}
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

// TestOCRKeepsOriginalScreenshotSegments 验证平台要求拼接时，OCR 仍拿到每段原图，避免长图缩放漏掉关键词。
func TestOCRKeepsOriginalScreenshotSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"parts":[{"path":"first.png","filename":"first.png"},{"path":"second.png","filename":"second.png"}],"complete":true}}`))
	}))
	defer server.Close()
	prepared := shared.PreparedTask{
		Position: cloud.PositionSnapshot{RequiresOCR: true},
		Platform: model.Config{Selectors: map[string]contract.SelectorSpec{"candidate.detail": {}}},
	}
	prepared.Platform.Behavior.StitchDetailScreenshots = true
	flow := Flow{Browser: client.New(server.URL), ScreenshotsDir: t.TempDir()}
	parts, err := flow.captureDetailScreenshots(context.Background(), prepared, model.Candidate{})
	if err != nil || len(parts) != 2 || parts[0].Path != "first.png" || parts[1].Path != "second.png" {
		t.Fatalf("OCR 原图分段没有保留：%+v %v", parts, err)
	}
}

// TestVisionKeywordReadsDetailBeforeMatching 验证 AI 图片中的关键词先转录再做 AND/排除词判断，后续评分复用原截图。
func TestVisionKeywordReadsDetailBeforeMatching(t *testing.T) {
	for _, excluded := range []bool{false, true} {
		t.Run(fmt.Sprintf("excluded_%t", excluded), func(t *testing.T) {
			dir := t.TempDir()
			imagePath := filepath.Join(dir, "detail.png")
			if err := os.WriteFile(imagePath, []byte("fixture-image"), 0o600); err != nil {
				t.Fatal(err)
			}
			var captures, aiCalls atomic.Int32
			reportedKinds := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/api/user-flow" {
					var body struct {
						Metadata cloud.ScreeningResultEvidence `json:"metadata"`
					}
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					reportedKinds <- body.Metadata.Kind
					_, _ = w.Write([]byte(`{"ok":true}`))
					return
				}
				if request.URL.Path == "/api/v1/element/screenshot-long" {
					captures.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"parts": []contract.ScreenshotPart{{Path: imagePath, Filename: "detail.png"}}}})
					return
				}
				call := aiCalls.Add(1)
				content := `{"text":"本科毕业，具有销售运营经验"}`
				if call > 1 {
					content = `{"score":0,"reason":"回归测试不发送"}`
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
			}))
			defer server.Close()
			store, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			position := cloud.PositionSnapshot{ID: "position-mixed", RequiresAI: true, Keywords: []string{"本科", "销售"}, IsAndMode: true,
				CommonConfig: cloud.PositionCommonConfig{ModeDefault: "keyword", DetailMode: "ai"},
				AI:           cloud.AIConfig{BaseURL: server.URL, APIKey: "fixture", Model: "fixture"}}
			if excluded {
				position.ExcludeKeywords = []string{"运营"}
			}
			prepared := shared.PreparedTask{Position: position, Platform: model.Config{Selectors: map[string]contract.SelectorSpec{"candidate.detail": {}}}}
			logger := &screeningTestLogger{}
			flow := Flow{Browser: client.New(server.URL), AI: ai.New(), Cloud: cloud.New(server.URL), Logger: logger, Store: store, ScreenshotsDir: dir}
			runtime := &imageDetailRuntime{}
			stats := shared.Stats{}
			if err := flow.processCandidate(context.Background(), prepared, runtime, model.Candidate{}, &stats); err != nil {
				t.Fatal(err)
			}
			expectedCalls := 2
			if excluded {
				expectedCalls = 1
			}
			if captures.Load() != 1 || aiCalls.Load() != int32(expectedCalls) || runtime.domReads != 0 || runtime.greetings != 0 {
				t.Fatalf("截图=%d AI=%d DOM=%d 发送=%d", captures.Load(), aiCalls.Load(), runtime.domReads, runtime.greetings)
			}
			keywordSeen := false
			for _, result := range logger.snapshots() {
				if result.Kind == "keyword" {
					keywordSeen = true
					if result.Terminal != excluded || (result.Terminal && result.Stage != "final") || (!result.Terminal && result.Stage != "preview") {
						t.Fatalf("关键词通过后不应在 AI 最终评分前标记完成：%+v", result)
					}
				}
			}
			if !keywordSeen {
				t.Fatal("实际混合流程未发布关键词结果")
			}
			waitScreeningSync(t, stats.ScreeningResultSync.Reported)
			wantKind := "ai"
			if excluded {
				wantKind = "keyword"
			}
			select {
			case kind := <-reportedKinds:
				if kind != wantKind {
					t.Fatalf("教程完成证据用了尚未结束的关键词通过结果：%s，应为 %s", kind, wantKind)
				}
			default:
				t.Fatal("真实终态完成后没有保存教程事实")
			}
		})
	}
}
