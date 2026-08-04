// Package cloud 本文件定义本地自动回复流程与云端之间的强类型协议。
package cloud

import (
	"encoding/json"
	"time"
)

const (
	// AutoReplyMaxAttachmentBytes 是本地和云端共同接受的单个简历附件上限。
	AutoReplyMaxAttachmentBytes int64 = 20 * 1024 * 1024
	// AutoReplyMaxHistoryMessages 是首次聊天同步允许的最大消息条数。
	AutoReplyMaxHistoryMessages = 5000
)

// AgentCredentials 表示本地 Agent 调用敏感自动回复接口所需的凭证。
type AgentCredentials struct {
	Token     string
	MachineID string
}

// AutoReplyPosition 表示自动回复快照中的最小岗位信息。
type AutoReplyPosition struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platform_id"`
	Status     string `json:"status"`
}

// AutoReplySubscription 表示自动回复快照中的会员权限。
type AutoReplySubscription struct {
	Active           bool      `json:"active"`
	MemberType       string    `json:"member_type"`
	MemberName       string    `json:"member_name"`
	ExpiresAt        time.Time `json:"expires_at"`
	RemainingDays    int       `json:"remaining_days"`
	RemainingSeconds int64     `json:"remaining_seconds"`
	AllowAI          bool      `json:"allow_ai"`
	AllowAutoReply   bool      `json:"allow_auto_reply"`
	Features         []string  `json:"features"`
}

// CompanyProfile 表示岗位选中的团队公司档案。
type CompanyProfile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Contact   string    `json:"contact"`
	Overview  string    `json:"overview"`
	ExtraInfo string    `json:"extra_info"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PositionReplyCondition 表示自动回复需要遵守或确认的一条岗位条件。
type PositionReplyCondition struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	DedupeKey string `json:"dedupe_key"`
	SortOrder int    `json:"sort_order"`
	Enabled   bool   `json:"enabled"`
}

// PositionAutoReplyConfig 表示岗位自动回复的完整配置。
type PositionAutoReplyConfig struct {
	PositionID              string                   `json:"position_id"`
	CompanyProfileID        string                   `json:"company_profile_id"`
	Enabled                 bool                     `json:"enabled"`
	PositionDescription     string                   `json:"position_description"`
	ResumeRequestMessage    string                   `json:"resume_request_message"`
	PollIntervalSeconds     int                      `json:"poll_interval_seconds"`
	MaxThreadsPerCheckpoint int                      `json:"max_threads_per_checkpoint"`
	Version                 int                      `json:"version"`
	Conditions              []PositionReplyCondition `json:"conditions"`
	UpdatedAt               time.Time                `json:"updated_at"`
}

// AutoReplyPositionSnapshot 表示一次本地自动回复检查使用的岗位冻结数据。
type AutoReplyPositionSnapshot struct {
	OK             bool                    `json:"ok"`
	Position       AutoReplyPosition       `json:"position"`
	Config         PositionAutoReplyConfig `json:"config"`
	CompanyProfile CompanyProfile          `json:"company_profile"`
	Subscription   AutoReplySubscription   `json:"subscription"`
}

// AutoReplyPositionSnapshots 表示当前招聘平台全部已开启自动回复的岗位快照。
type AutoReplyPositionSnapshots struct {
	OK        bool                        `json:"ok"`
	Positions []AutoReplyPositionSnapshot `json:"positions"`
}

// AutoReplyPositionStatus 表示岗位自动回复的实时开关和权限状态。
type AutoReplyPositionStatus struct {
	OK                bool                  `json:"ok"`
	Enabled           bool                  `json:"enabled"`
	ConfiguredEnabled bool                  `json:"configured_enabled"`
	Version           int                   `json:"version"`
	Subscription      AutoReplySubscription `json:"subscription"`
}

// CandidatePlatformIdentity 表示候选人在一个招聘平台账号中的稳定身份。
type CandidatePlatformIdentity struct {
	ID                  string    `json:"id"`
	CandidateID         string    `json:"candidate_id"`
	PlatformID          string    `json:"platform_id"`
	PlatformAccountID   string    `json:"platform_account_id"`
	PlatformCandidateID string    `json:"platform_candidate_id"`
	CandidateName       string    `json:"candidate_name"`
	Gender              string    `json:"gender"`
	NormalizedPhone     string    `json:"normalized_phone"`
	FirstSeenAt         time.Time `json:"first_seen_at"`
	LastSeenAt          time.Time `json:"last_seen_at"`
}

// AutoReplyConversation 表示可以先于正式简历存在的平台聊天会话。
type AutoReplyConversation struct {
	ID                      string     `json:"id"`
	CandidateID             string     `json:"candidate_id"`
	PlatformIdentityID      string     `json:"platform_identity_id"`
	EngagementID            string     `json:"engagement_id"`
	PositionID              string     `json:"position_id"`
	PlatformAccountID       string     `json:"platform_account_id"`
	PlatformID              string     `json:"platform_id"`
	PlatformThreadID        string     `json:"platform_thread_id"`
	CandidateName           string     `json:"candidate_name"`
	Gender                  string     `json:"gender"`
	PagePositionText        string     `json:"page_position_text"`
	Status                  string     `json:"status"`
	HistoryComplete         bool       `json:"history_complete"`
	LastSyncedMessageKey    string     `json:"last_synced_message_key"`
	LastCandidateMessageKey string     `json:"last_candidate_message_key"`
	UnresolvedReason        string     `json:"unresolved_reason"`
	LastCheckedAt           *time.Time `json:"last_checked_at"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// AutoReplyMessage 表示一条经过本地清洗的聊天消息。
