// Package hliepin 文件作用：承载 detail.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
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
