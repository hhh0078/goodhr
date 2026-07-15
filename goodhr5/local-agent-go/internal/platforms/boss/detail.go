// Package boss 文件作用：承载 detail.go 对应的平台职责实现。
package boss

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"path/filepath"
	"strings"
)

// FetchCandidateDetail 读取 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人，request 为详情请求。
func (r *Runtime) FetchCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate, request platformcore.DetailRequest) (platformcore.DetailResult, error) {
	name := candidateName(candidate)
	exec.Log("info", fmt.Sprintf("调用详情提取接口：name=%s mode=%s card_index=%d", name, detailModeLabel(request.Mode), intFromMap(candidate, "card_index")))
	result, err := exec.Post(ctx, "/api/v1/boss/candidates/detail", map[string]any{
		"platform_config":      cfg,
		"card_index":           intFromMap(candidate, "card_index"),
		"element_ref":          stringFromMap(candidate, "element_ref"),
		"screenshot":           request.Mode == "ocr" || request.Mode == "ai",
		"force_scroll":         true,
		"distance":             120,
		"wait_ms":              600,
		"detail_ready_timeout": 15000,
		"card_scroll_attempts": 18,
		"require_full":         true,
		"viewport_margin":      80,
		"dir":                  filepath.Join(request.ScreenshotsDir, request.TaskID),
		"filename":             request.Filename,
	})
	if err != nil {
		return platformcore.DetailResult{}, err
	}
	data := workerDataMap(result)
	detailText := strings.TrimSpace(firstNonEmpty(stringFromMap(data, "detail_text"), stringFromMap(data, "text")))
	// 调试截图信息
	if dbg := stringFromMap(data, "_screenshot_debug"); dbg != "" {
		exec.Log("info", "详情截图调试: "+dbg)
	}
	screenshot := mapFromAny(data["screenshot"])
	if len(screenshot) > 0 {
		if scrollDebug := stringFromMap(screenshot, "_scroll_debug"); scrollDebug != "" {
			// exec.Log("info", "详情截图滚动调试: "+scrollDebug)
		}
		if partsCount := intFromMap(screenshot, "parts_count"); partsCount > 0 {
			exec.Log("info", fmt.Sprintf("详情截图分段完成：name=%s parts=%d scrollable=%v", name, partsCount, screenshot["scrollable_container"] == true))
		} else {
			exec.Log("info", fmt.Sprintf("详情截图无分段: name=%s width=%d height=%d scrollable=%v parts_count=%d", name, intFromMap(screenshot, "width"), intFromMap(screenshot, "height"), stringFromMap(screenshot, "scrollable_container") == "true", intFromMap(screenshot, "parts_count")))
		}
		screenshot = stitchDetailScreenshot(exec, request.TaskID, request.ScreenshotsDir, candidate, screenshot)
	} else {
		exec.Log("warning", "详情截图返回为空")
	}
	return platformcore.DetailResult{Text: detailText, Screenshot: screenshot, Source: request.Mode}, nil
}

// CloseCandidateDetail 关闭 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	return r.closeCandidateDetail(ctx, exec, cfg, candidateName(candidate))
}

// CleanCandidateDetailText 清理 Boss 详情文本中的平台附加内容。
// text 为 OCR、DOM 或 AI 提取出的详情文本。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	cutMarkers := []string{
		"牛人分析器",
	}
	for _, marker := range cutMarkers {
		if index := strings.Index(text, marker); index >= 0 {
			text = strings.TrimSpace(text[:index])
		}
	}
	return text
}

// closeCandidateDetail 关闭 Boss 候选人详情。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidateName 为候选人名称。
func (r *Runtime) closeCandidateDetail(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidateName string) error {
	name := strings.TrimSpace(candidateName)
	if name == "" {
		name = "候选人"
	}
	exec.Log("info", "正在关闭"+name+"详情")
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/detail/close", map[string]any{
		"platform_config": cfg,
		"key":             "Escape",
		"candidate_name":  name,
	})
	if err == nil {
		exec.Log("info", name+"详情已关闭")
	}
	return err
}
