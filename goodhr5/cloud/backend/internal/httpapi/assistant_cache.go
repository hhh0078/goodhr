// 本文件只读核对浏览器长期历史中的候选人引用，任务过期不影响校验，也不会重新调用AI或续期。
package httpapi

import (
	"errors"
	"net/http"
)

// assistantCachedReference 只携带已展示结果的候选编号及版本，不信任浏览器保存的权限或正文。
type assistantCachedReference struct {
	CandidateID   string `json:"candidate_id"`
	ResumeVersion int64  `json:"resume_version"`
}

// validateCachedReferences 返回失效引用；调用者应清除对应旧结果，不能将未联网核对的历史当成当前推荐。
func (s *AssistantService) validateCachedReferences(w http.ResponseWriter, r *http.Request, scope CandidateLibraryScope) {
	var input struct {
		References []assistantCachedReference `json:"references"`
	}
	if err := readAssistantBody(w, r, &input); err != nil || input.References == nil || len(input.References) > 200 {
		writeError(w, 400, "请提供不超过200条候选人版本引用")
		return
	}
	invalid := []assistantCachedReference{}
	for _, reference := range input.References {
		if !assistantSafeID.MatchString(reference.CandidateID) || reference.ResumeVersion <= 0 {
			writeError(w, 400, "候选人编号或版本没有读完整")
			return
		}
		candidate, err := s.candidates.store.GetPositionCandidate(scope.TenantID, reference.CandidateID, "", scope.UserEmail, scope.IsAdmin)
		if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrRecruitmentAccess) {
			writeError(w, 503, "历史资料暂时没核对完，请稍后再试")
			return
		}
		if err != nil || candidate.ID == builtinCandidateID || candidate.RowVersion != reference.ResumeVersion {
			invalid = append(invalid, reference)
		}
	}
	current, err := s.currentScope(scope)
	if err != nil || current.TenantID != scope.TenantID || current.IsAdmin != scope.IsAdmin {
		writeError(w, 403, "团队权限已变化，请重新核对历史资料")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "invalid": invalid})
}
