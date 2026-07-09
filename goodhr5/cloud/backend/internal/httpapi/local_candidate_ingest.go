// 本文件负责接收本地程序回传的候选人 JSON，并写入云端简历库。
package httpapi

import (
	"encoding/json"
	"errors"
	stdlog "log"
	"net/http"
	"strings"
	"time"
)

type addProcessedResumesRequest struct {
	Count int `json:"count"`
}

type syncTaskCountsRequest struct {
	ScannedCount int `json:"scanned_count"`
	GreetedCount int `json:"greeted_count"`
	SkippedCount int `json:"skipped_count"`
	FailedCount  int `json:"failed_count"`
}

// SaveLocalCandidate 保存本地程序回传的候选人结果。
// w 为响应对象，r 为请求对象；路径格式为 /api/tasks/{taskID}/candidates。
func (s *TaskService) SaveLocalCandidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.candidateStore == nil || s.tenantStore == nil {
		writeError(w, http.StatusInternalServerError, "candidate store is not ready")
		return
	}
	taskID := localCandidateTaskID(r.URL.Path)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	task, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	candidateName := localCandidateDisplayName(payload)
	s.writeCandidateIngestLog(task.ID, task.UserEmail, "info", "云端收到候选人入库请求："+candidateName)
	now := time.Now().UTC()
	profile, err := s.candidateStore.SaveCandidateProfile(CandidateProfileInput{
		UserEmail:           task.UserEmail,
		PlatformID:          firstNonEmpty(localCandidateString(payload, "platform_id"), task.PlatformID),
		PlatformCandidateID: localCandidateString(payload, "id"),
		CandidateName:       firstNonEmpty(localCandidateString(payload, "candidate_name"), localCandidateString(payload, "name")),
		BirthYM:             localCandidateString(payload, "birth_ym"),
		Phone:               localCandidateString(payload, "phone"),
		Email:               localCandidateString(payload, "email"),
		WorkRegion:          localCandidateString(payload, "work_region"),
		WorkYears:           localCandidateString(payload, "work_years"),
		ExpectedSalaryMin:   localCandidateIntPtr(payload, "expected_salary_min"),
		ExpectedSalaryMax:   localCandidateIntPtr(payload, "expected_salary_max"),
		BasicInfo:           firstNonEmpty(localCandidateString(payload, "basic_info"), localCandidateString(payload, "summary")),
		EducationLevel:      localCandidateString(payload, "education_level"),
		ExpectedPosition:    localCandidateString(payload, "expected_position"),
		OnlineStatus:        localCandidateString(payload, "online_status"),
		PersonalDescription: firstNonEmpty(localCandidateString(payload, "personal_description"), localCandidateString(payload, "description")),
		WorkStatus:          localCandidateString(payload, "work_status"),
		RawText:             localCandidateString(payload, "raw_text"),
		WorkExperiences:     localCandidateJSONList[CandidateWorkExperience](payload, "work_experiences"),
		Educations:          localCandidateJSONList[CandidateEducation](payload, "educations"),
		Certificates:        localCandidateJSONList[CandidateCertificate](payload, "certificates"),
		Honors:              localCandidateJSONList[CandidateHonor](payload, "honors"),
		ProjectExperiences:  localCandidateJSONList[CandidateProjectExperience](payload, "project_experiences"),
		Communications:      localCandidateJSONList[CandidateCommunication](payload, "colleague_communications"),
		AIDetailReason:      localCandidateString(payload, "ai_detail_reason"),
		AIDetailScore:       localCandidateFloatPtr(payload, "ai_detail_score"),
		AIGreetReason:       localCandidateString(payload, "ai_greet_reason"),
		AIGreetScore:        localCandidateFloatPtr(payload, "ai_greet_score"),
		FirstSeenAt:         &now,
	})
	if err != nil {
		s.writeCandidateIngestLog(task.ID, task.UserEmail, "warning", "云端候选人主体保存失败："+candidateName+"，原因："+err.Error())
		writeError(w, http.StatusInternalServerError, "failed to save candidate")
		return
	}
	engagement, err := s.candidateStore.UpsertCandidateEngagement(CandidateEngagement{
		CandidateID:       profile.ID,
		UserEmail:         task.UserEmail,
		TaskID:            task.ID,
		PositionID:        task.PositionID,
		PlatformAccountID: task.PlatformAccountID,
		PlatformID:        task.PlatformID,
		Status:            localCandidateStatus(payload),
		FirstSeenAt:       &now,
	})
	if err != nil {
		s.writeCandidateIngestLog(task.ID, task.UserEmail, "warning", "云端候选人任务关联保存失败："+candidateName+"，原因："+err.Error())
		writeError(w, http.StatusInternalServerError, "failed to save candidate engagement")
		return
	}
	s.saveLocalCandidateScoreEvents(task, profile.ID, engagement.ID, payload)
	_ = s.candidateStore.UpdateCandidateEngagementStatus(engagement.ID, localCandidateStatus(payload), localDetailFetchedAt(payload, now), localGreetedAt(payload, now))
	_ = s.store.IncrementTaskCounts(task.ID, 1, localCountIfStatus(payload, "greeted"), localCountIfSkipped(payload), localCountIfStatus(payload, "failed"))
	s.writeCandidateIngestLog(task.ID, task.UserEmail, "info", "云端候选人入库成功："+candidateName+"，状态："+localCandidateStatus(payload))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"candidate":  publicTaskCandidate(profile),
		"engagement": engagement.ID,
	})
}

