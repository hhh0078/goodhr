// 本文件负责提供简历库候选人列表 HTTP API。
package httpapi

import (
	"encoding/json"
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

type candidateNoteRequest struct {
	Content string `json:"content"`
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
		Keyword:    firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("keyword")), strings.TrimSpace(r.URL.Query().Get("q"))),
		Page:       parsePositiveInt(r.URL.Query().Get("page")),
		PageSize:   parsePositiveInt(r.URL.Query().Get("page_size")),
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if !isAdmin {
		query.UserEmail = session.Email
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
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if !isAdmin {
		writeError(w, http.StatusForbidden, "只有团队管理员才能清空简历库")
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
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	item, err := s.store.GetTaskCandidate(tenant.ID, candidateID, engagementID, session.Email, isAdmin)
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

// Notes 处理候选人备注列表和新增请求。
// 路径格式为 /api/candidates/{id}/notes，权限沿用简历详情可见范围。
func (s *CandidateService) Notes(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.store == nil || s.tenantStore == nil {
		writeError(w, http.StatusInternalServerError, "candidate store not ready")
		return
	}
	candidateID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/notes")
	if candidateID == "" || candidateID == r.URL.Path {
		writeError(w, http.StatusBadRequest, "candidate id required")
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed get tenant")
		return
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if _, err := s.store.GetTaskCandidate(tenant.ID, candidateID, "", session.Email, isAdmin); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "candidate not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed load candidate")
		return
	}
	switch r.Method {
	case http.MethodGet:
		notes, err := s.store.ListCandidateNotes(tenant.ID, candidateID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed list notes")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "notes": publicCandidateNotes(notes)})
	case http.MethodPost:
		var req candidateNoteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		content := strings.TrimSpace(req.Content)
		if content == "" {
			writeError(w, http.StatusBadRequest, "备注内容不能为空")
			return
		}
		if len([]rune(content)) > 1000 {
			writeError(w, http.StatusBadRequest, "备注有点长，我先小声拦一下，控制在1000字内")
			return
		}
		noteEvent, err := s.store.SaveCandidateEvent(CandidateEvent{
			CandidateID: candidateID,
			EventType:   "manual_note",
			MessageText: content,
			Metadata:    map[string]any{"author_email": session.Email},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed save note")
			return
		}
		note := CandidateNote{ID: noteEvent.ID, CandidateID: candidateID, Content: content, AuthorEmail: session.Email, CreatedAt: noteEvent.CreatedAt}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note": publicCandidateNote(note)})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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
		"id":                       item.ID,
		"engagement_id":            item.EngagementID,
		"engagement_status":        item.EngagementStatus,
		"task_id":                  item.TaskID,
		"position_id":              item.PositionID,
		"position_name":            item.PositionName,
		"platform_account_id":      item.PlatformAccountID,
		"user_email":               item.UserEmail,
		"platform_id":              item.PlatformID,
		"platform_candidate_id":    item.PlatformCandidateID,
		"candidate_name":           item.CandidateName,
		"birth_ym":                 item.BirthYM,
		"phone":                    item.Phone,
		"email":                    item.Email,
		"work_region":              item.WorkRegion,
		"work_years":               item.WorkYears,
		"expected_salary_min":      item.ExpectedSalaryMin,
		"expected_salary_max":      item.ExpectedSalaryMax,
		"basic_info":               item.BasicInfo,
		"education_level":          item.EducationLevel,
		"expected_position":        item.ExpectedPosition,
		"online_status":            item.OnlineStatus,
		"personal_description":     item.PersonalDescription,
		"work_status":              item.WorkStatus,
		"work_experiences":         safeSlice(item.WorkExperiences),
		"educations":               safeSlice(item.Educations),
		"certificates":             safeSlice(item.Certificates),
		"honors":                   safeSlice(item.Honors),
		"project_experiences":      safeSlice(item.ProjectExperiences),
		"colleague_communications": safeSlice(item.Communications),
		"ai": map[string]any{
			"detail": map[string]any{"score": item.AIDetailScore, "reason": item.AIDetailReason},
			"greet":  map[string]any{"score": item.AIGreetScore, "reason": item.AIGreetReason},
		},
		"notes":             publicCandidateNotes(item.Notes),
		"raw_text":          item.RawText,
		"first_seen_at":     item.FirstSeenAt,
		"detail_fetched_at": item.DetailFetchedAt,
		"greeted_at":        item.GreetedAt,
		"created_at":        item.CreatedAt,
		"updated_at":        item.UpdatedAt,
	}
}

// publicCandidateNotes 将备注列表转换为前端响应结构。
// items 为候选人备注记录，返回安全数组。
func publicCandidateNotes(items []CandidateNote) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicCandidateNote(item))
	}
	return result
}

// publicCandidateNote 将单条备注转换为前端响应结构。
// item 为候选人备注记录。
func publicCandidateNote(item CandidateNote) map[string]any {
	return map[string]any{
		"id":           item.ID,
		"candidate_id": item.CandidateID,
		"content":      item.Content,
		"author_email": item.AuthorEmail,
		"created_at":   item.CreatedAt,
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
