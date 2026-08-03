// Package liepin 文件作用：承载 detail.go 对应的平台职责实现。
package liepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// FetchCandidateDetail 读取猎聘企业端新开详情页中的 DOM 文本。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	if strings.ToLower(strings.TrimSpace(request.Mode)) != "dom" {
		return platformcore.DetailResult{}, fmt.Errorf("%s只支持 DOM 详情识别", r.platformName)
	}
	item := platformElement(cfg, "card", "item")
	if item == nil {
		return platformcore.DetailResult{}, fmt.Errorf("平台配置中无候选人卡片选择器")
	}
	clickTarget := platformElement(cfg, "detail", "openTarget")
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": clickTarget, "timeout": 10000}); err != nil {
		return platformcore.DetailResult{}, err
	}
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

// CloseCandidateDetail 关闭猎聘企业端候选人详情页。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	closeBtn := platformElement(cfg, "detail", "closeBtn")
	if closeBtn != nil {
		_, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{"element": closeBtn, "timeout": 1500})
		if err == nil {
			return nil
		}
	}
	_, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape", "wait_ms": 200})
	return err
}

// CleanCandidateDetailText 清理猎聘企业端详情文本中的平台附加内容。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}
