// 本文件负责封装平台运行时行为，让任务主流程通过平台方法完成候选人可视区控制。
package httpapi

import (
	"fmt"
	"strings"
)

type localViewportResp struct {
	Ok         bool   `json:"ok"`
	InViewport bool   `json:"in_viewport"`
	Matched    string `json:"matched"`
}

type platformViewportExecutor interface {
	post(path string, body any, result any) error
	log(level, message string)
}

// EnsureCandidateVisible 由平台运行时逻辑确保候选人卡片位于可视区域。
func (cfg PlatformConfig) EnsureCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	switch strings.TrimSpace(cfg.ID) {
	case "boss":
		return cfg.ensureBossCandidateVisible(exec, elementRef)
	default:
		return cfg.ensureDefaultCandidateVisible(exec, elementRef)
	}
}

// ensureBossCandidateVisible 确保 Boss 候选人卡片进入可视区域后再读取字段。
func (cfg PlatformConfig) ensureBossCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	return ensureCandidateVisibleWithViewport(exec, elementRef, "Boss候选人卡片")
}

// ensureDefaultCandidateVisible 使用默认逻辑确保候选人卡片进入可视区域。
func (cfg PlatformConfig) ensureDefaultCandidateVisible(exec platformViewportExecutor, elementRef string) error {
	return ensureCandidateVisibleWithViewport(exec, elementRef, "候选人卡片")
}

// ensureCandidateVisibleWithViewport 通过本地原子接口判断并滚动元素到视口内。
func ensureCandidateVisibleWithViewport(exec platformViewportExecutor, elementRef string, label string) error {
	if strings.TrimSpace(elementRef) == "" {
		return nil
	}
	var viewportResp localViewportResp
	if err := exec.post("/api/v1/page/in-viewport", map[string]any{
		"element_ref": elementRef,
	}, &viewportResp); err != nil {
		return err
	}
	if viewportResp.InViewport {
		exec.log("info", fmt.Sprintf("%s已在当前视口内：%s", label, elementRef))
		return nil
	}
	exec.log("info", fmt.Sprintf("%s不在当前视口内，准备滚动到视口：%s", label, elementRef))
	if err := exec.post("/api/v1/page/scroll-into-view", map[string]any{
		"element_ref": elementRef,
	}, &viewportResp); err != nil {
		return err
	}
	exec.log("info", fmt.Sprintf("%s已滚动到视口内：%s", label, viewportResp.Matched))
	return nil
}
