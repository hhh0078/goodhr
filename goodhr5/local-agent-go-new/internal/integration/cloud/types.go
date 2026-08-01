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
	Active         bool     `json:"active"`
	MemberType     string   `json:"member_type"`
	MemberName     string   `json:"member_name"`
	ExpiresAt      string   `json:"expires_at"`
	RemainingDays  int      `json:"remaining_days"`
	AllowAI        bool     `json:"allow_ai"`
	AllowAutoReply bool     `json:"allow_auto_reply"`
	Features       []string `json:"features"`
}

// APIError 表示云端返回的稳定 HTTP 状态错误。
type APIError struct {
	StatusCode int
	Code       string
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
	OutputStructuredResume    bool   `json:"output_structured_resume"`
	RequestPhone              bool   `json:"request_phone"`
	RequestWechat             bool   `json:"request_wechat"`
	RequestResume             bool   `json:"request_resume"`
	HLiepinShortcutSearchName string `json:"hliepin_shortcut_search_name"`
}

// PositionAIOptions 表示岗位级 AI 阈值和提示词。
type PositionAIOptions struct {
	PositionRequirement   string  `json:"position_requirement"`
	OpenDetailPrompt      string  `json:"open_detail_prompt"`
	DetailScoreThreshold  float64 `json:"detail_score_threshold"`
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

// CandidateWorkExperience 表示 AI 识别出的候选人工作经历。
type CandidateWorkExperience struct {
	CompanyName  string `json:"company_name"`
	PositionName string `json:"position_name"`
	Content      string `json:"content"`
	StartYM      string `json:"start_ym"`
	EndYM        string `json:"end_ym"`
}

// CandidateEducation 表示 AI 识别出的候选人教育经历。
type CandidateEducation struct {
	SchoolName     string `json:"school_name"`
	MajorName      string `json:"major_name"`
	EducationLevel string `json:"education_level"`
	StartYM        string `json:"start_ym"`
	EndYM          string `json:"end_ym"`
}

// CandidateCertificate 表示 AI 识别出的候选人证书。
type CandidateCertificate struct {
	CertificateName string `json:"certificate_name"`
	IssuedBy        string `json:"issued_by"`
	IssuedYM        string `json:"issued_ym"`
}

// CandidateHonor 表示 AI 识别出的候选人荣誉。
type CandidateHonor struct {
	HonorName   string `json:"honor_name"`
	IssuedBy    string `json:"issued_by"`
	IssuedYM    string `json:"issued_ym"`
	Description string `json:"description"`
}

// CandidateProjectExperience 表示 AI 识别出的候选人项目经历。
type CandidateProjectExperience struct {
	ProjectName string `json:"project_name"`
	RoleName    string `json:"role_name"`
	Content     string `json:"content"`
	StartYM     string `json:"start_ym"`
	EndYM       string `json:"end_ym"`
}

// CandidateCommunication 表示 AI 识别出的候选人历史沟通摘要。
type CandidateCommunication struct {
	CommunicatorName string `json:"communicator_name"`
	CommunicatedAt   string `json:"communicated_at"`
	Content          string `json:"content"`
}

// StructuredCandidate 表示允许异步同步到云端简历库的结构化候选人字段。
type StructuredCandidate struct {
	CandidateName           string                       `json:"candidate_name,omitempty"`
	BirthYM                 string                       `json:"birth_ym,omitempty"`
	Phone                   string                       `json:"phone,omitempty"`
	Email                   string                       `json:"email,omitempty"`
	WorkRegion              string                       `json:"work_region,omitempty"`
	WorkYears               string                       `json:"work_years,omitempty"`
	ExpectedSalaryMin       *int                         `json:"expected_salary_min,omitempty"`
	ExpectedSalaryMax       *int                         `json:"expected_salary_max,omitempty"`
	EducationLevel          string                       `json:"education_level,omitempty"`
	ExpectedPosition        string                       `json:"expected_position,omitempty"`
	OnlineStatus            string                       `json:"online_status,omitempty"`
	PersonalDescription     string                       `json:"personal_description,omitempty"`
	WorkStatus              string                       `json:"work_status,omitempty"`
	RawText                 string                       `json:"raw_text,omitempty"`
	WorkExperiences         []CandidateWorkExperience    `json:"work_experiences,omitempty"`
	Educations              []CandidateEducation         `json:"educations,omitempty"`
	Certificates            []CandidateCertificate       `json:"certificates,omitempty"`
	Honors                  []CandidateHonor             `json:"honors,omitempty"`
	ProjectExperiences      []CandidateProjectExperience `json:"project_experiences,omitempty"`
	ColleagueCommunications []CandidateCommunication     `json:"colleague_communications,omitempty"`
}

// CandidateUpload 表示本地程序异步写入云端简历库的候选人结果。
type CandidateUpload struct {
	StructuredCandidate
	PlatformID          string   `json:"platform_id"`
	PlatformCandidateID string   `json:"id,omitempty"`
	BasicInfo           string   `json:"basic_info,omitempty"`
	Status              string   `json:"status"`
	AIDetailReason      string   `json:"ai_detail_reason,omitempty"`
	AIDetailScore       *float64 `json:"ai_detail_score,omitempty"`
	AIGreetReason       string   `json:"ai_greet_reason,omitempty"`
	AIGreetScore        *float64 `json:"ai_greet_score,omitempty"`
}

// TaskSummary 表示同步到云端的不含敏感数据任务摘要。
type TaskSummary struct {
	TaskID          string `json:"task_id"`
	PositionID      string `json:"position_id"`
	TaskType        string `json:"task_type,omitempty"`
	Status          string `json:"status"`
	Processed       int    `json:"processed"`
	Succeeded       int    `json:"succeeded"`
	Skipped         int    `json:"skipped"`
	Failed          int    `json:"failed"`
	RunGreetedCount int    `json:"run_greeted_count"`
	RunSkippedCount int    `json:"run_skipped_count"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// SummaryResult 表示云端状态同步和完成邮件结果。
type SummaryResult struct {
	Success    bool `json:"success"`
	NoticeSent bool `json:"notice_sent"`
}
