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
	payload["force_scroll"] = true
	payload["wait_ms"] = 600
	payload["detail_ready_timeout"] = 15000
	payload["viewport_margin"] = 80
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

// CloseCandidateDetail 关闭智联招聘候选人详情页。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	name := candidateName(candidate)
	exec.Log("info", "正在关闭"+name+"详情")
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/detail/close", map[string]any{
		"platform_id":     "zhaopin",
		"platform_config": cfg,
		"key":             "Escape",
		"candidate_name":  name,
	})
	if err == nil {
		exec.Log("info", name+"详情已关闭")
	}
	return err
}

// CleanCandidateDetailText 清理智联招聘详情文本中的平台附加内容。
// text 为 DOM 提取出的详情文本。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}
