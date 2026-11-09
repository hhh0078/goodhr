// 本文件仅接收本地已完成的结构化简历，复用现有自动入库和人工字段保护。
package httpapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
)

// agentSaveCandidate 仅在岗位明确开启结构化输出后保存清洗结果，原文和聊天由独立的服务器沟通记录保存。
func (s *AutoReplyService) agentSaveCandidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "结构化简历这里只支持保存")
		return
	}
	var payload autoReplyCandidateRequest
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	requestContext, position, ok := s.positionRequestContext(w, r, payload.PositionID, true, true)
	if !ok || !allowAutoReplyStructuredResult(w, position) {
		return
	}
	if payload.ReopenProof != nil && requestContext.Session.Kind == extensionSessionKind {
		writeAutoReplyError(w, 403, "DEVICE_BINDING_REQUIRED", "重新建档请使用已绑定的本地程序")
		return
	}
	if payload.RawText != "" || len(payload.Communications) != 0 || payload.AvatarURL != "" {
		writeAutoReplyError(w, http.StatusBadRequest, "STRUCTURED_FIELDS_ONLY", "这个入口只接收入库字段，聊天和附件请使用云端自动回复流程")
		return
	}
	if payload.PlatformID != position.PlatformID || strings.TrimSpace(payload.PlatformCandidateID) == "" {
		writeAutoReplyError(w, http.StatusBadRequest, "CANDIDATE_SOURCE_REQUIRED", "简历来源还没对上，请稍后重试")
		return
	}
	fields, err := candidateMutationFields(map[string]any{
		"candidate_name": payload.CandidateName, "birth_ym": payload.BirthYM,
		"phone": payload.Phone, "email": payload.Email, "gender": payload.Gender,
		"wechat": payload.Wechat, "birth_ym_precision": payload.BirthYMPrecision,
		"avatar_data_url": payload.AvatarDataURL,
	}, true)
	if err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "CANDIDATE_VALIDATION_FAILED", err.Error())
		return
	}
	secret := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"))
	profile, err := s.candidates.SaveCandidateProfile(CandidateProfileInput{
		ReopenServerWitness: secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(r.Header.Get("X-GoodHR-Runtime-Service"))) == 1,
		ReopenProof:         payload.ReopenProof, ReopenPlatformAccount: payload.PlatformAccountID, ReopenIsAdmin: requestContext.IsAdmin,
		TenantID: requestContext.Tenant.ID, UserEmail: requestContext.Session.Email, AutoReplyPositionID: position.ID,
		PlatformID: position.PlatformID, PlatformCandidateID: strings.TrimSpace(payload.PlatformCandidateID),
		CandidateName: candidateString(fields, "candidate_name"), BirthYM: candidateString(fields, "birth_ym"),
		Phone: candidateString(fields, "phone"), Email: candidateString(fields, "email"),
		WorkRegion: payload.WorkRegion, WorkYears: payload.WorkYears,
		ExpectedSalaryMin: payload.ExpectedSalaryMin, ExpectedSalaryMax: payload.ExpectedSalaryMax,
		EducationLevel: payload.EducationLevel, ExpectedPosition: payload.ExpectedPosition,
		OnlineStatus: payload.OnlineStatus, PersonalDescription: payload.PersonalDescription,
		WorkStatus: payload.WorkStatus, WorkExperiences: payload.WorkExperiences,
		Educations: payload.Educations, Certificates: payload.Certificates, Honors: payload.Honors,
		ProjectExperiences: payload.ProjectExperiences, Ext: map[string]any{"structured_fields": fields},
	})
	if err != nil {
		if errors.Is(err, errReopenTargetConflict) {
			writeAutoReplyError(w, http.StatusConflict, "CANDIDATE_REOPEN_TARGET_CONFLICT", err.Error())
			return
		}
		if payload.ReopenProof != nil && errors.Is(err, errCandidateDeleted) {
			writeAutoReplyError(w, http.StatusConflict, "CANDIDATE_REOPEN_PROOF_CONFLICT", "这次新联系的来源还没核对上，资料已保留，核对后可重试")
			return
		}
		writeAutoReplyStoreError(w, err, "这份结构化简历还没存好，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidate_id": profile.ID})
}

// allowAutoReplyStructuredResult 以服务端岗位开关再次确认本地结构化结果的上传权限。
func allowAutoReplyStructuredResult(w http.ResponseWriter, position Position) bool {
	if position.CommonConfig["output_structured_resume"] != true {
		writeAutoReplyError(w, http.StatusForbidden, "STRUCTURED_RESUME_DISABLED", "这个岗位还没有开启自动整理简历")
		return false
	}
	return true
}

// automaticCandidateExtraFields 仅提取经结构化入口校验过的附加字段，不接受原始文本或任意扩展字段。
func automaticCandidateExtraFields(input CandidateProfileInput) map[string]any {
	fields, _ := input.Ext["structured_fields"].(map[string]any)
	result := map[string]any{}
	for _, key := range []string{"gender", "wechat", "birth_ym_precision", "avatar_data_url"} {
		if value, ok := fields[key]; ok && !candidateEmptyValue(value) {
			result[key] = value
		}
	}
	return result
}