// writeCandidateIngestLog 写入云端候选人入库链路日志。
// taskID 为任务 ID，userEmail 为任务所属用户，level 为日志级别，message 为日志内容。
func (s *TaskService) writeCandidateIngestLog(taskID string, userEmail string, level string, message string) {
	if err := s.taskLogs.WriteLog(taskID, userEmail, level, message); err != nil {
		stdlog.Printf("[云端候选人入库] 写任务日志失败 task=%s user=%s level=%s err=%v message=%s", taskID, userEmail, level, err, message)
	}
}

// AddProcessedResumes 累加本地程序本次去重后新增的已处理简历数量。
// w 为响应对象，r 为请求对象；路径格式为 /api/tasks/{taskID}/processed-resumes。
func (s *TaskService) AddProcessedResumes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	if s.dailyStats == nil {
		writeError(w, http.StatusInternalServerError, "daily stats store is not ready")
		return
	}
	taskID := taskSubresourceID(r.URL.Path, "processed-resumes")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id is required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	if _, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	var req addProcessedResumesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Count <= 0 {
		writeError(w, http.StatusBadRequest, "count must be greater than 0")
		return
	}
	if req.Count > 500 {
		writeError(w, http.StatusBadRequest, "count is too large")
		return
	}
	if err := s.dailyStats.IncrementProcessedResumes(req.Count); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update processed resumes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"count": req.Count,
	})
}

