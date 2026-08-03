// Package httpapi 本文件负责本地 Agent 的自动回复岗位快照、候选人、会话、消息和确认项接口。
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// autoReplyCandidateRequest 表示本地清洗完成、准备进入正式简历库的候选人资料。
type autoReplyCandidateRequest struct {
	PositionID          string                       `json:"position_id"`
	PlatformID          string                       `json:"platform_id"`
	PlatformAccountID   string                       `json:"platform_account_id"`
	PlatformCandidateID string                       `json:"platform_candidate_id"`
	CandidateName       string                       `json:"candidate_name"`
	Gender              string                       `json:"gender"`
	BirthYM             string                       `json:"birth_ym"`
	BirthYMPrecision    string                       `json:"birth_ym_precision"`
	Phone               string                       `json:"phone"`
	Email               string                       `json:"email"`
	WorkRegion          string                       `json:"work_region"`
	WorkYears           string                       `json:"work_years"`
	ExpectedSalaryMin   *int                         `json:"expected_salary_min"`
	ExpectedSalaryMax   *int                         `json:"expected_salary_max"`
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

// autoReplyCandidateStateResponse 表示本地程序按平台身份或手机号查询到的已有简历状态。
type autoReplyCandidateStateResponse struct {
	OK                  bool                       `json:"ok"`
	Found               bool                       `json:"found"`
	HasResumeAttachment bool                       `json:"has_resume_attachment"`
	Candidate           *PositionCandidate         `json:"candidate,omitempty"`
	Identity            *CandidatePlatformIdentity `json:"identity,omitempty"`
	Conversation        *AutoReplyConversation     `json:"conversation,omitempty"`
	Attachments         []StoredResumeAttachment   `json:"attachments"`
}

// Agent 按子路径分发本地程序的自动回复数据请求。
func (s *AutoReplyService) Agent(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/agent/"), "/")
	switch {
	case strings.HasPrefix(path, "positions/") && strings.HasSuffix(path, "/snapshot"):
		s.agentPositionSnapshot(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "positions/"), "/snapshot"))
	case path == "positions":
		s.agentPositionSnapshots(w, r)
	case path == "candidates":
		s.agentSaveCandidate(w, r)
	case path == "candidate-state":
		s.agentCandidateState(w, r)
	case path == "identities":
		s.agentSaveIdentity(w, r)
	case path == "conversations":
		s.agentSaveConversation(w, r)
	case path == "messages/sync":
		s.agentSyncMessages(w, r)
	case path == "messages":
		s.agentListMessages(w, r)
	case path == "confirmations":
		s.agentConfirmations(w, r)
	case path == "attachments":
		s.agentAttachments(w, r)
	case path == "ai-runs/start":
		s.agentStartAIRun(w, r)
	case path == "ai-runs/finish":
		s.agentFinishAIRun(w, r)
	case path == "tool-calls":
		s.agentSaveToolCall(w, r)
	case path == "suggestions":
		s.agentSaveSuggestion(w, r)
	case path == "notifications":
		s.agentSendNotification(w, r)
	default:
		writeAutoReplyError(w, http.StatusNotFound, "AUTO_REPLY_ROUTE_NOT_FOUND", "这个本地自动回复地址没认出来")
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

// agentCandidateState 按平台候选人ID优先、手机号后备读取正式简历、会话和附件状态。
func (s *AutoReplyService) agentCandidateState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "候选人资料状态这里只支持读取")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	platformID := strings.TrimSpace(r.URL.Query().Get("platform_id"))
	accountID := strings.TrimSpace(r.URL.Query().Get("platform_account_id"))
	platformCandidateID := strings.TrimSpace(r.URL.Query().Get("platform_candidate_id"))
	platformThreadID := strings.TrimSpace(r.URL.Query().Get("platform_thread_id"))
	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	if platformID == "" || (platformCandidateID == "" && platformThreadID == "" && normalizeCandidatePhone(phone) == "") {
		writeAutoReplyError(w, http.StatusBadRequest, "CANDIDATE_LOOKUP_REQUIRED", "读取候选人资料需要招聘平台，以及候选人ID、会话ID或手机号")
		return
	}
	accountOwned, err := s.ownsPlatformAccount(requestContext, accountID, platformID)
	if err != nil {
		writeAutoReplyInternalError(w, "PLATFORM_ACCOUNT_CHECK_FAILED", "招聘平台账号归属暂时没查清楚", err)
		return
	}
	if !accountOwned {
		writeAutoReplyError(w, http.StatusForbidden, "PLATFORM_ACCOUNT_FORBIDDEN", "这个招聘平台账号不属于当前登录账号")
		return
	}

	response := autoReplyCandidateStateResponse{OK: true, Attachments: make([]StoredResumeAttachment, 0)}
	candidateID := ""
	if platformCandidateID != "" {
		identity, err := s.store.FindCandidatePlatformIdentity(r.Context(), requestContext.Tenant.ID, platformID, accountID, platformCandidateID)
		if err == nil {
			response.Identity = &identity
			candidateID = identity.CandidateID
		} else if !errors.Is(err, ErrNotFound) {
			writeAutoReplyStoreError(w, err, "平台候选人身份暂时没读出来")
			return
		}
	}
	if platformThreadID != "" {
		conversation, err := s.store.FindAutoReplyConversation(r.Context(), requestContext.Tenant.ID, platformID, accountID, platformThreadID)
		if err == nil {
			response.Conversation = &conversation
			if candidateID == "" {
				candidateID = conversation.CandidateID
			}
		} else if !errors.Is(err, ErrNotFound) {
			writeAutoReplyStoreError(w, err, "候选人会话状态暂时没读出来")
			return
		}
	}
	if candidateID == "" && normalizeCandidatePhone(phone) != "" {
		resolvedID, err := s.store.CandidateIDByPhone(r.Context(), requestContext.Tenant.ID, phone)
		if err == nil {
			candidateID = resolvedID
		} else if !errors.Is(err, ErrNotFound) {
			writeAutoReplyStoreError(w, err, "手机号身份暂时没读出来")
			return
		}
	}
	if candidateID != "" {
		candidate, err := s.store.GetAutoReplyCandidateProfile(r.Context(), requestContext.Tenant.ID, candidateID)
		if err != nil {
			writeAutoReplyStoreError(w, err, "正式简历暂时没读出来")
			return
		}
		response.Candidate = &candidate
		response.Found = true
	}
	conversationID := ""
	if response.Conversation != nil {
		conversationID = response.Conversation.ID
	}
	if candidateID != "" || conversationID != "" {
		attachments, err := s.store.ListResumeAttachments(r.Context(), requestContext.Tenant.ID, candidateID, conversationID)
		if err != nil {
			writeAutoReplyStoreError(w, err, "简历附件状态暂时没读出来")
			return
		}
		response.Attachments = attachments
		response.HasResumeAttachment = len(attachments) > 0
	}
	writeJSON(w, http.StatusOK, response)
}

