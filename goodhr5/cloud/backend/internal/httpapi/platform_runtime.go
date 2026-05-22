// 本文件负责平台运行时注册与分发，让主流程只通过统一入口调用平台实现。
package httpapi

import (
	"strings"

	"goodhr5/cloud/backend/internal/platformcore"
	"goodhr5/cloud/backend/internal/platforms/boss"
)

type platformViewportExecutor interface {
	post(path string, body any, result any) error
	log(level, message string)
}

type runtimeExecutorAdapter struct {
	exec platformViewportExecutor
}

// Post 调用 httpapi 执行器发送请求。
func (a runtimeExecutorAdapter) Post(path string, body any, result any) error {
	return a.exec.post(path, body, result)
}

// Log 调用 httpapi 执行器输出日志。
func (a runtimeExecutorAdapter) Log(level, message string) {
	a.exec.log(level, message)
}

var platformRuntimeRegistry = map[string]platformcore.PlatformRuntime{
	"boss": boss.NewRuntime(),
}

// runtimeByPlatformID 根据平台 ID 获取运行时实现。
func runtimeByPlatformID(platformID string) (platformcore.PlatformRuntime, error) {
	id := strings.ToLower(strings.TrimSpace(platformID))
	rt, ok := platformRuntimeRegistry[id]
	if !ok {
		return nil, platformcore.ErrRuntimeNotImplemented(platformID)
	}
	return rt, nil
}

// OpenEntryPage 由平台运行时逻辑打开任务入口页面。
func (cfg PlatformConfig) OpenEntryPage(exec platformViewportExecutor, cookies []map[string]any) error {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return err
	}
	return rt.OpenEntryPage(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), cookies)
}

// ListVisibleCandidates 由平台运行时逻辑提取当前可见候选人摘要。
func (cfg PlatformConfig) ListVisibleCandidates(exec platformViewportExecutor) ([]Candidate, error) {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return nil, err
	}
	return rt.ListVisibleCandidates(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig())
}

// ScrollCandidateList 由平台运行时逻辑滚动到候选人列表下一屏。
func (cfg PlatformConfig) ScrollCandidateList(exec platformViewportExecutor, prefs UserPreferences) error {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return err
	}
	return rt.ScrollCandidateList(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), toRuntimePreferences(prefs))
}

// GreetCandidate 由平台运行时逻辑执行打招呼动作。
func (cfg PlatformConfig) GreetCandidate(exec platformViewportExecutor, prefs UserPreferences, candidate Candidate) error {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return err
	}
	return rt.GreetCandidate(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), toRuntimePreferences(prefs), candidate)
}

// OpenCandidateDetail 由平台运行时逻辑打开候选人详情。
func (cfg PlatformConfig) OpenCandidateDetail(exec platformViewportExecutor, prefs UserPreferences, candidate Candidate) error {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return err
	}
	return rt.OpenCandidateDetail(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), toRuntimePreferences(prefs), candidate)
}

// CloseCandidateDetail 由平台运行时逻辑关闭候选人详情。
func (cfg PlatformConfig) CloseCandidateDetail(exec platformViewportExecutor, prefs UserPreferences) error {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return err
	}
	return rt.CloseCandidateDetail(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), toRuntimePreferences(prefs))
}

// FetchCandidateDetailText 由平台运行时逻辑返回详情文本。
func (cfg PlatformConfig) FetchCandidateDetailText(exec platformViewportExecutor, prefs UserPreferences, candidate Candidate, detailMode string) (string, error) {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return "", err
	}
	return rt.FetchCandidateDetailText(runtimeExecutorAdapter{exec: exec}, cfg.toRuntimeConfig(), toRuntimePreferences(prefs), candidate, detailMode)
}

// CandidateFilterText 由平台运行时逻辑拼接候选人筛选文本。
func (cfg PlatformConfig) CandidateFilterText(candidate Candidate) string {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return strings.TrimSpace(candidate.FilterText)
	}
	return rt.CandidateFilterText(candidate)
}

// CandidateFingerprint 由平台运行时逻辑生成候选人去重指纹。
func (cfg PlatformConfig) CandidateFingerprint(candidate Candidate) string {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return ""
	}
	return rt.CandidateFingerprint(candidate)
}

// MapFieldsToCandidate 由平台运行时逻辑映射候选人字段。
func (cfg PlatformConfig) MapFieldsToCandidate(fields map[string]any) (Candidate, error) {
	rt, err := runtimeByPlatformID(cfg.ID)
	if err != nil {
		return Candidate{}, err
	}
	return rt.MapFieldsToCandidate(cfg.ID, fields), nil
}

// toRuntimeConfig 将平台配置转换为 runtime 公共配置快照。
func (cfg PlatformConfig) toRuntimeConfig() platformcore.RuntimeConfig {
	pages := make([]platformcore.RuntimePage, 0, len(cfg.Auth.Pages))
	for _, page := range cfg.Auth.Pages {
		pages = append(pages, platformcore.RuntimePage{
			URL:   page.URL,
			Title: page.Title,
			Entry: page.Entry,
		})
	}
	return platformcore.RuntimeConfig{
		PlatformID: cfg.ID,
		EntryPages: pages,
		Card: platformcore.RuntimeCardConfig{
			CardElement:   cfg.Card.CardElement(),
			ScrollElement: cfg.Card.ScrollElement(),
			FieldRequests: cfg.Card.ExtractFieldRequests(),
		},
		Actions: platformcore.RuntimeActionsConfig{
			GreetBtn:    cfg.Actions.GreetBtn.AsPayload(),
			ContinueBtn: cfg.Actions.ContinueBtn.AsPayload(),
			ConfirmBtn:  cfg.Actions.ConfirmBtn.AsPayload(),
		},
		Detail: platformcore.RuntimeDetailConfig{
			OpenTarget: cfg.Detail.OpenTarget.AsPayload(),
			CloseBtn:   cfg.Detail.CloseBtn.AsPayload(),
			Content:    cfg.Detail.Content.AsPayload(),
		},
		Behavior: platformcore.RuntimeBehaviorConfig{
			NeedsDetailPage: cfg.Behavior.NeedsDetailPage,
		},
		PlatformName: cfg.Name,
	}
}

// toRuntimePreferences 将用户偏好转换为 runtime 公共偏好。
func toRuntimePreferences(prefs UserPreferences) platformcore.RuntimePreferences {
	return platformcore.RuntimePreferences{
		ScrollDelayMin: prefs.ScrollDelayMin,
		ScrollDelayMax: prefs.ScrollDelayMax,
		GreetDelayMin:  prefs.GreetDelayMin,
		GreetDelayMax:  prefs.GreetDelayMax,
		DetailDelayMin: prefs.DetailViewDelayMin,
		DetailDelayMax: prefs.DetailViewDelayMax,
	}
}
