// 本文件负责提供简历库候选人列表 HTTP API。
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// CandidateService 处理简历库和任务候选人查询请求。
type CandidateService struct {
	auth        *AuthService
	store       CandidateStore
	tenantStore TenantStore
}

// NewCandidateService 创建候选人查询服务。
// auth 用于认证当前用户，store 用于读取候选人，tenantStore 用于限定团队范围。
func NewCandidateService(auth *AuthService, store CandidateStore, tenantStore TenantStore) *CandidateService {
	return &CandidateService{auth: auth, store: store, tenantStore: tenantStore}
}

// Collection 处理简历库候选人列表请求。
// 支持通过 task_id 查询某个任务下的候选人，否则返回当前团队候选人。
func (s *CandidateService) Collection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.ClearTeam(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.store == nil || s.tenantStore == nil {
		writeError(w, http.StatusInternalServerError, "candidate store is not ready")
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}

	query := TaskCandidateQuery{
		TaskID:     strings.TrimSpace(r.URL.Query().Get("task_id")),
		PositionID: strings.TrimSpace(r.URL.Query().Get("position_id")),
		Keyword:    strings.TrimSpace(r.URL.Query().Get("keyword")),
		Page:       parsePositiveInt(r.URL.Query().Get("page")),
		PageSize:   parsePositiveInt(r.URL.Query().Get("page_size")),
	}
	result, err := s.store.ListTaskCandidates(tenant.ID, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"candidates": publicTaskCandidates(result.Items),
		"total":      result.Total,
		"page":       result.Page,
		"page_size":  result.PageSize,
	})
}

// ClearTeam 清空当前团队的全部候选人数据。
// 会删除候选人主体，关联的 AI 事件和触达记录由数据库级联删除。
func (s *CandidateService) ClearTeam(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.store == nil || s.tenantStore == nil {
		writeError(w, http.StatusInternalServerError, "candidate store is not ready")
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	deleted, err := s.store.DeleteTeamCandidates(tenant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": deleted,
	})
}

// Detail 处理单个候选人详情请求。
// 路径格式为 /api/candidates/{id}，只允许查看当前团队内候选人。
func (s *CandidateService) Detail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	candidateID := strings.TrimPrefix(r.URL.Path, "/api/candidates/")
	if candidateID == "" || candidateID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "candidate id is required")
		return
	}
	engagementID := strings.TrimSpace(r.URL.Query().Get("engagement_id"))
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	item, err := s.store.GetTaskCandidate(tenant.ID, candidateID, engagementID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "candidate not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load candidate")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"candidate": publicTaskCandidate(item),
	})
}

// currentSession 从请求中解析当前登录会话。
func (s *CandidateService) currentSession(w http.ResponseWriter, r *http.Request) (Session, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return Session{}, false
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return Session{}, false
	}
	return session, true
}

// parsePositiveInt 解析正整数查询参数。
// value 为 URL 参数原文，解析失败时返回 0。
func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

// publicTaskCandidates 将候选人记录列表转换为前端响应结构。
func publicTaskCandidates(items []TaskCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicTaskCandidate(item))
	}
	return result
}

// publicTaskCandidate 将候选人记录转换为前端响应结构。
func publicTaskCandidate(item TaskCandidate) map[string]any {
	return map[string]any{
		"id":                    item.ID,
		"engagement_id":         item.EngagementID,
		"engagement_status":     item.EngagementStatus,
		"task_id":               item.TaskID,
		"position_id":           item.PositionID,
		"position_name":         item.PositionName,
		"platform_account_id":   item.PlatformAccountID,
		"user_email":            item.UserEmail,
		"platform_id":           item.PlatformID,
		"platform_candidate_id": item.PlatformCandidateID,
		"candidate_name":        item.CandidateName,
		"birth_ym":              item.BirthYM,
		"phone":                 item.Phone,
		"email":                 item.Email,
		"work_region":           item.WorkRegion,
		"work_years":            item.WorkYears,
		"expected_salary_min":   item.ExpectedSalaryMin,
		"expected_salary_max":   item.ExpectedSalaryMax,
		"basic_info":            item.BasicInfo,
		"education_level":       item.EducationLevel,
		"expected_position":     item.ExpectedPosition,
		"online_status":         item.OnlineStatus,
		"personal_description":  item.PersonalDescription,
		"work_status":           item.WorkStatus,
		"raw_text":              item.RawText,
		"filter_text":           item.FilterText,
		"work_experiences":      safeSlice(item.WorkExperiences),
		"educations":            safeSlice(item.Educations),
		"certificates":          safeSlice(item.Certificates),
		"honors":                safeSlice(item.Honors),
		"project_experiences":   safeSlice(item.ProjectExperiences),
		"communications":        safeSlice(item.Communications),
		"resume_url":            item.ResumeURL,
		"resume_text":           item.ResumeText,
		"ai_detail_reason":      item.AIDetailReason,
		"ai_detail_score":       item.AIDetailScore,
		"ai_greet_reason":       item.AIGreetReason,
		"ai_greet_score":        item.AIGreetScore,
		"ai_review_reason":      item.AIReviewReason,
		"ai_review_score":       item.AIReviewScore,
		"ext":                   safeMap(item.Ext),
		"first_seen_at":         item.FirstSeenAt,
		"detail_fetched_at":     item.DetailFetchedAt,
		"greeted_at":            item.GreetedAt,
		"created_at":            item.CreatedAt,
		"updated_at":            item.UpdatedAt,
		"events":                safeSlice(item.Events),
	}
}

// safeSlice 确保前端收到数组而不是 null。
func safeSlice[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// safeMap 确保前端收到对象而不是 null。
func safeMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