// agentPositionSnapshot 返回本地流程运行需要的岗位、公司、条件和实时开关快照。
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

// agentSaveCandidate 校验手机号和简历字段后保存或更新团队内正式候选人。
func (s *AutoReplyService) agentSaveCandidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "正式简历这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var payload autoReplyCandidateRequest
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	position, err := s.positions.PositionByID(requestContext.Tenant.ID, requestContext.Session.Email, strings.TrimSpace(payload.PositionID), false)
	if errors.Is(err, ErrNotFound) {
		writeAutoReplyError(w, http.StatusNotFound, "POSITION_NOT_FOUND", "这个岗位没有找到，可能已经被删除了")
		return
	}
	if err != nil {
		writeAutoReplyInternalError(w, "POSITION_LOAD_FAILED", "岗位信息暂时没读出来", err)
		return
	}
	if err := validateAutoReplyCandidateRequest(payload); err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "CANDIDATE_VALIDATION_FAILED", err.Error())
		return
	}
	accountOwned, err := s.ownsPlatformAccount(requestContext, payload.PlatformAccountID, payload.PlatformID)
	if err != nil {
		writeAutoReplyInternalError(w, "PLATFORM_ACCOUNT_CHECK_FAILED", "招聘平台账号归属暂时没查清楚", err)
		return
	}
	if !accountOwned {
		writeAutoReplyError(w, http.StatusForbidden, "PLATFORM_ACCOUNT_FORBIDDEN", "这个招聘平台账号不属于当前登录账号")
		return
	}
	var existingIdentity CandidatePlatformIdentity
	if strings.TrimSpace(payload.PlatformCandidateID) != "" {
		existingIdentity, err = s.store.FindCandidatePlatformIdentity(
			r.Context(), requestContext.Tenant.ID, payload.PlatformID,
			payload.PlatformAccountID, payload.PlatformCandidateID,
		)
		if err != nil && !errors.Is(err, ErrNotFound) {
			writeAutoReplyStoreError(w, err, "平台候选人身份暂时没读出来")
			return
		}
		if errors.Is(err, ErrNotFound) {
			existingIdentity = CandidatePlatformIdentity{}
		}
	}
	normalizedPhone := normalizeCandidatePhone(payload.Phone)
	if existingIdentity.NormalizedPhone != "" && existingIdentity.NormalizedPhone != normalizedPhone {
		writeAutoReplyError(w, http.StatusConflict, "CANDIDATE_IDENTITY_CONFLICT", "这个候选人的平台身份和手机号对不上，我先不乱合并，请人工确认")
		return
	}
	canonicalID, err := s.store.CandidateIDByPhone(r.Context(), requestContext.Tenant.ID, payload.Phone)
	if err != nil && !errors.Is(err, ErrNotFound) {
		writeAutoReplyStoreError(w, err, "手机号身份暂时没查清楚")
		return
	}
	if errors.Is(err, ErrNotFound) {
		canonicalID = existingIdentity.CandidateID
	}
	if canonicalID != "" && existingIdentity.CandidateID != "" && canonicalID != existingIdentity.CandidateID {
		writeAutoReplyError(w, http.StatusConflict, "CANDIDATE_IDENTITY_CONFLICT", "这个候选人的平台身份和手机号对应到两份简历，我先不乱合并，请人工确认")
		return
	}
	profile, err := s.candidates.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: canonicalID, UserEmail: requestContext.Session.Email,
		PlatformID: strings.TrimSpace(payload.PlatformID), PlatformCandidateID: strings.TrimSpace(payload.PlatformCandidateID),
		CandidateName: strings.TrimSpace(payload.CandidateName), Gender: strings.TrimSpace(payload.Gender),
		BirthYM: strings.TrimSpace(payload.BirthYM), BirthYMPrecision: strings.TrimSpace(payload.BirthYMPrecision),
		NormalizedPhone: normalizedPhone, Phone: strings.TrimSpace(payload.Phone),
		Email: strings.TrimSpace(payload.Email), WorkRegion: strings.TrimSpace(payload.WorkRegion),
		WorkYears: strings.TrimSpace(payload.WorkYears), ExpectedSalaryMin: payload.ExpectedSalaryMin,
		ExpectedSalaryMax: payload.ExpectedSalaryMax, BasicInfo: payload.BasicInfo,
		EducationLevel: payload.EducationLevel, ExpectedPosition: payload.ExpectedPosition,
		OnlineStatus: payload.OnlineStatus, PersonalDescription: payload.PersonalDescription,
		WorkStatus: payload.WorkStatus, RawText: payload.RawText, WorkExperiences: payload.WorkExperiences,
		Educations: payload.Educations, Certificates: payload.Certificates, Honors: payload.Honors,
		ProjectExperiences: payload.ProjectExperiences, Communications: payload.Communications,
	})
	if err != nil {
		writeAutoReplyStoreError(w, err, "正式简历没保存成功")
		return
	}
	canonicalID, err = s.store.ResolveCanonicalCandidateByPhone(r.Context(), requestContext.Tenant.ID, profile.ID, payload.Phone)
	if err != nil {
		writeAutoReplyStoreError(w, err, "手机号身份没保存成功")
		return
	}
	var identity CandidatePlatformIdentity
	if strings.TrimSpace(payload.PlatformCandidateID) != "" {
		identity, err = s.store.UpsertCandidatePlatformIdentity(r.Context(), CandidatePlatformIdentity{
			TenantID: requestContext.Tenant.ID, CandidateID: canonicalID,
			PlatformID: payload.PlatformID, PlatformAccountID: payload.PlatformAccountID,
			PlatformCandidateID: payload.PlatformCandidateID, CandidateName: payload.CandidateName,
			Gender: payload.Gender, NormalizedPhone: payload.Phone,
		})
		if err != nil {
			writeAutoReplyStoreError(w, err, "平台候选人身份没保存成功")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "candidate_id": canonicalID, "platform_identity": identity,
		"position": publicAutoReplyPosition(position),
	})
}

