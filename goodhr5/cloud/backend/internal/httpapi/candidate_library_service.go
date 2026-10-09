// 本文件提供简历库独立新增、差异更新、查重和岗位推荐接口，不调用自动岗位运行统计。
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Route 分发候选人子资源，确保新增查重、备注和推荐路径不被详情接口误处理。
func (s *CandidateService) Route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/candidates/")
	if path == "duplicates" {
		s.Duplicates(w, r)
		return
	}
	if strings.HasSuffix(path, "/notes") {
		s.Notes(w, r)
		return
	}
	if strings.HasSuffix(path, "/recommendations") {
		s.Recommendations(w, r)
		return
	}
	if path == "" || strings.Contains(path, "/") {
		writeError(w, http.StatusNotFound, "这个入口暂时没找到")
		return
	}
	s.Detail(w, r)
}

// libraryScope 解析团队权限，操作者身份只来自登录会话。
func (s *CandidateService) libraryScope(w http.ResponseWriter, r *http.Request) (CandidateLibraryScope, bool) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return CandidateLibraryScope{}, false
	}
	if s.store == nil || s.tenantStore == nil {
		writeError(w, http.StatusServiceUnavailable, "简历库还没准备好，请稍后再试")
		return CandidateLibraryScope{}, false
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "团队信息暂时没读到")
		return CandidateLibraryScope{}, false
	}
	admin, err := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "权限暂时没读到")
		return CandidateLibraryScope{}, false
	}
	return CandidateLibraryScope{TenantID: tenant.ID, UserEmail: session.Email, AuthorName: session.Email, IsAdmin: admin}, true
}

// readCandidateJSON 限制请求大小并拒绝拼接多个 JSON 对象。
func readCandidateJSON(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	var input map[string]any
	if err := decoder.Decode(&input); err != nil {
		return nil, errors.New("简历内容格式没认出来，请检查后再试")
	}
	if input == nil {
		return nil, errors.New("简历内容还是空的")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("这次请只提交一份简历内容")
	}
	return input, nil
}

// candidateHTTPKey 优先使用请求头幂等键，限制大小并隔离备注操作人。
func candidateHTTPKey(r *http.Request, input map[string]any) (string, error) {
	key := firstNonEmpty(strings.TrimSpace(r.Header.Get("Idempotency-Key")), candidateString(input, "idempotency_key"))
	if len(key) > 160 {
		return "", errors.New("请求编号太长了，请刷新后重试")
	}
	return key, nil
}

// writeCandidateError 把业务冲突与权限缺失映射到稳定 HTTP 状态。
func writeCandidateError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrCandidateDuplicate), errors.Is(err, ErrCandidateVersion), errors.Is(err, ErrCandidateRequest):
		status = http.StatusConflict
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "这次没保存成功，请稍后再试"
	}
	writeError(w, status, message)
}

// Save 保存新入口的候选人或用户明确选择的字段修改，姓名和手机号先经过校验。
func (s *CandidateService) Save(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	store, ok := s.store.(CandidateLibraryStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "简历维护入口还没准备好")
		return
	}
	input, err := readCandidateJSON(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := ""
	version := int64(0)
	if r.Method == http.MethodPatch {
		id = strings.TrimPrefix(r.URL.Path, "/api/candidates/")
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "候选人编号没认出来")
			return
		}
		raw, ok := input["row_version"].(float64)
		if !ok || raw < 1 || raw != float64(int64(raw)) {
			writeError(w, http.StatusBadRequest, "请先刷新简历，再提交修改")
			return
		}
		version = int64(raw)
	}
	fields, err := candidateMutationFields(input, id == "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := candidateHTTPKey(r, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if id != "" {
		current, err := s.store.GetPositionCandidate(scope.TenantID, id, "", scope.UserEmail, scope.IsAdmin)
		if err != nil {
			writeCandidateError(w, err)
			return
		}
		min, max := current.ExpectedSalaryMin, current.ExpectedSalaryMax
		for field, target := range map[string]**float64{"expected_salary_min": &min, "expected_salary_max": &max} {
			if raw, ok := fields[field]; ok {
				*target = nil
				if number, ok := raw.(float64); ok {
					*target = &number
				}
			}
		}
		if min != nil && max != nil && *min > *max {
			writeError(w, http.StatusBadRequest, "最低薪资不能高于最高薪资")
			return
		}
	}
	item, err := store.SaveLibraryCandidate(scope, id, fields, version, key)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidate": publicPositionCandidate(item)})
}

