// 本文件负责从统一系统配置表读取 AI 默认提示词。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const defaultPromptsConfigKey = "ai.default_prompts"
const builtinReviewPrompt = `你是一个资深的HR专家。当前候选人分数接近岗位阈值，请做打招呼前二次复核评分。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 评分更关注风险点与关键硬指标。

岗位要求：
${岗位信息}

候选人信息：
${候选人信息}

请返回JSON：{"score": 72, "reason": "边界候选人可谨慎通过"}`

// DefaultPrompts 表示系统级 AI 默认提示词配置。
type DefaultPrompts struct {
	FilterPrompt     string `json:"filter_prompt"`
	OpenDetailPrompt string `json:"open_detail_prompt"`
	ReviewPrompt     string `json:"review_prompt"`
}

// loadDefaultPrompts 从 system_configs 中读取 AI 默认提示词。
func loadDefaultPrompts(store SystemConfigStore) DefaultPrompts {
	if store == nil {
		return DefaultPrompts{}
	}
	cfg, err := store.Get(defaultPromptsConfigKey)
	if err != nil {
		return DefaultPrompts{}
	}
	var prompts DefaultPrompts
	if err := json.Unmarshal([]byte(cfg.ConfigValue), &prompts); err != nil {
		return DefaultPrompts{}
	}
	prompts.FilterPrompt = strings.TrimSpace(prompts.FilterPrompt)
	prompts.OpenDetailPrompt = strings.TrimSpace(prompts.OpenDetailPrompt)
	prompts.ReviewPrompt = strings.TrimSpace(prompts.ReviewPrompt)
	if prompts.ReviewPrompt == "" {
		prompts.ReviewPrompt = builtinReviewPrompt
	}
	return prompts
}

// GetDefaultPrompts 返回当前系统默认提示词，供岗位模板空字段兜底。
func (s *Server) GetDefaultPrompts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, err := s.auth.SessionFromRequest(r); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "session is invalid or expired")
			return
		}
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"prompts": loadDefaultPrompts(s.systemConfigs),
	})
}
