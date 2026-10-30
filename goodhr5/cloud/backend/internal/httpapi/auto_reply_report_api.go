// 本文件接收本地生成的结构化推荐报告，并提供经用户开启的公开只读访问。
package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
)

// autoReplyCompletedReportRequest 定义本地已完成报告的上传合同，不接收模型提示词或聊天消息。
type autoReplyCompletedReportRequest struct {
	PositionID  string                        `json:"position_id"`
	CandidateID string                        `json:"candidate_id"`
	InputHash   string                        `json:"input_hash"`
	Report      CandidateRecommendationReport `json:"report_data"`
	Model       string                        `json:"model"`
	TokenUsage  int                           `json:"token_usage"`
}

// agentSaveRecommendation 保存本地生成的报告，独立检查岗位授权及候选人可见范围。
func (s *AutoReplyService) agentSaveRecommendation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "生成好的报告这里只支持保存")
		return
	}
	var payload autoReplyCompletedReportRequest
	if err := decodeAutoReplyJSON(w, r, &payload); err != nil {
		return
	}
	requestContext, position, ok := s.positionRequestContext(w, r, payload.PositionID, true, true)
	if !ok || !allowAutoReplyStructuredResult(w, position) {
		return
	}
	candidate, err := s.candidates.GetPositionCandidate(requestContext.Tenant.ID, payload.CandidateID, "", requestContext.Session.Email, requestContext.IsAdmin)
	if err != nil {
		writeAutoReplyStoreError(w, err, "这份简历暂时没找到，请先同步结构化简历")
		return
	}
	if err := validateAutoReplyCompletedReport(payload); err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "REPORT_INVALID", err.Error())
		return
	}
	payload.InputHash = strings.ToLower(payload.InputHash)
	// 报告使用云端已保存的简历快照，继续尊重人工修正，也不保留本机路径或招聘平台头像地址。
	payload.Report.Candidate = autoReplyReportCandidate(candidate)
	payload.Report.Position.ID = position.ID
	payload.Report.Position.Name = position.Name
	payload.Report.Position.PlatformID = position.PlatformID
	if payload.Report.GeneratedAt.IsZero() {
		payload.Report.GeneratedAt = time.Now().UTC()
	}
	item, err := s.store.SaveAutoReplyReport(r.Context(), CandidateAutoReplyReport{
		TenantID: requestContext.Tenant.ID, CandidateID: payload.CandidateID, PositionID: position.ID,
		InputHash: payload.InputHash, Report: payload.Report, Model: payload.Model, TokenUsage: payload.TokenUsage,
	})
	if err != nil {
		writeAutoReplyStoreError(w, err, "报告暂时没保存好，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendation": item})
}

// autoReplyReportCandidate 从已经过权限校验的简历构造报告快照，保留人工维护值和小数薪资。
func autoReplyReportCandidate(candidate PositionCandidate) RecommendationCandidateSnapshot {
	return RecommendationCandidateSnapshot{
		ID: candidate.ID, Name: candidate.CandidateName,
		AvatarURL: candidateString(candidate.LibraryFields, "avatar_data_url"),
		Gender:    candidateString(candidate.LibraryFields, "gender"),
		BirthYM:   candidate.BirthYM, Phone: candidate.Phone, Email: candidate.Email,
		Wechat:     candidateString(candidate.LibraryFields, "wechat"),
		WorkRegion: candidate.WorkRegion, WorkYears: candidate.WorkYears, EducationLevel: candidate.EducationLevel,
		ExpectedPosition: candidate.ExpectedPosition, ExpectedSalaryMin: candidate.ExpectedSalaryMin, ExpectedSalaryMax: candidate.ExpectedSalaryMax,
		WorkStatus: candidate.WorkStatus, OnlineStatus: candidate.OnlineStatus, PersonalDescription: candidate.PersonalDescription,
		BasicInfo: candidate.BasicInfo, WorkExperiences: candidate.WorkExperiences, Educations: candidate.Educations,
		Certificates: candidate.Certificates, Honors: candidate.Honors, ProjectExperiences: candidate.ProjectExperiences,
		CreatedAt: candidate.CreatedAt, UpdatedAt: candidate.UpdatedAt,
	}
}