// agentSaveIdentity 保存有稳定平台候选人 ID 的身份映射。
func (s *AutoReplyService) agentSaveIdentity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "平台身份这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item CandidatePlatformIdentity
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	accountOwned, err := s.ownsPlatformAccount(requestContext, item.PlatformAccountID, item.PlatformID)
	if err != nil {
		writeAutoReplyInternalError(w, "PLATFORM_ACCOUNT_CHECK_FAILED", "招聘平台账号归属暂时没查清楚", err)
		return
	}
	if !accountOwned {
		writeAutoReplyError(w, http.StatusForbidden, "PLATFORM_ACCOUNT_FORBIDDEN", "这个招聘平台账号不属于当前登录账号")
		return
	}
	saved, err := s.store.UpsertCandidatePlatformIdentity(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "平台候选人身份没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "identity": saved})
}

// agentSaveConversation 保存可在正式候选人和岗位未解析前存在的会话。
func (s *AutoReplyService) agentSaveConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "候选人会话这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyConversation
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	if item.PositionID != "" {
		_, positionErr := s.positions.PositionByID(requestContext.Tenant.ID, requestContext.Session.Email, item.PositionID, false)
		if errors.Is(positionErr, ErrNotFound) {
			writeAutoReplyError(w, http.StatusNotFound, "POSITION_NOT_FOUND", "会话对应的岗位没找到，我先不乱归类")
			return
		}
		if positionErr != nil {
			writeAutoReplyInternalError(w, "POSITION_LOAD_FAILED", "会话对应的岗位暂时没读出来", positionErr)
			return
		}
	}
	accountOwned, err := s.ownsPlatformAccount(requestContext, item.PlatformAccountID, item.PlatformID)
	if err != nil {
		writeAutoReplyInternalError(w, "PLATFORM_ACCOUNT_CHECK_FAILED", "招聘平台账号归属暂时没查清楚", err)
		return
	}
	if !accountOwned {
		writeAutoReplyError(w, http.StatusForbidden, "PLATFORM_ACCOUNT_FORBIDDEN", "这个招聘平台账号不属于当前登录账号")
		return
	}
	saved, err := s.store.UpsertAutoReplyConversation(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "候选人会话没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "conversation": saved})
}