type AutoReplyMessage struct {
	ID                string          `json:"id,omitempty"`
	ConversationID    string          `json:"conversation_id,omitempty"`
	PlatformMessageID string          `json:"platform_message_id"`
	Fingerprint       string          `json:"fingerprint"`
	Direction         string          `json:"direction"`
	MessageType       string          `json:"message_type"`
	TextContent       string          `json:"text_content"`
	CardContent       json.RawMessage `json:"card_content"`
	SenderName        string          `json:"sender_name"`
	PlatformSentAt    *time.Time      `json:"platform_sent_at"`
	IngestedAt        time.Time       `json:"ingested_at"`
	CreatedAt         time.Time       `json:"created_at"`
}

// AutoReplyMessageSyncRequest 表示一次首次或差量聊天同步。
type AutoReplyMessageSyncRequest struct {
	ConversationID  string             `json:"conversation_id"`
	HistoryComplete bool               `json:"history_complete"`
	Messages        []AutoReplyMessage `json:"messages"`
}

// AutoReplyMessageSyncResult 表示云端幂等保存后的差量游标。
type AutoReplyMessageSyncResult struct {
	Inserted                int    `json:"inserted"`
	LastSyncedMessageKey    string `json:"last_synced_message_key"`
	LastCandidateMessageKey string `json:"last_candidate_message_key"`
}

// StoredResumeAttachment 表示已保存到云端持久化目录的简历附件元数据。
type StoredResumeAttachment struct {
	ID              string    `json:"id"`
	CandidateID     string    `json:"candidate_id"`
	ConversationID  string    `json:"conversation_id"`
	SourceMessageID string    `json:"source_message_id"`
	PlatformID      string    `json:"platform_id"`
	OriginalName    string    `json:"original_name"`
	SHA256          string    `json:"sha256"`
	MIMEType        string    `json:"mime_type"`
	SizeBytes       int64     `json:"size_bytes"`
	ExtractedText   string    `json:"extracted_text"`
	CreatedAt       time.Time `json:"created_at"`
}

