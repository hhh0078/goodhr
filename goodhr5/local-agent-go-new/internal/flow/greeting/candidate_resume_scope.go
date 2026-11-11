// Package greeting 文件作用：按实际岗位条件生成断点摘要，保留真实查询条件并定位进度失效原因，不保存条件原文。
package greeting

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
)

// candidateResumeScope 返回账号隔离键及按实际运行含义归一后的条件摘要。
func candidateResumeScope(prepared shared.PreparedTask, pageURL string) (string, string, error) {
	key, scope, _, err := candidateResumeScopeHashes(prepared, pageURL, true)
	return key, scope, err
}

// candidateResumeScopeHashes 同时生成各条件摘要；normalize为false时保留旧版字节格式，仅用于校验旧断点。
func candidateResumeScopeHashes(prepared shared.PreparedTask, pageURL string, normalize bool) (string, string, map[string]string, error) {
	if normalize {
		pageURL = candidateResumePageURL(prepared, pageURL)
		enabled := prepared.Request.EnableGreet == nil || *prepared.Request.EnableGreet
		prepared.Request.EnableGreet = &enabled
	}
	identity, err := json.Marshal([]string{prepared.Session.UserID, prepared.Platform.ID, prepared.Position.ID,
		prepared.ProfilePath, prepared.Request.ProfileID, prepared.Request.PlatformAccountID})
	if err != nil {
		return "", "", nil, err
	}
	position := prepared.Position
	scope, err := json.Marshal(struct {
		PageURL         string                     `json:"page_url"`
		Name            string                     `json:"name"`
		Keyword         string                     `json:"keyword"`
		Keywords        []string                   `json:"keywords"`
		ExcludeKeywords []string                   `json:"exclude_keywords"`
		IsAndMode       bool                       `json:"is_and_mode"`
		Description     string                     `json:"description"`
		CommonConfig    cloud.PositionCommonConfig `json:"common_config"`
		AIOptions       cloud.PositionAIOptions    `json:"ai_options"`
		EnableGreet     *bool                      `json:"enable_greet"`
		RequestPhone    bool                       `json:"request_phone"`
		RequestWechat   bool                       `json:"request_wechat"`
		RequestResume   bool                       `json:"request_resume"`
	}{pageURL, position.Name, position.Keyword, position.Keywords, position.ExcludeKeywords, position.IsAndMode,
		position.Description, position.CommonConfig, position.AIOptions, prepared.Request.EnableGreet,
		position.RequestPhone, position.RequestWechat, position.RequestResume})
	if err != nil {
		return "", "", nil, err
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(scope, &values); err != nil {
		return "", "", nil, err
	}
	fields := make(map[string]string, len(values))
	for key, raw := range values {
		fields[key] = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return fmt.Sprintf("greeting_resume:%x", sha256.Sum256(identity)), fmt.Sprintf("%x", sha256.Sum256(scope)), fields, nil
}

// candidateResumePageURL 仅对配置允许的推荐入口去除追踪参数，其他查询条件和页面路由始终参与断点隔离。
func candidateResumePageURL(prepared shared.PreparedTask, raw string) string {
	if len(prepared.Platform.CandidateResumeIgnoredQueryKeys) == 0 || !common.PageURLMatches(raw, prepared.Platform.EntryURL) {
		return raw
	}
	page, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query, err := url.ParseQuery(page.RawQuery)
	if err != nil {
		return raw
	}
	for _, key := range prepared.Platform.CandidateResumeIgnoredQueryKeys {
		query.Del(key)
	}
	page.RawQuery = query.Encode()
	return page.String()
}

// candidateResumeChangeReason 只输出变化字段的中文类别；旧断点没有分项摘要时明确说明，避免猜测具体原因。
func candidateResumeChangeReason(previous, current map[string]string) string {
	if len(previous) == 0 {
		return "旧版进度与当前条件没对上，旧记录没有具体差异；这次从头检查"
	}
	var changed []string
	for _, field := range []struct{ key, label string }{
		{"page_url", "页面或筛选参数"}, {"name", "岗位名称"}, {"keyword", "关键词"},
		{"keywords", "关键词列表"}, {"exclude_keywords", "排除词"}, {"is_and_mode", "关键词规则"},
		{"description", "岗位要求"}, {"common_config", "岗位运行设置"}, {"ai_options", "AI筛选设置"},
		{"enable_greet", "打招呼开关"}, {"request_phone", "电话索要设置"},
		{"request_wechat", "微信索要设置"}, {"request_resume", "简历索要设置"},
	} {
		if previous[field.key] != current[field.key] {
			changed = append(changed, field.label)
		}
	}
	return "上次进度未复用，变化项：" + strings.Join(changed, "、") + "；这次从头检查"
}