// agentSyncMessages 幂等保存聊天消息并更新会话差量游标。
func (s *AutoReplyService) agentSyncMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "聊天同步这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var payload struct {
		ConversationID  string             `json:"conversation_id"`
		HistoryComplete bool               `json:"history_complete"`
		Messages        []AutoReplyMessage `json:"messages"`
	}
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	result, err := s.store.SyncAutoReplyMessages(r.Context(), requestContext.Tenant.ID, strings.TrimSpace(payload.ConversationID), payload.HistoryComplete, payload.Messages)
	if err != nil {
		writeAutoReplyStoreError(w, err, "聊天记录没同步成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sync": result})
}

// agentListMessages 返回本地 AI 工具需要的当前会话最近聊天记录。
func (s *AutoReplyService) agentListMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "聊天记录这里只支持读取")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	items, err := s.store.ListAutoReplyMessages(r.Context(), requestContext.Tenant.ID, strings.TrimSpace(r.URL.Query().Get("conversation_id")), 5000)
	if err != nil {
		writeAutoReplyStoreError(w, err, "聊天记录暂时没读出来")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "messages": items})
}

// agentConfirmations 读取或保存候选人确认项。
func (s *AutoReplyService) agentConfirmations(w http.ResponseWriter, r *http.Request) {
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.ListConfirmationItems(r.Context(), requestContext.Tenant.ID, strings.TrimSpace(r.URL.Query().Get("conversation_id")))
		if err != nil {
			writeAutoReplyStoreError(w, err, "候选人确认项暂时没读出来")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "confirmation_items": items})
		return
	}
	if r.Method != http.MethodPut {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "候选人确认项只支持读取或保存")
		return
	}
	var item CandidateConfirmationItem
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.UpsertConfirmationItem(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "候选人确认项没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "confirmation_item": saved})
}

