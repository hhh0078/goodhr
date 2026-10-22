// 本文件在付费模型调用及推荐返回前重新核对岗位、候选人版本和团队权限，避免长任务沿用过期授权。
package httpapi

import "errors"

// checkRecommendationDocuments 重新读取全部本批候选人；参数只有编号和版本，结果只返回当前可信身份。
func (s *AssistantService) checkRecommendationDocuments(scope CandidateLibraryScope, documents []ResumeRerankDocument) (CandidateLibraryScope, error) {
	current, err := s.currentScope(scope)
	if err != nil {
		return scope, err
	}
	for _, document := range documents {
		candidate, err := s.candidates.store.GetPositionCandidate(current.TenantID, document.CandidateID, "", current.UserEmail, current.IsAdmin)
		if err != nil || candidate.RowVersion != document.ResumeVersion {
			return current, assistantFatalError{"候选人资料或访问权限已变化，请重新检索"}
		}
	}
	return current, nil
}

// assistantJobUnchanged 同时核对版本与完整JD，防止未正确递增版本的旧接口让推荐使用过期资料。
func assistantJobUnchanged(original, current HeadhunterJob, preferences string) bool {
	return original.ID == current.ID && original.Revision == current.Revision && assistantJobSource(original, preferences) == assistantJobSource(current, preferences)
}

// checkRecommendationJob 最终返回前读取岗位最新资料，变化后拒绝把旧条件结果当成当前推荐。
func (s *AssistantService) checkRecommendationJob(scope CandidateLibraryScope, original HeadhunterJob, preferences string) error {
	current, _, err := s.recommendationJob(scope, original.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return assistantFatalError{"岗位已删除或无权访问，请重新选择"}
		}
		return err
	}
	if !assistantJobUnchanged(original, current, preferences) {
		return assistantFatalError{"岗位资料已变化，本轮旧条件结果未采用，请重新检索"}
	}
	return nil
}
