// 本文件负责定义云端自动回复配置、会话、简历、确认项和 AI 审计的强类型数据模型。
package httpapi

import (
	"encoding/json"
	"time"
)

const (
	// AutoReplyDefaultResumeRequestMessage 是岗位没有自定义时使用的索要简历话术。
	AutoReplyDefaultResumeRequestMessage = "你好，能发一份简历吗？"
	// AutoReplyMaxAttachmentBytes 是单个简历附件允许的最大字节数。
	AutoReplyMaxAttachmentBytes int64 = 20 * 1024 * 1024
)

// CompanyProfile 表示团队成员共享的一份公司档案。
type CompanyProfile struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	Contact         string    `json:"contact"`
	Overview        string    `json:"overview"`
	ExtraInfo       string    `json:"extra_info"`
	CreatedByUserID string    `json:"created_by_user_id"`
	UpdatedByUserID string    `json:"updated_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PositionReplyCondition 表示一条岗位自动回复条件。
type PositionReplyCondition struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	PositionID      string    `json:"position_id"`
	Type            string    `json:"type"`
	Content         string    `json:"content"`
	DedupeKey       string    `json:"dedupe_key"`
	SortOrder       int       `json:"sort_order"`
	Enabled         bool      `json:"enabled"`
	CreatedByUserID string    `json:"created_by_user_id"`
	UpdatedByUserID string    `json:"updated_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PositionAutoReplyConfig 表示岗位自动回复开关和配置快照。
type PositionAutoReplyConfig struct {
	PositionID              string                   `json:"position_id"`
	TenantID                string                   `json:"tenant_id"`
	CompanyProfileID        string                   `json:"company_profile_id"`
	Enabled                 bool                     `json:"enabled"`
	PositionDescription     string                   `json:"position_description"`
	ResumeRequestMessage    string                   `json:"resume_request_message"`
	PollIntervalSeconds     int                      `json:"poll_interval_seconds"`
	MaxThreadsPerCheckpoint int                      `json:"max_threads_per_checkpoint"`
	Version                 int                      `json:"version"`
	UpdatedByUserID         string                   `json:"updated_by_user_id"`
	Conditions              []PositionReplyCondition `json:"conditions"`
	CreatedAt               time.Time                `json:"created_at"`
	UpdatedAt               time.Time                `json:"updated_at"`
}

// CandidatePlatformIdentity 表示正式入库前后都可使用的平台候选人身份。
type CandidatePlatformIdentity struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenant_id"`
	CandidateID         string    `json:"candidate_id"`
	PlatformID          string    `json:"platform_id"`
	PlatformAccountID   string    `json:"platform_account_id"`
	PlatformCandidateID string    `json:"platform_candidate_id"`
	CandidateName       string    `json:"candidate_name"`
	Gender              string    `json:"gender"`
	NormalizedPhone     string    `json:"normalized_phone"`
	FirstSeenAt         time.Time `json:"first_seen_at"`
	LastSeenAt          time.Time `json:"last_seen_at"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AutoReplyConversation 表示一段可在候选人正式入库前存在的聊天会话。
type AutoReplyConversation struct {
	ID                      string     `json:"id"`
	TenantID                string     `json:"tenant_id"`
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

// AutoReplyMessage 表示一条标准化聊天消息。
type AutoReplyMessage struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
	ConversationID    string          `json:"conversation_id"`
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

// MessageSyncResult 表示聊天消息差量同步结果。
type MessageSyncResult struct {
	Inserted                int    `json:"inserted"`
	LastSyncedMessageKey    string `json:"last_synced_message_key"`
	LastCandidateMessageKey string `json:"last_candidate_message_key"`
}

// StoredResumeAttachment 表示云端持久化目录中的简历附件元数据。
type StoredResumeAttachment struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	CandidateID     string    `json:"candidate_id"`
	ConversationID  string    `json:"conversation_id"`
	SourceMessageID string    `json:"source_message_id"`
	PlatformID      string    `json:"platform_id"`
	OriginalName    string    `json:"original_name"`
	StoragePath     string    `json:"storage_path"`
	SHA256          string    `json:"sha256"`
	MIMEType        string    `json:"mime_type"`
	SizeBytes       int64     `json:"size_bytes"`
	ExtractedText   string    `json:"extracted_text"`
	CreatedByUserID string    `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// CandidateConfirmationItem 表示候选人和岗位之间的一条可审计确认项。
type CandidateConfirmationItem struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
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

// AutoReplyAIRun 表示自动回复的一次 AI 总审计记录。
type AutoReplyAIRun struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenant_id"`
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

// AutoReplyToolCall 表示一次 AI 工具调用审计记录。
type AutoReplyToolCall struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
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

// AutoReplyConfigSuggestion 表示 AI 提交、等待 HR 审核的配置修改建议。
type AutoReplyConfigSuggestion struct {
	ID               string          `json:"id"`
	TenantID         string          `json:"tenant_id"`
	ConversationID   string          `json:"conversation_id"`
	PositionID       string          `json:"position_id"`
	CompanyProfileID string          `json:"company_profile_id"`
	SuggestionType   string          `json:"suggestion_type"`
	Operation        string          `json:"operation"`
	TargetID         string          `json:"target_id"`
	ProposedValue    json.RawMessage `json:"proposed_value"`
	Reason           string          `json:"reason"`
	Status           string          `json:"status"`
	ReviewedByUserID string          `json:"reviewed_by_user_id"`
	ReviewedAt       *time.Time      `json:"reviewed_at"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// AutoReplyNotification 表示一次需要 HR 人工接管的幂等邮件通知。
type AutoReplyNotification struct {
	ID                string     `json:"id"`
	TenantID          string     `json:"tenant_id"`
	ConversationID    string     `json:"conversation_id"`
	PositionID        string     `json:"position_id"`
	BasedOnMessageKey string     `json:"based_on_message_key"`
	ReasonKey         string     `json:"reason_key"`
	CandidateName     string     `json:"candidate_name"`
	Gender            string     `json:"gender"`
	PlatformID        string     `json:"platform_id"`
	Reason            string     `json:"reason"`
	RecipientEmail    string     `json:"recipient_email"`
	Status            string     `json:"status"`
	ErrorMessage      string     `json:"error_message"`
	SentAt            *time.Time `json:"sent_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