// Duplicates 读取精确或可能重复项，隐藏记录只返回匿名提示。
func (s *CandidateService) Duplicates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	store, ok := s.store.(CandidateLibraryStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "查重入口还没准备好")
		return
	}
	input, err := readCandidateJSON(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := CandidateDuplicateQuery{Phone: candidateString(input, "phone"), Name: candidateString(input, "candidate_name"), PlatformID: candidateString(input, "platform_id"), PlatformCandidateID: candidateString(input, "platform_candidate_id"), Keyword: candidateString(input, "keyword"), ExcludeID: candidateString(input, "exclude_id")}
	if query.PlatformID == "liepin_hunter" {
		query.PlatformID = "hliepin"
	}
	if len(query.Name) > 200 || len(query.Keyword) > 500 || len(query.PlatformCandidateID) > 2048 {
		writeError(w, http.StatusBadRequest, "查重内容太长了，请减少关键词")
		return
	}
	matches, hidden, err := store.FindCandidateDuplicates(scope, query)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	public := []map[string]any{}
	for _, match := range matches {
		public = append(public, map[string]any{"candidate": publicPositionCandidate(match.Candidate), "match_type": match.MatchType, "reasons": match.Reasons})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matches": public, "hidden_match": hidden})
}

// Recommendations 读取全部人工关联并记录明确的推荐或阶段推进，不启动岗位任务。
func (s *CandidateService) Recommendations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	store, ok := s.store.(CandidateLibraryStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "岗位推荐入口还没准备好")
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/recommendations")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "候选人编号没认出来")
		return
	}
	if r.Method == http.MethodGet {
		items, err := store.ListCandidateRecommendations(scope, id)
		if err != nil {
			writeCandidateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendations": safeSlice(items)})
		return
	}
	input, err := readCandidateJSON(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	positionID := candidateString(input, "position_id")
	stage := firstNonEmpty(candidateString(input, "stage"), "draft")
	content := candidateString(input, "content")
	if positionID == "" || !validRecommendationStage(stage) || len([]rune(content)) > 2000 {
		writeError(w, http.StatusBadRequest, "请选择岗位和推进阶段，备注控制在2000字内")
		return
	}
	if s.positionStore == nil {
		writeError(w, http.StatusServiceUnavailable, "岗位列表暂时没准备好")
		return
	}
	positions, err := s.positionStore.ListPositions(scope.TenantID, scope.UserEmail, scope.IsAdmin)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	var position Position
	for _, item := range positions {
		if item.ID == positionID {
			position = item
			break
		}
	}
	if position.ID == "" {
		writeError(w, http.StatusNotFound, "这个岗位暂时无法访问")
		return
	}
	key, err := candidateHTTPKey(r, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.SaveCandidateRecommendation(scope, id, position, stage, content, key)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendation": item})
}

// validCandidateDate 防止筛选日期错误进入数据库后变为服务器错误。
func validCandidateDate(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// candidateNoteRequestIdentity 把备注请求键绑定作者，避免多个成员键碰撞。
func candidateNoteRequestIdentity(metadata map[string]any) string {
	key := stringFromMap(metadata, "idempotency_key")
	if key == "" {
		return ""
	}
	return stringFromMap(metadata, "author_email") + ":" + key
}