// CandidateConfirmationItem 表示候选人与岗位之间的一条结构化确认项。
type CandidateConfirmationItem struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	CandidateID    string    `json:"candidate_id"`
	PositionID     string    `json:"position_id"`
	ItemType       string    `json:"item_type"`
	Content        string    `json:"content"`
	DedupeKey      string    `json:"dedupe_key"`
	Status         string    `json:"status"`
	SourceType     string    `json:"source_type"`
	SourceRef      string    `json:"source_ref"`
	EvidenceText   string    `json:"evidence_text"`
	Summary        string    `json:"summary"`
	CreatedByKind  string    `json:"created_by_kind"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AutoReplyAIRun 表示一次自动回复 AI 总记录。
type AutoReplyAIRun struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversation_id"`
	CandidateID       string          `json:"candidate_id"`
	PositionID        string          `json:"position_id"`
	TraceID           string          `json:"trace_id"`
	Model             string          `json:"model"`
	Status            string          `json:"status"`
	BasedOnMessageKey string          `json:"based_on_message_key"`
	InputMessages     json.RawMessage `json:"input_messages"`
	OutputMessage     json.RawMessage `json:"output_message"`
	ErrorCode         string          `json:"error_code"`
	ErrorMessage      string          `json:"error_message"`
	TokenUsage        int             `json:"token_usage"`
	StartedAt         time.Time       `json:"started_at"`
	CompletedAt       *time.Time      `json:"completed_at"`
	ExpiresAt         time.Time       `json:"expires_at"`
	CreatedAt         time.Time       `json:"created_at"`
}

// AutoReplyToolCall 表示一次 AI 工具调用的参数、结果和状态。
type AutoReplyToolCall struct {
	ID            string          `json:"id"`
	AIRunID       string          `json:"ai_run_id"`
	ToolCallID    string          `json:"tool_call_id"`
	SequenceNo    int             `json:"sequence_no"`
	ToolName      string          `json:"tool_name"`
	ArgumentsJSON json.RawMessage `json:"arguments_json"`
	ResultJSON    json.RawMessage `json:"result_json"`
	Status        string          `json:"status"`
	ErrorCode     string          `json:"error_code"`
	ErrorMessage  string          `json:"error_message"`
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   *time.Time      `json:"completed_at"`
	CreatedAt     time.Time       `json:"created_at"`
}

