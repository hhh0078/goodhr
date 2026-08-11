// Package httpapi 本文件负责定义云端自动回复配置、会话、简历、确认项和 AI 审计的强类型数据模型。
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
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	ReviewID            string     `json:"review_id"`
	ConversationID      string     `json:"conversation_id"`
	CandidateID         string     `json:"candidate_id"`
	PositionID          string     `json:"position_id"`
	PositionConditionID string     `json:"position_condition_id"`
	ItemType            string     `json:"item_type"`
	Content             string     `json:"content"`
	DedupeKey           string     `json:"dedupe_key"`
	Status              string     `json:"status"`
	StatusReason        string     `json:"status_reason"`
	SourceType          string     `json:"source_type"`
	SourceRef           string     `json:"source_ref"`
	EvidenceText        string     `json:"evidence_text"`
	Summary             string     `json:"summary"`
	CreatedByKind       string     `json:"created_by_kind"`
	AskCount            int        `json:"ask_count"`
	LastAskedAt         *time.Time `json:"last_asked_at"`
	LastAnsweredAt      *time.Time `json:"last_answered_at"`
	LastReviewedAt      *time.Time `json:"last_reviewed_at"`
	ArchivedAt          *time.Time `json:"archived_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// CandidatePositionReview 表示候选人与岗位之间跨会话复用的长期评估主体。
type CandidatePositionReview struct {
	ID                      string     `json:"id"`
	TenantID                string     `json:"tenant_id"`
	CandidateID             string     `json:"candidate_id"`
	PositionID              string     `json:"position_id"`
	Status                  string     `json:"status"`
	ConditionsInitializedAt *time.Time `json:"conditions_initialized_at"`
	QualifiedAt             *time.Time `json:"qualified_at"`
	CurrentRecommendationID string     `json:"current_recommendation_id"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// RecommendationPoint 表示推荐报告中的一条优势、风险或面试关注点。
type RecommendationPoint struct {
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Severity string   `json:"severity,omitempty"`
	Evidence []string `json:"evidence"`
}

