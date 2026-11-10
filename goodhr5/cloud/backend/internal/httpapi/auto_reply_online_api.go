// 本文件只接受服务器自动回复提交的同会话在线资料；正式入库仍受岗位开关约束。
package httpapi

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// autoReplyOnlineProfileRequest 只绑定已核实岗位、原生来源和当前服务器会话，不接受调用方指定简历库编号。
type autoReplyOnlineProfileRequest struct {
	PositionID          string                 `json:"position_id"`
	ConversationID      string                 `json:"conversation_id"`
	PlatformID          string                 `json:"platform_id"`
	PlatformCandidateID string                 `json:"platform_candidate_id"`
	PlatformAccountID   string                 `json:"platform_account_id"`
	Profile             OnlineCandidateProfile `json:"profile"`
}

// onlineProfileStoreInput 保存服务端核实的当前成员、设备、岗位权限，HTTP正文不能设置这些授权字段。
type onlineProfileStoreInput struct {
	Scope             CandidateLibraryScope
	UserID, MachineID string
	Structured        bool
	Request           autoReplyOnlineProfileRequest
	cachedProof       bool   // 仅服务端核实同actor/current代缓存后可复用旧设备的真实读取收据。
	cachedCandidateID string // 缓存已有canonical时不得因换设备改绑为另一份资料。
}

// autoReplyOnlineProfileResult 区分简历库编号与自动回复缓存；关闭正式入库时编号明确为空。
type autoReplyOnlineProfileResult struct {
	CandidateID string                   `json:"candidate_id"`
	MatchStatus string                   `json:"match_status"`
	ProfileKey  string                   `json:"profile_key"`
	Candidate   autoReplyOnlineCandidate `json:"candidate"`
}

// autoReplyOnlineCandidate 固定在线资料返回协议，与客户端StructuredCandidate逐字段对应，不暴露动态库扩展字段。
type autoReplyOnlineCandidate struct {
	CandidateName       string                       `json:"candidate_name,omitempty"`
	BirthYM             string                       `json:"birth_ym,omitempty"`
	Phone               string                       `json:"phone,omitempty"`
	Email               string                       `json:"email,omitempty"`
	Wechat              string                       `json:"wechat,omitempty"`
	WorkRegion          string                       `json:"work_region,omitempty"`
	WorkYears           string                       `json:"work_years,omitempty"`
	ExpectedSalaryMin   *float64                     `json:"expected_salary_min,omitempty"`
	ExpectedSalaryMax   *float64                     `json:"expected_salary_max,omitempty"`
	EducationLevel      string                       `json:"education_level,omitempty"`
	ExpectedPosition    string                       `json:"expected_position,omitempty"`
	OnlineStatus        string                       `json:"online_status,omitempty"`
	PersonalDescription string                       `json:"personal_description,omitempty"`
	WorkStatus          string                       `json:"work_status,omitempty"`
	RawText             string                       `json:"raw_text,omitempty"`
	WorkExperiences     []CandidateWorkExperience    `json:"work_experiences,omitempty"`
	Educations          []CandidateEducation         `json:"educations,omitempty"`
	Certificates        []CandidateCertificate       `json:"certificates,omitempty"`
	Honors              []CandidateHonor             `json:"honors,omitempty"`
	ProjectExperiences  []CandidateProjectExperience `json:"project_experiences,omitempty"`
	Communications      []CandidateCommunication     `json:"colleague_communications,omitempty"`
}

// onlineCandidateDTO 将服务器主体显式转换为固定响应，保留正式资料的已知字段。
func onlineCandidateDTO(p PositionCandidate) autoReplyOnlineCandidate {
	return autoReplyOnlineCandidate{CandidateName: p.CandidateName, BirthYM: p.BirthYM, Phone: p.Phone, Email: p.Email, Wechat: candidateString(p.LibraryFields, "wechat"), WorkRegion: p.WorkRegion, WorkYears: p.WorkYears, ExpectedSalaryMin: p.ExpectedSalaryMin, ExpectedSalaryMax: p.ExpectedSalaryMax, EducationLevel: p.EducationLevel, ExpectedPosition: p.ExpectedPosition, OnlineStatus: p.OnlineStatus, PersonalDescription: p.PersonalDescription, WorkStatus: p.WorkStatus, RawText: p.RawText, WorkExperiences: p.WorkExperiences, Educations: p.Educations, Certificates: p.Certificates, Honors: p.Honors, ProjectExperiences: p.ProjectExperiences, Communications: p.Communications}
}

// agentOnlineProfile 复用当前登录、设备和岗位校验，仅持服务器运行凭证的流程可以登记已读取的在线资料。
func (s *AutoReplyService) agentOnlineProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "在线资料这里只支持保存")
		return
	}
	secret := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"))
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(r.Header.Get("X-GoodHR-Runtime-Service"))) != 1 {
		writeAutoReplyError(w, 403, "RUNTIME_AUTH_REQUIRED", "在线资料只能由服务器自动回复流程保存")
		return
	}
	var in autoReplyOnlineProfileRequest
	if err := decodeAutoReplyJSON(w, r, &in); err != nil {
		return
	}
	a, position, ok := s.positionRequestContext(w, r, in.PositionID, true, true)
	if !ok {
		return
	}
	if in.PlatformID != position.PlatformID || strings.TrimSpace(in.PlatformCandidateID) == "" || strings.TrimSpace(in.PlatformAccountID) == "" || in.ConversationID == "" || in.Profile.SourceKey == "" || strings.TrimSpace(in.Profile.RawText) == "" || strings.TrimSpace(in.Profile.CandidateName) == "" {
		writeAutoReplyError(w, 400, "ONLINE_PROFILE_SOURCE_REQUIRED", "在线资料与本轮会话还没核对上，请稍后再试")
		return
	}
	userID, err := s.store.activeTenantUserID(r.Context(), a.Tenant.ID, a.Session.Email)
	if err != nil {
		writeAutoReplyStoreError(w, err, "成员信息暂时没读出来")
		return
	}
	store, ok := s.candidates.(*PostgresCandidateStore)
	if !ok {
		writeAutoReplyError(w, 503, "ONLINE_PROFILE_STORAGE_UNAVAILABLE", "在线资料的服务器存储还没准备好")
		return
	}
	result, err := store.saveOnlineProfile(r.Context(), onlineProfileStoreInput{
		Scope:  CandidateLibraryScope{TenantID: a.Tenant.ID, UserEmail: a.Session.Email, IsAdmin: a.IsAdmin},
		UserID: userID, MachineID: strings.TrimSpace(r.Header.Get("X-GoodHR-Machine-ID")), Structured: position.CommonConfig["output_structured_resume"] == true, Request: in,
	})
	if err != nil {
		writeAutoReplyStoreError(w, err, "在线资料还没存好，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
