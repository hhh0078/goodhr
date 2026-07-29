// Package cloud 文件作用：定义本地程序与 GoodHR 云端之间的强类型数据模型。
package cloud

import (
	"fmt"
	"strings"
)

// UserSession 表示云端登录状态。
type UserSession struct {
	UserID   string `json:"id"`
	LoggedIn bool   `json:"logged_in"`
}

// Subscription 表示会员可用状态。
type Subscription struct {
	Active     bool   `json:"active"`
	MemberType string `json:"member_type"`
	ExpiresAt  string `json:"expires_at"`
}

// APIError 表示云端返回的稳定 HTTP 状态错误。
type APIError struct {
	StatusCode int
	Message    string
}

// Error 返回适合本地日志和用户查看的云端错误。
func (e *APIError) Error() string {
	if strings.TrimSpace(e.Message) == "" {
		return fmt.Sprintf("云端请求失败，状态码 %d", e.StatusCode)
	}
	return e.Message
}

// AIConfig 表示云端下发给本地 AI 客户端的配置。
type AIConfig struct {
	BaseURL           string  `json:"base_url"`
	APIKey            string  `json:"api_key"`
	Model             string  `json:"model"`
	Temperature       float64 `json:"temperature"`
	ScoreThreshold    float64 `json:"score_threshold"`
	SystemPrompt      string  `json:"system_prompt"`
	ReplySystemPrompt string  `json:"reply_system_prompt"`
	PromptTemplate    string  `json:"prompt_template"`
}

// PositionCommonConfig 表示云端岗位公共运行配置。
type PositionCommonConfig struct {
	ModeDefault               string `json:"mode_default"`
	DetailMode                string `json:"detail_mode"`
	ScanRounds                int    `json:"scan_rounds"`
	RequestPhone              bool   `json:"request_phone"`
	RequestWechat             bool   `json:"request_wechat"`
	RequestResume             bool   `json:"request_resume"`
	HLiepinShortcutSearchName string `json:"hliepin_shortcut_search_name"`
}

// PositionAIOptions 表示岗位级 AI 阈值和提示词。
type PositionAIOptions struct {
	GreetScoreThreshold   float64 `json:"greet_score_threshold"`
	RequestScoreThreshold float64 `json:"request_score_threshold"`
	GreetPrompt           string  `json:"greet_prompt"`
	ReplyPrompt           string  `json:"reply_prompt"`
}

// UserPreferences 表示云端个人配置中的拟人等待和休息参数。
type UserPreferences struct {
	AIModel                string  `json:"ai_model"`
	ClickFrequency         int     `json:"click_frequency"`
	DetailOpenProbability  int     `json:"detail_open_probability"`
	ScrollDelayMin         int     `json:"scroll_delay_min"`
	ScrollDelayMax         int     `json:"scroll_delay_max"`
	ListViewDelayMin       float64 `json:"list_view_delay_min"`
	ListViewDelayMax       float64 `json:"list_view_delay_max"`
	DetailViewDelayMin     float64 `json:"detail_view_delay_min"`
	DetailViewDelayMax     float64 `json:"detail_view_delay_max"`
	GreetDelayMin          float64 `json:"greet_delay_min"`
	GreetDelayMax          float64 `json:"greet_delay_max"`
	DetailOpenDelayMin     float64 `json:"detail_open_delay_min"`
	DetailOpenDelayMax     float64 `json:"detail_open_delay_max"`
	DetailCloseDelayMin    float64 `json:"detail_close_delay_min"`
	DetailCloseDelayMax    float64 `json:"detail_close_delay_max"`
	GreetBeforeDelayMin    float64 `json:"greet_before_delay_min"`
	GreetBeforeDelayMax    float64 `json:"greet_before_delay_max"`
	RestAfterCandidatesMin int     `json:"rest_after_candidates_min"`
	RestAfterCandidatesMax int     `json:"rest_after_candidates_max"`
	RestTimesMin           int     `json:"rest_times_min"`
	RestTimesMax           int     `json:"rest_times_max"`
	RestDurationMin        float64 `json:"rest_duration_min"`
	RestDurationMax        float64 `json:"rest_duration_max"`
}

// PositionSnapshot 表示任务启动时冻结的岗位配置。
type PositionSnapshot struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	PlatformID      string               `json:"platform_id"`
	ProfileID       string               `json:"profile_id"`
	Keyword         string               `json:"keyword"`
	Keywords        []string             `json:"keywords"`
	ExcludeKeywords []string             `json:"exclude_keywords"`
	IsAndMode       bool                 `json:"is_and_mode"`
	Description     string               `json:"description"`
	GreetMessage    string               `json:"greet_message"`
	RequestPhone    bool                 `json:"request_phone"`
	RequestWechat   bool                 `json:"request_wechat"`
	RequestResume   bool                 `json:"request_resume"`
	MatchLimit      int                  `json:"match_limit"`
	MaxBatches      int                  `json:"max_batches"`
	ScannedCount    int                  `json:"scanned_count"`
	GreetedCount    int                  `json:"greeted_count"`
	SkippedCount    int                  `json:"skipped_count"`
	FailedCount     int                  `json:"failed_count"`
	EnableSound     bool                 `json:"enable_sound"`
	EnableThinking  bool                 `json:"enable_thinking"`
	RequiresAI      bool                 `json:"requires_ai"`
	RequiresOCR     bool                 `json:"requires_ocr"`
	AutoReplyWait   int                  `json:"auto_reply_wait_seconds"`
	CommonConfig    PositionCommonConfig `json:"common_config"`
	AIOptions       PositionAIOptions    `json:"ai_config"`
	AI              AIConfig             `json:"ai"`
}

// TaskSummary 表示同步到云端的不含敏感数据任务摘要。
type TaskSummary struct {
	TaskID       string `json:"task_id"`
	PositionID   string `json:"position_id"`
	TaskType     string `json:"task_type,omitempty"`
	Status       string `json:"status"`
	Processed    int    `json:"processed"`
	Succeeded    int    `json:"succeeded"`
	Skipped      int    `json:"skipped"`
	Failed       int    `json:"failed"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// SummaryResult 表示云端状态同步和完成邮件结果。
type SummaryResult struct {
	Success    bool `json:"success"`
	NoticeSent bool `json:"notice_sent"`
}