// RecommendationCandidateSnapshot 表示推荐报告冻结的完整结构化简历。
type RecommendationCandidateSnapshot struct {
	ID                  string                       `json:"id"`
	Name                string                       `json:"name"`
	AvatarURL           string                       `json:"avatar_url"`
	Gender              string                       `json:"gender"`
	BirthYM             string                       `json:"birth_ym"`
	Phone               string                       `json:"phone"`
	Email               string                       `json:"email"`
	Wechat              string                       `json:"wechat"`
	WorkRegion          string                       `json:"work_region"`
	WorkYears           string                       `json:"work_years"`
	EducationLevel      string                       `json:"education_level"`
	ExpectedPosition    string                       `json:"expected_position"`
	ExpectedSalaryMin   *int                         `json:"expected_salary_min"`
	ExpectedSalaryMax   *int                         `json:"expected_salary_max"`
	WorkStatus          string                       `json:"work_status"`
	OnlineStatus        string                       `json:"online_status"`
	PersonalDescription string                       `json:"personal_description"`
	BasicInfo           string                       `json:"basic_info"`
	WorkExperiences     []CandidateWorkExperience    `json:"work_experiences"`
	Educations          []CandidateEducation         `json:"educations"`
	Certificates        []CandidateCertificate       `json:"certificates"`
	Honors              []CandidateHonor             `json:"honors"`
	ProjectExperiences  []CandidateProjectExperience `json:"project_experiences"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

// RecommendationPositionSnapshot 表示推荐报告冻结的岗位和公司资料。
type RecommendationPositionSnapshot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PlatformID  string `json:"platform_id"`
	Description string `json:"description"`
	CompanyName string `json:"company_name"`
	Address     string `json:"address"`
	Contact     string `json:"contact"`
	Overview    string `json:"overview"`
	ExtraInfo   string `json:"extra_info"`
}

// RecommendationConditionSnapshot 表示报告生成时一条条件的状态和依据。
type RecommendationConditionSnapshot struct {
	ID           string   `json:"id"`
	ItemType     string   `json:"item_type"`
	Content      string   `json:"content"`
	Status       string   `json:"status"`
	StatusReason string   `json:"status_reason"`
	Evidence     []string `json:"evidence"`
}

// RecommendationMessageSnapshot 表示公开沟通记录中的一条候选人或HR消息。
type RecommendationMessageSnapshot struct {
	ID             string     `json:"id"`
	Direction      string     `json:"direction"`
	MessageType    string     `json:"message_type"`
	TextContent    string     `json:"text_content"`
	SenderName     string     `json:"sender_name"`
	PlatformSentAt *time.Time `json:"platform_sent_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// RecommendationConversationSnapshot 表示推荐报告数据截止时间内的一段真实沟通记录。
type RecommendationConversationSnapshot struct {
	ID            string                          `json:"id"`
	PlatformID    string                          `json:"platform_id"`
	PositionText  string                          `json:"position_text"`
	CandidateName string                          `json:"candidate_name"`
	Messages      []RecommendationMessageSnapshot `json:"messages"`
	CreatedAt     time.Time                       `json:"created_at"`
	UpdatedAt     time.Time                       `json:"updated_at"`
}

// CandidateRecommendationReport 表示公开页面和邮件共同使用的推荐报告快照。
type CandidateRecommendationReport struct {
	Candidate            RecommendationCandidateSnapshot   `json:"candidate"`
	Position             RecommendationPositionSnapshot    `json:"position"`
	MatchScore           float64                           `json:"match_score"`
	RecommendationLevel  string                            `json:"recommendation_level"`
	ExecutiveSummary     string                            `json:"executive_summary"`
	Strengths            []RecommendationPoint             `json:"strengths"`
	Risks                []RecommendationPoint             `json:"risks"`
	Conditions           []RecommendationConditionSnapshot `json:"conditions"`
	BonusItems           []RecommendationPoint             `json:"bonus_items"`
	UnconfirmedItems     []RecommendationPoint             `json:"unconfirmed_items"`
	JobIntentSummary     string                            `json:"job_intent_summary"`
	StabilitySummary     string                            `json:"stability_summary"`
	CommunicationSummary string                            `json:"communication_summary"`
	InterviewFocus       []RecommendationPoint             `json:"interview_focus"`
	SuggestedQuestions   []string                          `json:"suggested_questions"`
	SourceCutoffAt       time.Time                         `json:"source_cutoff_at"`
	GeneratedAt          time.Time                         `json:"generated_at"`
}

// CandidateRecommendation 表示一份可公开分享、可撤销的不可变推荐记录。
type CandidateRecommendation struct {
	ID                  string                        `json:"id"`
	PublicID            string                        `json:"public_id"`
	TenantID            string                        `json:"tenant_id,omitempty"`
	ReviewID            string                        `json:"review_id"`
	CandidateID         string                        `json:"candidate_id"`
	PositionID          string                        `json:"position_id"`
	Version             int                           `json:"version"`
	InputHash           string                        `json:"-"`
	Status              string                        `json:"status"`
	MatchScore          float64                       `json:"match_score"`
	RecommendationLevel string                        `json:"recommendation_level"`
	Summary             string                        `json:"summary"`
	Report              CandidateRecommendationReport `json:"report"`
	Model               string                        `json:"model,omitempty"`
	TokenUsage          int                           `json:"token_usage,omitempty"`
	ShareEnabled        bool                          `json:"share_enabled"`
	NotificationStatus  string                        `json:"notification_status,omitempty"`
	NotificationError   string                        `json:"notification_error,omitempty"`
	NotifiedAt          *time.Time                    `json:"notified_at,omitempty"`
	SourceCutoffAt      time.Time                     `json:"source_cutoff_at"`
	GeneratedAt         time.Time                     `json:"generated_at"`
	CreatedAt           time.Time                     `json:"created_at"`
	UpdatedAt           time.Time                     `json:"updated_at"`
}

// CandidateRecommendationJob 表示数据库中可重试的推荐报告生成任务。
type CandidateRecommendationJob struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	ReviewID      string    `json:"review_id"`
	CandidateID   string    `json:"candidate_id"`
	PositionID    string    `json:"position_id"`
	InputHash     string    `json:"input_hash"`
	Status        string    `json:"status"`
	AttemptCount  int       `json:"attempt_count"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	LastError     string    `json:"last_error"`
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
	LatestMessage     string     `json:"latest_message"`
	RecipientEmail    string     `json:"recipient_email"`
	Status            string     `json:"status"`
	ErrorMessage      string     `json:"error_message"`
	SentAt            *time.Time `json:"sent_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// AutoReplyAuditRecord 表示前端运行小窗和审计页使用的一条 AI 总记录。
type AutoReplyAuditRecord struct {
	Run           AutoReplyAIRun      `json:"run"`
	CandidateName string              `json:"candidate_name"`
	Gender        string              `json:"gender"`
	PlatformID    string              `json:"platform_id"`
	PositionName  string              `json:"position_name"`
	ToolCalls     []AutoReplyToolCall `json:"tool_calls"`
}
