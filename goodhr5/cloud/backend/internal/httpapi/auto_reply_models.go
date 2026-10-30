// 本文件定义自动回复配置与本地生成报告的云端保存合同，不包含聊天和原始附件。
package httpapi

import "time"

const (
	// AutoReplyDefaultResumeRequestMessage 是岗位没有自定义时使用的索要简历话术。
	AutoReplyDefaultResumeRequestMessage = "你好，能发一份简历吗？"
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
	ExpectedSalaryMin   *float64                     `json:"expected_salary_min"`
	ExpectedSalaryMax   *float64                     `json:"expected_salary_max"`
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

// CandidateAutoReplyReport 表示一份经用户开启后才可公开分享的结构化推荐记录。
type CandidateAutoReplyReport struct {
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
