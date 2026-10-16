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
	if path == "folders" || strings.HasPrefix(path, "folders/") {
		s.Folders(w, r)
		return
	}
	if s.handleBuiltinCandidate(w, r, path) {
		return
	}
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
	return CandidateLibraryScope{TenantID: tenant.ID, UserEmail: session.Email, AuthorName: session.Email, IsAdmin: admin, IsExtension: session.Kind == extensionSessionKind}, true
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
	case errors.Is(err, ErrCandidatePositionClosed):
		status = http.StatusConflict
	case errors.Is(err, ErrCandidateFolderInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, ErrCandidateFolderForbidden), errors.Is(err, ErrCandidateFolderScopeForbidden), errors.Is(err, ErrCandidateRecommendationForbidden):
		status = http.StatusForbidden
	case errors.Is(err, ErrCandidateFolderConflict):
		status = http.StatusConflict
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
	if hasBuiltinCandidateMutation(input) {
		writeError(w, http.StatusForbidden, "内置示例的身份不能用于创建或修改真实简历")
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
		if err := validateCandidateSalaryPatch(current, fields); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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

// validateCandidateSalaryPatch 将部分更新与已有薪资合并核对；省略保留原值，显式 nil 清空该侧。
func validateCandidateSalaryPatch(current PositionCandidate, fields map[string]any) error {
	min, max := current.ExpectedSalaryMin, current.ExpectedSalaryMax
	for field, target := range map[string]**float64{"expected_salary_min": &min, "expected_salary_max": &max} {
		if raw, present := fields[field]; present {
			*target = nil
			if number, ok := raw.(float64); ok {
				*target = &number
			}
		}
	}
	if min != nil && max != nil && *min > *max {
		return errors.New("最低薪资不能高于最高薪资")
	}
	return nil
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
	query, err := readCandidateDuplicateQuery(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	matches, hidden, err := store.FindCandidateDuplicates(scope, query)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	public := []map[string]any{}
	groupItems := map[string][]map[string]any{}
	for _, match := range matches {
		item := map[string]any{"candidate": publicPositionCandidate(match.Candidate), "match_type": match.MatchType, "reasons": match.Reasons}
		public = append(public, item)
		for _, key := range match.GroupKeys {
			groupItems[key] = append(groupItems[key], item)
		}
	}
	groups := []map[string]any{}
	for _, group := range [][2]string{{"strict", "综合条件相近"}, {"surname_gender_school", "学校线索相近"}, {"surname_gender_company", "公司线索相近"}} {
		if items := groupItems[group[0]]; len(items) > 0 {
			groups = append(groups, map[string]any{"key": group[0], "title": group[1], "summary": "线索相近，请人工确认是否同一位候选人", "matches": items})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matches": public, "hidden_match": hidden, "criteria": candidateDuplicateCriteria(query), "groups": groups})
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
	if !position.IsOpen {
		writeError(w, http.StatusConflict, "这个岗位已经关闭了，换个开启的岗位试试")
		return
	}
	key, err := candidateHTTPKey(r, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if raw, exists := input["occurred_at"]; exists && raw != nil {
		if _, ok := raw.(string); !ok {
			writeError(w, http.StatusBadRequest, "发生时间请填写带时区的日期")
			return
		}
	}
	occurredAt, err := parseCandidateOccurredAt(candidateString(input, "occurred_at"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.SaveCandidateRecommendation(scope, id, position, stage, content, key, occurredAt)
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

// parseCandidateOccurredAt 校验人工记录实际时间，允许历史时间和时钟偏差，拒绝明显超前的时间。
func parseCandidateOccurredAt(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil || parsed.Year() < 1900 || parsed.After(time.Now().Add(24*time.Hour)) {
		return time.Time{}, errors.New("发生时间请使用有效日期和时区，也别填到遥远的未来")
	}
	return parsed, nil
}

// candidateNoteRequestIdentity 把备注请求键绑定作者，避免多个成员键碰撞。
func candidateNoteRequestIdentity(metadata map[string]any) string {
	key := stringFromMap(metadata, "idempotency_key")
	if key == "" {
		return ""
	}
	return stringFromMap(metadata, "author_email") + ":" + key
}
