// Package cloud 本文件定义本地生成的推荐报告合同，不包含原始附件、聊天全文或AI请求记录。
package cloud

import "time"

// AutoReplyReportPoint 表示推荐报告中的一条优势、风险或面试关注点。
type AutoReplyReportPoint struct {
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Severity string   `json:"severity,omitempty"`
	Evidence []string `json:"evidence"`
}

// AutoReplyReportCandidate 表示推荐报告冻结的完整结构化简历。
type AutoReplyReportCandidate struct {
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

// AutoReplyReportPosition 表示推荐报告冻结的岗位和公司资料。
type AutoReplyReportPosition struct {
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

// AutoReplyReportCondition 表示报告生成时一条条件的状态和依据。
type AutoReplyReportCondition struct {
	ID           string   `json:"id"`
	ItemType     string   `json:"item_type"`
	Content      string   `json:"content"`
	Status       string   `json:"status"`
	StatusReason string   `json:"status_reason"`
	Evidence     []string `json:"evidence"`
}

// AutoReplyRecommendationReport 表示公开页面和邮件共同使用的推荐报告快照。
type AutoReplyRecommendationReport struct {
	Candidate            AutoReplyReportCandidate   `json:"candidate"`
	Position             AutoReplyReportPosition    `json:"position"`
	MatchScore           float64                    `json:"match_score"`
	RecommendationLevel  string                     `json:"recommendation_level"`
	ExecutiveSummary     string                     `json:"executive_summary"`
	Strengths            []AutoReplyReportPoint     `json:"strengths"`
	Risks                []AutoReplyReportPoint     `json:"risks"`
	Conditions           []AutoReplyReportCondition `json:"conditions"`
	BonusItems           []AutoReplyReportPoint     `json:"bonus_items"`
	UnconfirmedItems     []AutoReplyReportPoint     `json:"unconfirmed_items"`
	JobIntentSummary     string                     `json:"job_intent_summary"`
	StabilitySummary     string                     `json:"stability_summary"`
	CommunicationSummary string                     `json:"communication_summary"`
	InterviewFocus       []AutoReplyReportPoint     `json:"interview_focus"`
	SuggestedQuestions   []string                   `json:"suggested_questions"`
	SourceCutoffAt       time.Time                  `json:"source_cutoff_at"`
	GeneratedAt          time.Time                  `json:"generated_at"`
}