// ownsPlatformAccount 判断平台账号是否属于当前登录用户，空账号仅用于平台无法提供账号标识的会话。
func (s *AutoReplyService) ownsPlatformAccount(requestContext autoReplyRequestContext, accountID, platformID string) (bool, error) {
	if strings.TrimSpace(accountID) == "" {
		return true, nil
	}
	items, err := s.accounts.ListPlatformAccounts(requestContext.Tenant.ID, requestContext.Session.Email, strings.TrimSpace(platformID), false)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.ID == strings.TrimSpace(accountID) {
			return true, nil
		}
	}
	return false, nil
}

// validateAutoReplyCandidateRequest 校验正式简历最关键的手机号、性别和出生年月精度。
func validateAutoReplyCandidateRequest(item autoReplyCandidateRequest) error {
	if strings.TrimSpace(item.PositionID) == "" || strings.TrimSpace(item.PlatformID) == "" {
		return errors.New("正式简历缺少岗位或招聘平台")
	}
	if normalizeCandidatePhone(item.Phone) == "" {
		return errors.New("没有手机号时只能保存临时会话，暂时不能进入正式简历库")
	}
	if item.Gender != "" && item.Gender != "男" && item.Gender != "女" {
		return errors.New("候选人性别只支持男、女或空值")
	}
	if item.BirthYMPrecision != "" && item.BirthYMPrecision != "month" && item.BirthYMPrecision != "year_estimated" {
		return errors.New("出生年月精度只支持精确到月或按年龄估算年份")
	}
	birthYM := strings.TrimSpace(item.BirthYM)
	precision := strings.TrimSpace(item.BirthYMPrecision)
	if birthYM != "" && precision == "" {
		return errors.New("有出生年月时需要说明是精确到月，还是按年龄估算年份")
	}
	if precision == "year_estimated" && !validAutoReplyBirthYear(birthYM) {
		return errors.New("按年龄估算的出生时间只能保存四位年份，不能编造月份")
	}
	if precision == "month" {
		parts := strings.Split(birthYM, "-")
		month := 0
		if len(parts) != 2 || !validAutoReplyBirthYear(parts[0]) {
			return errors.New("精确出生年月需要使用 YYYY-MM 格式")
		}
		month, _ = strconv.Atoi(parts[1])
		if len(parts[1]) != 2 || month < 1 || month > 12 {
			return errors.New("精确出生年月需要使用 YYYY-MM 格式")
		}
	}
	return nil
}

// validAutoReplyBirthYear 判断出生年份是合理的四位年份。
func validAutoReplyBirthYear(value string) bool {
	if len(value) != 4 {
		return false
	}
	year, err := strconv.Atoi(value)
	return err == nil && year >= 1900 && year <= time.Now().Year()
}
