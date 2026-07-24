// Package hliepin 文件作用：承载 detail.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"math/rand/v2"
	"strings"
	"time"
)

// FetchCandidateDetail 读取猎聘猎头端新开详情页中的 DOM 文本。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	if strings.ToLower(strings.TrimSpace(request.Mode)) != "dom" {
		return platformcore.DetailResult{}, fmt.Errorf("%s只支持 DOM 详情识别", r.platformName)
	}
	item := candidateItemElement(candidate, cfg)
	clickTarget := platformElement(cfg, "detail", "openTarget")
	if clickTarget == nil {
		clickTarget = map[string]any{"selector": "a"}
	} else {
		clickTarget["selectors"] = []any{"a"}
	}
	openResult, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": clickTarget,
		"timeout": 10000, "wait_for_new_page": true, "require_new_page": true,
		"diagnostic_platform": "hliepin", "diagnostic_platform_name": "猎聘",
		"diagnostic_action": "读取候选人详情", "diagnostic_candidate_name": candidateName(candidate),
	})
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	openData := workerDataMap(openResult)
	candidate["detail_page_token"] = stringFromMap(openData, "page_token")
	candidate["detail_return_page_token"] = stringFromMap(openData, "previous_page_token")
	if err := exec.Delay(ctx, "等待猎聘详情页打开", 1.2); err != nil {
		return platformcore.DetailResult{}, err
	}
	content := platformElement(cfg, "detail", "content")
	payload := map[string]any{"timeout": 5000}
	if content != nil {
		payload["element"] = content
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", payload)
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	data := workerDataMap(result)
	text := strings.TrimSpace(firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"])))
	if text == "" {
		return platformcore.DetailResult{}, fmt.Errorf("猎聘详情页未读取到 DOM 文本")
	}
	return platformcore.DetailResult{Text: text, Source: "dom"}, nil
}

// ScrollCandidateDetail 不移动鼠标，使用连续滚轮在随机二到五秒内把猎聘新详情页滚动到底。
func (r *Runtime) ScrollCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, distance int) error {
	startedAt := time.Now()
	duration := 2000 + rand.IntN(3001)
	result, err := exec.Post(ctx, "/api/v1/page/scroll-or-click-next", map[string]any{
		"distance": 360, "bottom_threshold": 80, "scroll_to_bottom_duration_ms": duration,
		"next_element": map[string]any{"selector": "[data-goodhr-detail-scroll-next='never']"},
	})
	if err != nil {
		return err
	}
	data := workerDataMap(result)
	if stringFromMap(data, "action") != "end" {
		return fmt.Errorf("猎聘详情页滚动完成后仍未到达底部，候选人=%s", candidateName(candidate))
	}
	exec.Log("info", fmt.Sprintf("详情浏览：猎聘详情页已拟人滚动到底，候选人=%s，目标时长=%dms，耗时=%s", candidateName(candidate), duration, time.Since(startedAt).Round(time.Millisecond)))
	return nil
}

// CloseCandidateDetail 关闭猎聘猎头端候选人详情页。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	payload := map[string]any{
		"page_token":          stringFromMap(candidate, "detail_page_token"),
		"return_page_token":   stringFromMap(candidate, "detail_return_page_token"),
		"target_url_contains": "/resume/showresumedetail/",
		"only_url_contains":   "/resume/showresumedetail/",
		"return_url_contains": "h.liepin.com/search/",
		"go_back_if_same":     true,
		"timeout":             10000,
	}
	_, err := exec.Post(ctx, "/api/v1/page/close", payload)
	return err
}

// CleanCandidateDetailText 清理猎聘猎头端详情文本中的平台附加内容。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}
