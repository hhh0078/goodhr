// Package zhaopin 文件作用：承载 detail.go 对应的平台职责实现。
package zhaopin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

// FetchCandidateDetail 读取智联招聘新开详情页中的 DOM 文本。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人，request 为详情请求。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	if strings.ToLower(strings.TrimSpace(request.Mode)) != "dom" {
		return platformcore.DetailResult{}, fmt.Errorf("智联招聘只支持 DOM 详情识别")
	}
	name := candidateName(candidate)
	exec.Log("info", fmt.Sprintf("调用智联详情 DOM 提取接口：name=%s card_index=%d", name, intFromMap(candidate, "card_index")))
	payload := zhaopinCandidateVisiblePayload(cfg, candidate)
	payload["screenshot"] = false
	payload["force_scroll"] = false
	payload["card_scroll_attempts"] = 3
	payload["require_full"] = false
	payload["wait_ms"] = 300
	payload["detail_ready_timeout"] = 8000
	payload["viewport_margin"] = 24
	result, err := exec.Post(ctx, "/api/v1/boss/candidates/detail", payload)
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	data := workerDataMap(result)
	text := strings.TrimSpace(firstNonEmpty(stringFromMap(data, "detail_text"), stringFromMap(data, "text")))
	if text == "" {
		return platformcore.DetailResult{}, fmt.Errorf("智联招聘详情弹框未读取到 DOM 文本")
	}
	return platformcore.DetailResult{Text: text, Source: "dom"}, nil
}

// ScrollCandidateDetail 在最终 AI 分析期间滚动一次智联详情弹框。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人，distance 为滚动距离。
func (r *Runtime) ScrollCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, distance int) error {
	_, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{
		"element":  map[string]any{"selector": ".new-resume-detail--inner, .resume-detail, .resume-item__content"},
		"distance": distance,
	})
	return err
}

// CloseCandidateDetail 关闭智联招聘候选人详情页。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	name := candidateName(candidate)
	exec.Log("info", "正在关闭"+name+"详情")
	if _, err := exec.Post(ctx, "/api/v1/boss/candidates/detail/close", map[string]any{
		"platform_id":     "zhaopin",
		"platform_config": cfg,
		"key":             "Escape",
		"candidate_name":  name,
	}); err != nil {
		return err
	}
	if err := exec.Delay(ctx, "等待智联详情弹框关闭", 0.15); err != nil {
		return err
	}
	for attempt := 1; attempt <= 2; attempt++ {
		result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
			"element":      map[string]any{"selector": ".new-resume-detail--inner, .resume-detail"},
			"visible_only": true,
			"max_items":    1,
		})
		if err != nil {
			return fmt.Errorf("确认智联详情弹框关闭失败：%w", err)
		}
		if len(mapList(workerData(result, "items"))) == 0 {
			exec.Log("info", name+"详情已关闭")
			return nil
		}
		if attempt == 1 {
			if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape"}); err != nil {
				return fmt.Errorf("再次关闭智联详情弹框失败：%w", err)
			}
			if err := exec.Delay(ctx, "再次等待智联详情弹框关闭", 0.2); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("智联详情弹框仍未关闭")
}

// CleanCandidateDetailText 清理智联招聘详情文本中的平台附加内容。
// text 为 DOM 提取出的详情文本。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}
