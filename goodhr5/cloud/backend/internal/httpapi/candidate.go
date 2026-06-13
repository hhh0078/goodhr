// 本文件负责定义云端候选人和简历库共享的数据结构。
package httpapi

import "strings"

// CandidateSalary 表示候选人期望薪资区间。
type CandidateSalary struct {
	Min *int `json:"min"`
	Max *int `json:"max"`
}

// CandidateWorkExperience 表示工作经历。
type CandidateWorkExperience struct {
	CompanyName  string `json:"company_name"`
	PositionName string `json:"position_name"`
	Content      string `json:"content"`
	StartYM      string `json:"start_ym"`
	EndYM        string `json:"end_ym"`
}

// CandidateEducation 表示教育经历。
type CandidateEducation struct {
	SchoolName       string `json:"school_name"`
	MajorName        string `json:"major_name"`
	EducationLevel   string `json:"education_level"`
	CampusExperience string `json:"campus_experience"`
	StartYM          string `json:"start_ym"`
	EndYM            string `json:"end_ym"`
}

// CandidateProjectExperience 表示项目经验。
type CandidateProjectExperience struct {
	CompanyName  string `json:"company_name"`
	PositionName string `json:"position_name"`
	Content      string `json:"content"`
	StartYM      string `json:"start_ym"`
	EndYM        string `json:"end_ym"`
}

// CandidateCommunication 表示沟通记录。
type CandidateCommunication struct {
	Content string `json:"content"`
	Time    string `json:"time"`
}

// CandidateBasicProfile 表示在线简历基础信息。
type CandidateBasicProfile struct {
	BirthYM                 string                       `json:"birth_ym"`
	Phone                   string                       `json:"phone"`
	Email                   string                       `json:"email"`
	WorkRegion              string                       `json:"work_region"`
	WorkYears               string                       `json:"work_years"`
	ExpectedSalary          CandidateSalary              `json:"expected_salary"`
	PersonalDescription     string                       `json:"personal_description"`
	WorkStatus              string                       `json:"work_status"`
	ExpectedPosition        string                       `json:"expected_position"`
	OnlineStatus            string                       `json:"online_status"`
	WorkExperiences         []CandidateWorkExperience    `json:"work_experiences"`
	Educations              []CandidateEducation         `json:"educations"`
	Certificates            []string                     `json:"certificates"`
	Honors                  []string                     `json:"honors"`
	ProjectExperiences      []CandidateProjectExperience `json:"project_experiences"`
	ColleagueCommunications []CandidateCommunication     `json:"colleague_communications"`
}

// CandidateResumeAttachment 表示简历附件信息。
type CandidateResumeAttachment struct {
	URL           string `json:"url"`
	ExtractedText string `json:"extracted_text"`
}

// CandidateAIScore 表示 AI 某阶段评分。
type CandidateAIScore struct {
	Reason string   `json:"reason"`
	Score  *float64 `json:"score"`
}

// CandidateAIScores 表示 AI 三阶段评分。
type CandidateAIScores struct {
	Detail CandidateAIScore `json:"detail"`
	Greet  CandidateAIScore `json:"greet"`
	Review CandidateAIScore `json:"review"`
}

// CandidateDetail 表示候选人详情抓取信息。
type CandidateDetail struct {
	Opened          bool           `json:"opened"`
	OpenReason      string         `json:"open_reason"`
	Text            string         `json:"text"`
	Fields          map[string]any `json:"fields"`
	ScreenshotPaths []string       `json:"screenshot_paths"`
	OCRPath         string         `json:"ocr_path"`
	TokenUsage      int            `json:"token_usage"`
}

// CandidateRuntime 表示流程运行态信息。
type CandidateRuntime struct {
	ElementRef  string `json:"element_ref"`
	CardIndex   int    `json:"card_index"`
	Fingerprint string `json:"fingerprint"`
}

// CandidateTimestamps 表示候选人时间字段。
type CandidateTimestamps struct {
	FirstSeenAt     string `json:"first_seen_at"`
	DetailFetchedAt string `json:"detail_fetched_at"`
	UpdatedAt       string `json:"updated_at"`
	GreetedAt       string `json:"greeted_at"`
}

// Candidate 表示本地程序同步到云端的候选人统一对象。
type Candidate struct {
	ID                  string                    `json:"id"`
	PlatformID          string                    `json:"platform_id"`
	PlatformCandidateID string                    `json:"platform_candidate_id"`
	Name                string                    `json:"name"`
	BasicInfo           string                    `json:"basic_info"`
	EducationLevel      string                    `json:"education"`
	PersonalDescription string                    `json:"description"`
	BasicProfile        CandidateBasicProfile     `json:"basic_profile"`
	ResumeAttachment    CandidateResumeAttachment `json:"resume_attachment"`
	RawText             string                    `json:"raw_text"`
	FilterText          string                    `json:"filter_text"`
	Detail              CandidateDetail           `json:"detail"`
	AI                  CandidateAIScores         `json:"ai"`
	Runtime             CandidateRuntime          `json:"runtime"`
	Timestamps          CandidateTimestamps       `json:"timestamps"`
	Ext                 map[string]any            `json:"ext"`
}

// DisplayName 返回候选人可读名称，优先姓名，缺失时回退占位文案。
func (c Candidate) DisplayName() string {
	name := strings.TrimSpace(c.Name)
	if name != "" {
		return name
	}
	return "未知候选人"
}
