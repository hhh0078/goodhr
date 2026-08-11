// 本文件负责提供简历库候选人列表 HTTP API。
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// CandidateService 处理简历库和岗位候选人查询请求。
type CandidateService struct {
	auth        *AuthService
	store       CandidateStore
	tenantStore TenantStore
	autoReply   *PostgresAutoReplyStore
	resumeDir   string
}

type candidateNoteRequest struct {
	Content string `json:"content"`
}

// NewCandidateService 创建候选人查询服务。
// auth 用于认证当前用户，store 用于读取候选人，tenantStore 用于限定团队范围，autoReply 用于读取关联沟通资料，resumeDir 为附件保存目录。
func NewCandidateService(auth *AuthService, store CandidateStore, tenantStore TenantStore, autoReply *PostgresAutoReplyStore, resumeDir string) *CandidateService {
	return &CandidateService{auth: auth, store: store, tenantStore: tenantStore, autoReply: autoReply, resumeDir: strings.TrimSpace(resumeDir)}
}

// Collection 处理简历库候选人列表请求。
// 支持通过 position_id 查询某个岗位下的候选人，否则返回当前团队候选人。
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

	query := PositionCandidateQuery{
		PositionID:      strings.TrimSpace(r.URL.Query().Get("position_id")),
		PlatformID:      normalizeCandidatePlatformID(r.URL.Query().Get("platform_id")),
		Keyword:         firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("keyword")), strings.TrimSpace(r.URL.Query().Get("q"))),
		PhoneStatus:     normalizeCandidatePhoneStatus(r.URL.Query().Get("has_phone")),
		ConditionStatus: normalizeCandidateConditionStatus(r.URL.Query().Get("condition_status")),
		Sort:            normalizeCandidateSort(r.URL.Query().Get("sort")),
		Page:            parsePositiveInt(r.URL.Query().Get("page")),
		PageSize:        parsePositiveInt(r.URL.Query().Get("page_size")),
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if !isAdmin {
		query.UserEmail = session.Email
	}
	result, err := s.store.ListPositionCandidates(tenant.ID, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list candidates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"candidates": publicPositionCandidates(result.Items),
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
	result, err := s.store.DeleteTeamCandidates(tenant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear candidates")
		return
	}
	cleanupFailed := s.cleanupCandidateAttachments(result.AttachmentPaths) + s.cleanupCandidateAvatars(result.AvatarURLs)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"deleted":        result.Deleted,
		"cleanup_failed": cleanupFailed,
	})
}

// Detail 处理单个候选人详情请求。
// 路径格式为 /api/candidates/{id}，只允许查看当前团队内候选人。
func (s *CandidateService) Detail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.Delete(w, r)
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
	item, err := s.store.GetPositionCandidate(tenant.ID, candidateID, engagementID, session.Email, isAdmin)
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
		"candidate": publicPositionCandidate(item),
	})
}

// Delete 删除当前用户有权查看的单个候选人及其全部关联资料。
// 路径格式为 /api/candidates/{id}，删除数据库事务成功后再清理附件文件。
func (s *CandidateService) Delete(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.store == nil || s.tenantStore == nil {
		writeError(w, http.StatusInternalServerError, "简历库还没准备好，请稍后再试")
		return
	}
	candidateID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/")
	if candidateID == "" || strings.Contains(candidateID, "/") {
		writeError(w, http.StatusBadRequest, "我还没认出要删除哪份简历")
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "团队信息暂时没读出来，请稍后再试")
		return
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	engagementID := strings.TrimSpace(r.URL.Query().Get("engagement_id"))
	if _, err = s.store.GetPositionCandidate(tenant.ID, candidateID, engagementID, session.Email, isAdmin); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "这份简历已经不在了，刷新一下就好")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除前没确认好这份简历，请稍后再试")
		return
	}
	result, err := s.store.DeleteCandidate(tenant.ID, candidateID)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "这份简历已经不在了，刷新一下就好")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "这份简历暂时没删成功，请稍后再试")
		return
	}
	cleanupFailed := s.cleanupCandidateAttachments(result.AttachmentPaths) + s.cleanupCandidateAvatars(result.AvatarURLs)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"deleted":        result.Deleted,
		"cleanup_failed": cleanupFailed,
	})
}

// cleanupCandidateAttachments 安全清理数据库删除后不再使用的候选人附件文件。
// paths 为附件相对路径，返回路径不安全或文件删除失败的数量。
func (s *CandidateService) cleanupCandidateAttachments(paths []string) int {
	failed := 0
	seen := make(map[string]struct{}, len(paths))
	for _, relativePath := range paths {
		relativePath = strings.TrimSpace(relativePath)
		if relativePath == "" {
			continue
		}
		if _, exists := seen[relativePath]; exists {
			continue
		}
		seen[relativePath] = struct{}{}
		absolutePath, err := autoReplyResumeStoragePath(s.resumeDir, relativePath)
		if err != nil {
			failed++
			log.Printf("[简历删除] 跳过不安全的附件路径")
			continue
		}
		if err = os.Remove(absolutePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failed++
			log.Printf("[简历删除] 附件文件清理失败 err=%v", err)
		}
	}
	return failed
}

// cleanupCandidateAvatars 安全清理数据库删除后不再使用的自托管候选人头像。
// avatarURLs 为数据库原有头像地址，远程地址或不安全路径只跳过、不发起网络请求。
func (s *CandidateService) cleanupCandidateAvatars(avatarURLs []string) int {
	failed := 0
	seen := make(map[string]struct{}, len(avatarURLs))
	for _, avatarURL := range avatarURLs {
		avatarURL = strings.TrimSpace(avatarURL)
		if avatarURL == "" {
			continue
		}
		if _, exists := seen[avatarURL]; exists {
			continue
		}
		seen[avatarURL] = struct{}{}
		absolutePath, err := candidateAvatarStoragePath(s.resumeDir, avatarURL)
		if err != nil {
			log.Printf("[简历删除] 跳过非自托管头像地址")
			continue
		}
		if err = os.Remove(absolutePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failed++
			log.Printf("[简历删除] 头像文件清理失败 err=%v", err)
		}
	}
	return failed
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
	if _, err := s.store.GetPositionCandidate(tenant.ID, candidateID, "", session.Email, isAdmin); err != nil {
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

// publicPositionCandidates 将候选人记录列表转换为前端响应结构。
func publicPositionCandidates(items []PositionCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, publicPositionCandidate(item))
	}
	return result
}

// publicPositionCandidate 将候选人记录转换为前端响应结构。
func publicPositionCandidate(item PositionCandidate) map[string]any {
	return map[string]any{
		"id":                       item.ID,
		"engagement_id":            item.EngagementID,
		"engagement_status":        item.EngagementStatus,
		"position_id":              item.PositionID,
		"position_name":            item.PositionName,
		"platform_account_id":      item.PlatformAccountID,
		"user_email":               item.UserEmail,
		"platform_id":              item.PlatformID,
		"platform_candidate_id":    item.PlatformCandidateID,
		"candidate_name":           item.CandidateName,
		"avatar_url":               item.AvatarURL,
		"gender":                   item.Gender,
		"birth_ym":                 item.BirthYM,
		"birth_ym_precision":       item.BirthYMPrecision,
		"normalized_phone":         item.NormalizedPhone,
		"phone":                    item.Phone,
		"email":                    item.Email,
		"wechat":                   item.Wechat,
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
