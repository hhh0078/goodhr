// 本文件负责向本地自动回复提供岗位配置，并接收明确授权的结构化简历与结果摘要。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

// autoReplyCandidateRequest 表示本地清洗完成、准备进入正式简历库的候选人资料。
type autoReplyCandidateRequest struct {
	PositionID          string                       `json:"position_id"`
	PlatformID          string                       `json:"platform_id"`
	PlatformAccountID   string                       `json:"platform_account_id"`
	PlatformCandidateID string                       `json:"platform_candidate_id"`
	AvatarDataURL       string                       `json:"avatar_data_url"`
	AvatarURL           string                       `json:"avatar_url"`
	CandidateName       string                       `json:"candidate_name"`
	Gender              string                       `json:"gender"`
	BirthYM             string                       `json:"birth_ym"`
	BirthYMPrecision    string                       `json:"birth_ym_precision"`
	Phone               string                       `json:"phone"`
	Email               string                       `json:"email"`
	Wechat              string                       `json:"wechat"`
	WorkRegion          string                       `json:"work_region"`
	WorkYears           string                       `json:"work_years"`
	ExpectedSalaryMin   *float64                     `json:"expected_salary_min"`
	ExpectedSalaryMax   *float64                     `json:"expected_salary_max"`
	BasicInfo           string                       `json:"basic_info"`
	EducationLevel      string                       `json:"education_level"`
	ExpectedPosition    string                       `json:"expected_position"`
	OnlineStatus        string                       `json:"online_status"`
	PersonalDescription string                       `json:"personal_description"`
	WorkStatus          string                       `json:"work_status"`
	RawText             string                       `json:"raw_text"`
	WorkExperiences     []CandidateWorkExperience    `json:"work_experiences"`
	Educations          []CandidateEducation         `json:"educations"`
	Certificates        []CandidateCertificate       `json:"certificates"`
	Honors              []CandidateHonor             `json:"honors"`
	ProjectExperiences  []CandidateProjectExperience `json:"project_experiences"`
	Communications      []CandidateCommunication     `json:"colleague_communications"`
}

// Agent 只开放配置、授权结构化结果和简短通知，聊天、附件及AI执行均留在本机。
func (s *AutoReplyService) Agent(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/agent/"), "/")
	switch {
	case strings.HasPrefix(path, "positions/") && strings.HasSuffix(path, "/snapshot"):
		s.agentPositionSnapshot(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "positions/"), "/snapshot"))
	case path == "positions":
		s.agentPositionSnapshots(w, r)
	case path == "candidates":
		s.agentSaveCandidate(w, r)
	case path == "notifications":
		s.agentSendNotification(w, r)
	case path == "recommendations/completed":
		s.agentSaveRecommendation(w, r)
	default:
		writeAutoReplyError(w, http.StatusNotFound, "LOCAL_DATA_ONLY", "这份资料保存在本机，请在本地程序中查看")
	}
}

// agentPositionSnapshots 返回当前账号在指定平台已开启自动回复的全部岗位快照。
func (s *AutoReplyService) agentPositionSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "岗位快照列表这里只支持读取")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	platformID := strings.TrimSpace(r.URL.Query().Get("platform_id"))
	if platformID == "" {
		writeAutoReplyError(w, http.StatusBadRequest, "PLATFORM_REQUIRED", "读取自动回复岗位需要招聘平台")
		return
	}
	positions, err := s.positions.ListPositions(requestContext.Tenant.ID, requestContext.Session.Email, false)
	if err != nil {
		writeAutoReplyInternalError(w, "POSITION_LIST_FAILED", "自动回复岗位暂时没读出来", err)
		return
	}
	items := make([]map[string]any, 0)
	for _, position := range positions {
		if !strings.EqualFold(strings.TrimSpace(position.PlatformID), platformID) {
			continue
		}
		config, configErr := s.store.GetPositionAutoReplyConfig(r.Context(), requestContext.Tenant.ID, position.ID)
		if errors.Is(configErr, ErrNotFound) || (configErr == nil && !config.Enabled) {
			continue
		}
		if configErr != nil {
			writeAutoReplyInternalError(w, "AUTO_REPLY_CONFIG_LOAD_FAILED", "自动回复岗位配置暂时没读出来", configErr)
			return
		}
		company, companyErr := s.store.GetCompanyProfile(r.Context(), requestContext.Tenant.ID, config.CompanyProfileID)
		if companyErr != nil {
			writeAutoReplyStoreError(w, companyErr, "岗位公司档案暂时没读出来")
			return
		}
		items = append(items, map[string]any{
			"ok": true, "position": publicAutoReplyPosition(position), "config": config,
			"company_profile": company, "subscription": publicSubscriptionAccess(requestContext.Access),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "positions": items})
}

// agentPositionSnapshot 校验岗位权限并返回自动回复配置快照。
func (s *AutoReplyService) agentPositionSnapshot(w http.ResponseWriter, r *http.Request, positionID string) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "岗位快照这里只支持读取")
		return
	}
	requestContext, position, ok := s.positionRequestContext(w, r, positionID, true, true)
	if !ok {
		return
	}
	config, err := s.store.GetPositionAutoReplyConfig(r.Context(), requestContext.Tenant.ID, position.ID)
	if errors.Is(err, ErrNotFound) || (err == nil && !config.Enabled) {
		writeAutoReplyError(w, http.StatusConflict, "AUTO_REPLY_NOT_ENABLED", "这个岗位还没有开启自动回复")
		return
	}
	if err != nil {
		writeAutoReplyInternalError(w, "AUTO_REPLY_CONFIG_LOAD_FAILED", "岗位自动回复配置暂时没读出来", err)
		return
	}
	company, err := s.store.GetCompanyProfile(r.Context(), requestContext.Tenant.ID, config.CompanyProfileID)
	if err != nil {
		writeAutoReplyStoreError(w, err, "岗位公司档案暂时没读出来")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "position": publicAutoReplyPosition(position), "config": config,
		"company_profile": company, "subscription": publicSubscriptionAccess(requestContext.Access),
	})
}