// AutoReplyConfigSuggestion 表示等待 HR 审核的岗位或公司资料建议。
type AutoReplyConfigSuggestion struct {
	ID               string          `json:"id"`
	ConversationID   string          `json:"conversation_id"`
	PositionID       string          `json:"position_id"`
	CompanyProfileID string          `json:"company_profile_id"`
	SuggestionType   string          `json:"suggestion_type"`
	Operation        string          `json:"operation"`
	TargetID         string          `json:"target_id"`
	ProposedValue    json.RawMessage `json:"proposed_value"`
	Reason           string          `json:"reason"`
	Status           string          `json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
}

// AutoReplyNotification 表示一封需要 HR 人工接管的幂等通知。
type AutoReplyNotification struct {
	ID                string     `json:"id"`
	ConversationID    string     `json:"conversation_id"`
	PositionID        string     `json:"position_id"`
	BasedOnMessageKey string     `json:"based_on_message_key"`
	ReasonKey         string     `json:"reason_key"`
	CandidateName     string     `json:"candidate_name"`
	Gender            string     `json:"gender"`
	PlatformID        string     `json:"platform_id"`
	Reason            string     `json:"reason"`
	LatestMessage     string     `json:"latest_message"`
	Status            string     `json:"status"`
	ErrorMessage      string     `json:"error_message"`
	SentAt            *time.Time `json:"sent_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// AutoReplyNotificationResult 表示人工接管邮件的幂等发送结果。
type AutoReplyNotificationResult struct {
	Notification AutoReplyNotification `json:"notification"`
	EmailSent    bool                  `json:"email_sent"`
	Duplicate    bool                  `json:"duplicate"`
	Warning      string                `json:"warning"`
}

// AutoReplyCandidateLookup 表示按平台身份优先、手机号后备的候选人查询条件。
type AutoReplyCandidateLookup struct {
	PlatformID          string
	PlatformAccountID   string
	PlatformCandidateID string
	PlatformThreadID    string
	Phone               string
}

// AutoReplyStoredCandidate 表示云端已有的正式简历。
// 当前候选人存储模型仍使用 Go 字段名输出，因此这里显式兼容该现有协议。
type AutoReplyStoredCandidate struct {
	ID                  string                       `json:"ID"`
	CandidateName       string                       `json:"CandidateName"`
	Gender              string                       `json:"Gender"`
	BirthYM             string                       `json:"BirthYM"`
	BirthYMPrecision    string                       `json:"BirthYMPrecision"`
	Phone               string                       `json:"Phone"`
	Email               string                       `json:"Email"`
	Wechat              string                       `json:"Wechat"`
	WorkRegion          string                       `json:"WorkRegion"`
	WorkYears           string                       `json:"WorkYears"`
	ExpectedSalaryMin   *int                         `json:"ExpectedSalaryMin"`
	ExpectedSalaryMax   *int                         `json:"ExpectedSalaryMax"`
	BasicInfo           string                       `json:"BasicInfo"`
	EducationLevel      string                       `json:"EducationLevel"`
	ExpectedPosition    string                       `json:"ExpectedPosition"`
	OnlineStatus        string                       `json:"OnlineStatus"`
	PersonalDescription string                       `json:"PersonalDescription"`
	WorkStatus          string                       `json:"WorkStatus"`
	RawText             string                       `json:"RawText"`
	WorkExperiences     []CandidateWorkExperience    `json:"WorkExperiences"`
	Educations          []CandidateEducation         `json:"Educations"`
	Certificates        []CandidateCertificate       `json:"Certificates"`
	Honors              []CandidateHonor             `json:"Honors"`
	ProjectExperiences  []CandidateProjectExperience `json:"ProjectExperiences"`
	Communications      []CandidateCommunication     `json:"Communications"`
}

// AutoReplyCandidateState 表示已有简历、身份、会话和附件状态。
type AutoReplyCandidateState struct {
	OK                  bool                       `json:"ok"`
	Found               bool                       `json:"found"`
	HasResumeAttachment bool                       `json:"has_resume_attachment"`
	Candidate           *AutoReplyStoredCandidate  `json:"candidate,omitempty"`
	Identity            *CandidatePlatformIdentity `json:"identity,omitempty"`
	Conversation        *AutoReplyConversation     `json:"conversation,omitempty"`
	Attachments         []StoredResumeAttachment   `json:"attachments"`
}

// AutoReplyCandidateInput 表示本地清洗并校验后准备正式入库的简历。
type AutoReplyCandidateInput struct {
	StructuredCandidate
	PositionID          string `json:"position_id"`
	PlatformID          string `json:"platform_id"`
	PlatformAccountID   string `json:"platform_account_id"`
	PlatformCandidateID string `json:"platform_candidate_id"`
	Gender              string `json:"gender"`
	BirthYMPrecision    string `json:"birth_ym_precision"`
	BasicInfo           string `json:"basic_info"`
}

// AutoReplyCandidateSaveResult 表示正式简历和平台身份保存结果。
type AutoReplyCandidateSaveResult struct {
	OK               bool                      `json:"ok"`
	CandidateID      string                    `json:"candidate_id"`
	PlatformIdentity CandidatePlatformIdentity `json:"platform_identity"`
	Position         AutoReplyPosition         `json:"position"`
}

// AutoReplyAttachmentUpload 表示本地下载完成、准备上传云端的简历附件。
type AutoReplyAttachmentUpload struct {
	FilePath        string
	CandidateID     string
	ConversationID  string
	SourceMessageID string
	PlatformID      string
	ExtractedText   string
}