// validateAutoReplyCompletedReport 检查结果大小、分数和去重摘要，拒绝不完整或无法显示的报告。
func validateAutoReplyCompletedReport(payload autoReplyCompletedReportRequest) error {
	if strings.TrimSpace(payload.CandidateID) == "" || len(payload.InputHash) != 64 {
		return newAutoReplyValidationError("报告的简历编号或去重摘要还没准备好")
	}
	if _, err := hex.DecodeString(payload.InputHash); err != nil {
		return newAutoReplyValidationError("报告去重摘要格式没认出来")
	}
	if math.IsNaN(payload.Report.MatchScore) || math.IsInf(payload.Report.MatchScore, 0) || payload.Report.MatchScore < 0 || payload.Report.MatchScore > 100 || payload.TokenUsage < 0 {
		return newAutoReplyValidationError("报告分数请保持在0到100之间，用量不能小于0")
	}
	if strings.TrimSpace(payload.Report.ExecutiveSummary) == "" || len([]rune(payload.Report.ExecutiveSummary)) > 4000 {
		return newAutoReplyValidationError("报告需要一段不超过4000字的摘要")
	}
	raw, err := json.Marshal(payload.Report)
	if err != nil || len(raw) > 512<<10 {
		return newAutoReplyValidationError("报告内容太长，请只同步结构化结果和摘要")
	}
	return nil
}

// PublicRecommendation 仅公开已开启分享的报告，沟通全文始终留在本机。
func (s *AutoReplyService) PublicRecommendation(w http.ResponseWriter, r *http.Request) {
	setPublicRecommendationHeaders(w)
	if s == nil || s.store == nil {
		writeAutoReplyError(w, http.StatusServiceUnavailable, "REPORT_UNAVAILABLE", "报告暂时还没准备好，请稍后再试")
		return
	}
	publicID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/public/recommendations/"), "/")
	if r.Method != http.MethodGet || publicID == "" || strings.Contains(publicID, "/") {
		writeAutoReplyError(w, http.StatusNotFound, "REPORT_NOT_FOUND", "这份报告暂时没找到，可能已经关闭分享")
		return
	}
	item, err := s.store.GetPublicAutoReplyReport(r.Context(), publicID)
	if errors.Is(err, ErrNotFound) {
		writeAutoReplyError(w, http.StatusNotFound, "REPORT_NOT_FOUND", "这份报告暂时没找到，可能已经关闭分享")
		return
	}
	if err != nil {
		writeAutoReplyInternalError(w, "REPORT_LOAD_FAILED", "报告这次没读出来，请稍后再试", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendation": publicRecommendation(item)})
}

// Recommendation 根据用户明确操作开启或撤销公开分享，不因同步报告自动公开简历。
func (s *AutoReplyService) Recommendation(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/recommendations/"), "/"), "/")
	if r.Method != http.MethodPost || len(parts) != 2 || (parts[1] != "share" && parts[1] != "revoke") {
		writeAutoReplyError(w, http.StatusNotFound, "REPORT_ROUTE_NOT_FOUND", "这个报告操作暂时没找到")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	if err := s.store.SetAutoReplyReportShare(r.Context(), requestContext.Tenant.ID, requestContext.Session.Email, parts[0], parts[1] == "share"); err != nil {
		writeAutoReplyStoreError(w, err, "报告分享状态这次没存好，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "share_enabled": parts[1] == "share"})
}

// publicRecommendation 隐藏团队、内部任务和用量信息，只返回已授权分享的报告字段。
func publicRecommendation(item CandidateAutoReplyReport) map[string]any {
	return map[string]any{"public_id": item.PublicID, "version": item.Version, "status": item.Status, "match_score": item.MatchScore, "recommendation_level": item.RecommendationLevel, "summary": item.Summary, "report": item.Report, "share_enabled": item.ShareEnabled, "source_cutoff_at": item.SourceCutoffAt, "generated_at": item.GeneratedAt}
}

// setPublicRecommendationHeaders 禁止公开报告缓存、搜索收录及向外部网站发送来源地址。
func setPublicRecommendationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
