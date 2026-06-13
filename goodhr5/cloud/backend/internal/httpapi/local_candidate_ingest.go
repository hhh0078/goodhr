// 本文件负责接收本地程序回传的候选人 JSON，并写入云端简历库。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

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
	now := time.Now().UTC()
	profile, err := s.candidateStore.SaveCandidateProfile(CandidateProfileInput{
		UserEmail:           task.UserEmail,
		PlatformID:          firstNonEmpty(localCandidateString(payload, "platform_id"), task.PlatformID),
		PlatformCandidateID: localCandidateString(payload, "id"),
		CandidateName:       firstNonEmpty(localCandidateString(payload, "candidate_name"), localCandidateString(payload, "name")),
		BasicInfo:           firstNonEmpty(localCandidateString(payload, "basic_info"), localCandidateString(payload, "summary")),
		EducationLevel:      localCandidateString(payload, "education_level"),
		ExpectedPosition:    localCandidateString(payload, "expected_position"),
		OnlineStatus:        localCandidateString(payload, "online_status"),
		PersonalDescription: firstNonEmpty(localCandidateString(payload, "personal_description"), localCandidateString(payload, "description")),
		RawText:             firstNonEmpty(localCandidateString(payload, "raw_text"), localCandidateString(payload, "detail_text")),
		FilterText:          firstNonEmpty(localCandidateString(payload, "filter_text"), localCandidateString(payload, "raw_text")),
		ResumeText:          firstNonEmpty(localCandidateString(payload, "resume_text"), localCandidateString(payload, "detail_text")),
		Ext:                 map[string]any{"local_candidate_json": payload},
		FirstSeenAt:         &now,
	})
	if err != nil {
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
		writeError(w, http.StatusInternalServerError, "failed to save candidate engagement")
		return
	}
	s.saveLocalCandidateScoreEvents(task, profile.ID, engagement.ID, payload)
	_ = s.candidateStore.UpdateCandidateEngagementStatus(engagement.ID, localCandidateStatus(payload), localDetailFetchedAt(payload, now), localGreetedAt(payload, now))
	_ = s.store.IncrementTaskCounts(task.ID, 1, localCountIfStatus(payload, "greeted"), localCountIfSkipped(payload), localCountIfStatus(payload, "failed"))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"candidate":  publicTaskCandidate(profile),
		"engagement": engagement.ID,
	})
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
		{Type: "review_analysis", ScoreKey: "ai_review_score", ReasonKey: "ai_review_reason"},
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
			InputText:         firstNonEmpty(localCandidateString(payload, "filter_text"), localCandidateString(payload, "raw_text")),
			OutputText:        localCandidateString(payload, item.ReasonKey),
			Metadata:          map[string]any{"source": "local-agent-go"},
		})
	}
}

// localCandidateTaskID 从任务候选人路径中提取任务 ID。
// path 为请求路径。
func localCandidateTaskID(path string) string {
	text := strings.Trim(strings.TrimPrefix(path, "/api/tasks/"), "/")
	parts := strings.Split(text, "/")
	if len(parts) >= 2 && parts[1] == "candidates" {
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
	if localCandidateString(item, "detail_text") != "" || localCandidateString(item, "resume_text") != "" {
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