// SyncTaskCounts 接收本地程序同步的任务累计统计。
// w 为响应对象，r 为请求对象；路径格式为 /api/tasks/{taskID}/counts。
func (s *TaskService) SyncTaskCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	taskID := taskSubresourceID(r.URL.Path, "counts")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "task id required")
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	if _, err := s.store.TaskByID(tenantID, session.Email, taskID, isAdmin); errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task")
		return
	}
	var req syncTaskCountsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.ScannedCount < 0 || req.GreetedCount < 0 || req.SkippedCount < 0 || req.FailedCount < 0 {
		writeError(w, http.StatusBadRequest, "counts must greater or equal 0")
		return
	}
	if err := s.store.SyncTaskCounts(taskID, req.ScannedCount, req.GreetedCount, req.SkippedCount, req.FailedCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sync task counts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// saveLocalCandidateScoreEvents 保存本地程序产生的 AI 评分事件。
// task 为云端任务，candidateID 和 engagementID 为候选人关系 ID，payload 为本地候选人 JSON。
func (s *TaskService) saveLocalCandidateScoreEvents(task TaskRun, candidateID string, engagementID string, payload map[string]any) {
	events := []struct {
		Type      string
		ScoreKey  string
		ReasonKey string
	}{
		{Type: "detail_analysis", ScoreKey: "ai_detail_score", ReasonKey: "ai_detail_reason"},
		{Type: "greet_analysis", ScoreKey: "ai_greet_score", ReasonKey: "ai_greet_reason"},
	}
	for _, item := range events {
		score, ok := localCandidateFloat(payload, item.ScoreKey)
		reason := localCandidateString(payload, item.ReasonKey)
		if !ok && reason == "" {
			continue
		}
		_, _ = s.candidateStore.SaveCandidateEvent(CandidateEvent{
			CandidateID:       candidateID,
			EngagementID:      engagementID,
			TaskID:            task.ID,
			PositionID:        task.PositionID,
			PlatformAccountID: task.PlatformAccountID,
			PlatformID:        task.PlatformID,
			EventType:         item.Type,
			Score:             float64Ptr(score),
			Reason:            reason,
			InputText:         localCandidateString(payload, "raw_text"),
			OutputText:        localCandidateString(payload, item.ReasonKey),
			Metadata:          map[string]any{"source": "local-agent-go"},
		})
	}
}

// localCandidateTaskID 从任务候选人路径中提取任务 ID。
// path 为请求路径。
func localCandidateTaskID(path string) string {
	return taskSubresourceID(path, "candidates")
}

// taskSubresourceID 从任务子资源路径中提取任务 ID。
// path 为请求路径，resource 为子资源名称。
func taskSubresourceID(path string, resource string) string {
	text := strings.Trim(strings.TrimPrefix(path, "/api/tasks/"), "/")
	parts := strings.Split(text, "/")
	if len(parts) >= 2 && parts[1] == resource {
		return strings.TrimSpace(parts[0])
	}
	return ""
}

// localCandidateString 从候选人 JSON 中读取字符串。
// item 为候选人 JSON，key 为字段名。
func localCandidateString(item map[string]any, key string) string {
	if item == nil {
		return ""
	}
	if value, ok := item[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// localCandidateDisplayName 返回云端候选人入库日志里的展示名。
// item 为候选人 JSON，返回候选人姓名或平台候选人 ID。
func localCandidateDisplayName(item map[string]any) string {
	name := firstNonEmpty(firstNonEmpty(localCandidateString(item, "candidate_name"), localCandidateString(item, "name")), localCandidateString(item, "id"))
	if name == "" {
		return "未知候选人"
	}
	return name
}

// localCandidateFloat 从候选人 JSON 中读取分数。
// item 为候选人 JSON，key 为字段名。
func localCandidateFloat(item map[string]any, key string) (float64, bool) {
	if item == nil {
		return 0, false
	}
	switch value := item[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	default:
		return 0, false
	}
}

// localCandidateFloatPtr 从候选人 JSON 中读取可空分数。
// item 为候选人 JSON，key 为字段名。
func localCandidateFloatPtr(item map[string]any, key string) *float64 {
	value, ok := localCandidateFloat(item, key)
	if !ok {
		return nil
	}
	return &value
}

// localCandidateIntPtr 从候选人 JSON 中读取可空整数。
// item 为候选人 JSON，key 为字段名。
func localCandidateIntPtr(item map[string]any, key string) *int {
	if item == nil {
		return nil
	}
	switch value := item[key].(type) {
	case float64:
		parsed := int(value)
		return &parsed
	case int:
		return &value
	default:
		return nil
	}
}

// localCandidateJSONList 将 JSON 数组规范成指定结构数组。
// item 为候选人 JSON，key 为数组字段名。
func localCandidateJSONList[T any](item map[string]any, key string) []T {
	if item == nil || item[key] == nil {
		return []T{}
	}
	raw, err := json.Marshal(item[key])
	if err != nil {
		return []T{}
	}
	var result []T
	if err := json.Unmarshal(raw, &result); err != nil {
		return []T{}
	}
	return result
}

// localCandidateStatus 返回候选人触达状态。
// item 为候选人 JSON。
func localCandidateStatus(item map[string]any) string {
	status := localCandidateString(item, "status")
	if status == "" {
		return "scanned"
	}
	return status
}

// localDetailFetchedAt 根据候选人状态返回详情读取时间。
// item 为候选人 JSON，now 为当前时间。
func localDetailFetchedAt(item map[string]any, now time.Time) *time.Time {
	if localCandidateString(item, "detail_text") != "" || localCandidateString(item, "raw_text") != "" {
		return &now
	}
	return nil
}

// localGreetedAt 根据候选人状态返回打招呼时间。
// item 为候选人 JSON，now 为当前时间。
func localGreetedAt(item map[string]any, now time.Time) *time.Time {
	if localCandidateStatus(item) == "greeted" {
		return &now
	}
	return nil
}

// localCountIfStatus 判断候选人状态是否命中并返回计数。
// item 为候选人 JSON，status 为目标状态。
func localCountIfStatus(item map[string]any, status string) int {
	if localCandidateStatus(item) == status {
		return 1
	}
	return 0
}

// localCountIfSkipped 判断候选人是否为跳过状态。
// item 为候选人 JSON。
func localCountIfSkipped(item map[string]any) int {
	status := localCandidateStatus(item)
	if status == "skipped" || strings.Contains(status, "skip") {
		return 1
	}
	return 0
}
