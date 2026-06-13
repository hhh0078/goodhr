// Package localdb 负责管理本地任务、日志和候选人数据。
package localdb

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Task 表示本地任务记录。
type Task struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	PlatformID        string         `json:"platform_id"`
	PlatformAccountID string         `json:"platform_account_id"`
	PositionID        string         `json:"position_id"`
	Mode              string         `json:"mode"`
	MatchLimit        int            `json:"match_limit"`
	Status            string         `json:"status"`
	ScannedCount      int            `json:"scanned_count"`
	GreetedCount      int            `json:"greeted_count"`
	SkippedCount      int            `json:"skipped_count"`
	FailedCount       int            `json:"failed_count"`
	EnableSound       bool           `json:"enable_sound"`
	EnableThinking    bool           `json:"enable_thinking"`
	PositionSnapshot  map[string]any `json:"position_snapshot"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

// Log 表示本地任务日志。
type Log struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"task_id"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// CandidateFilter 表示本地候选人筛选条件。
type CandidateFilter struct {
	TaskID     string
	PositionID string
	Keyword    string
	Page       int
	PageSize   int
}

// CreateTask 创建本地任务。
// payload 为任务参数，返回新建任务。
func (db *DB) CreateTask(payload map[string]any) (Task, error) {
	now := nowISO()
	task := Task{
		ID:                stringOr(payload["id"], uuid.NewString()),
		Name:              stringOr(payload["name"], ""),
		PlatformID:        stringOr(payload["platform_id"], "boss"),
		PlatformAccountID: stringOr(payload["platform_account_id"], ""),
		PositionID:        stringOr(payload["position_id"], ""),
		Mode:              stringOr(payload["mode"], "ai"),
		MatchLimit:        maxInt(0, intValue(payload["match_limit"])),
		Status:            "pending",
		EnableSound:       boolValue(payload["enable_sound"]),
		EnableThinking:    boolValue(payload["enable_thinking"]),
		PositionSnapshot:  mapValue(payload["position_snapshot"]),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	positionJSON, err := json.Marshal(task.PositionSnapshot)
	if err != nil {
		return Task{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
INSERT INTO local_tasks (
    id, name, platform_id, platform_account_id, position_id, mode, match_limit,
    status, enable_sound, enable_thinking, position_snapshot, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.Name, task.PlatformID, task.PlatformAccountID, task.PositionID, task.Mode,
		task.MatchLimit, task.Status, boolInt(task.EnableSound), boolInt(task.EnableThinking), string(positionJSON), task.CreatedAt, task.UpdatedAt,
	)
	if err != nil {
		return Task{}, fmt.Errorf("创建本地任务失败：%w", err)
	}
	return task, nil
}

// UpsertTaskSnapshot 保存云端任务在本地运行所需的轻量快照。
// payload 为云端任务字段，返回本地任务记录；已有任务只更新基础字段，不清空运行日志。
func (db *DB) UpsertTaskSnapshot(payload map[string]any) (Task, error) {
	taskID := strings.TrimSpace(stringOr(payload["id"], ""))
	if taskID == "" {
		return Task{}, fmt.Errorf("任务 ID 不能为空")
	}
	if existing, err := db.GetTask(taskID); err == nil {
		updated := map[string]any{
			"name":                stringOr(payload["name"], existing.Name),
			"platform_id":         stringOr(payload["platform_id"], existing.PlatformID),
			"platform_account_id": stringOr(payload["platform_account_id"], existing.PlatformAccountID),
			"position_id":         stringOr(payload["position_id"], existing.PositionID),
			"mode":                stringOr(payload["mode"], existing.Mode),
			"match_limit":         intValueOr(payload["match_limit"], existing.MatchLimit),
			"enable_sound":        boolValueOr(payload["enable_sound"], existing.EnableSound),
			"enable_thinking":     boolValueOr(payload["enable_thinking"], existing.EnableThinking),
			"position_snapshot":   mapValueOr(payload["position_snapshot"], existing.PositionSnapshot),
		}
		return db.UpdateTask(taskID, updated)
	}
	return db.CreateTask(payload)
}

// ListTasks 读取本地任务列表。
// 返回值按创建时间倒序排列。
func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(`SELECT id, name, platform_id, platform_account_id, position_id, mode, match_limit, status, scanned_count, greeted_count, skipped_count, failed_count, enable_sound, enable_thinking, position_snapshot, created_at, updated_at FROM local_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取本地任务失败：%w", err)
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

// GetTask 读取单个本地任务。
// taskID 为任务 ID。
func (db *DB) GetTask(taskID string) (Task, error) {
	row := db.conn.QueryRow(`SELECT id, name, platform_id, platform_account_id, position_id, mode, match_limit, status, scanned_count, greeted_count, skipped_count, failed_count, enable_sound, enable_thinking, position_snapshot, created_at, updated_at FROM local_tasks WHERE id=?`, taskID)
	task, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return task, err
}

// UpdateTask 更新本地任务基础信息。
// taskID 为任务 ID，payload 为更新参数。
func (db *DB) UpdateTask(taskID string, payload map[string]any) (Task, error) {
	existing, err := db.GetTask(taskID)
	if err != nil {
		return Task{}, err
	}
	updated := existing
	updated.Name = stringOr(payload["name"], existing.Name)
	updated.PlatformID = stringOr(payload["platform_id"], existing.PlatformID)
	updated.PlatformAccountID = stringOr(payload["platform_account_id"], existing.PlatformAccountID)
	updated.PositionID = stringOr(payload["position_id"], existing.PositionID)
	updated.Mode = stringOr(payload["mode"], existing.Mode)
	if _, ok := payload["match_limit"]; ok {
		updated.MatchLimit = maxInt(0, intValue(payload["match_limit"]))
	}
	if _, ok := payload["enable_sound"]; ok {
		updated.EnableSound = boolValue(payload["enable_sound"])
	}
	if _, ok := payload["enable_thinking"]; ok {
		updated.EnableThinking = boolValue(payload["enable_thinking"])
	}
	if _, ok := payload["position_snapshot"]; ok {
		updated.PositionSnapshot = mapValue(payload["position_snapshot"])
	}
	updated.UpdatedAt = nowISO()
	positionJSON, err := json.Marshal(updated.PositionSnapshot)
	if err != nil {
		return Task{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
UPDATE local_tasks
SET name=?, platform_id=?, platform_account_id=?, position_id=?, mode=?,
    match_limit=?, enable_sound=?, enable_thinking=?, position_snapshot=?, updated_at=?
WHERE id=?`,
		updated.Name, updated.PlatformID, updated.PlatformAccountID, updated.PositionID,
		updated.Mode, updated.MatchLimit, boolInt(updated.EnableSound), boolInt(updated.EnableThinking), string(positionJSON),
		updated.UpdatedAt, taskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("更新本地任务失败：%w", err)
	}
	return db.GetTask(taskID)
}

// UpdateTaskStatus 更新任务状态。
// taskID 为任务 ID，status 为新状态。
func (db *DB) UpdateTaskStatus(taskID string, status string) (Task, error) {
	if status == "" {
		return Task{}, fmt.Errorf("任务状态不能为空")
	}
	result, err := db.conn.Exec(`UPDATE local_tasks SET status=?, updated_at=? WHERE id=?`, status, nowISO(), taskID)
	if err != nil {
		return Task{}, fmt.Errorf("更新任务状态失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return db.GetTask(taskID)
}

// IncrementTaskCounts 累加任务统计数量。
// taskID 为任务 ID，scanned/greeted/skipped/failed 为增量。
func (db *DB) IncrementTaskCounts(taskID string, scanned int, greeted int, skipped int, failed int) (Task, error) {
	result, err := db.conn.Exec(`
UPDATE local_tasks
SET scanned_count=scanned_count+?,
    greeted_count=greeted_count+?,
    skipped_count=skipped_count+?,
    failed_count=failed_count+?,
    updated_at=?
WHERE id=?`,
		maxInt(0, scanned), maxInt(0, greeted), maxInt(0, skipped), maxInt(0, failed), nowISO(), taskID,
	)
	if err != nil {
		return Task{}, fmt.Errorf("更新任务统计失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Task{}, fmt.Errorf("本地任务不存在")
	}
	return db.GetTask(taskID)
}

// DeleteTask 删除本地任务及关联数据。
// taskID 为任务 ID。
func (db *DB) DeleteTask(taskID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_tasks WHERE id=?`, taskID)
	if err != nil {
		return fmt.Errorf("删除本地任务失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("本地任务不存在")
	}
	return nil
}

// AddTaskLog 写入本地任务日志。
// taskID 为任务 ID，level 为日志级别，message 为日志内容。
func (db *DB) AddTaskLog(taskID string, level string, message string) (Log, error) {
	if _, err := db.GetTask(taskID); err != nil {
		return Log{}, err
	}
	if level == "" {
		level = "info"
	}
	now := nowISO()
	result, err := db.conn.Exec(
		`INSERT INTO local_task_logs(task_id, level, message, created_at) VALUES(?, ?, ?, ?)`,
		taskID, level, message, now,
	)
	if err != nil {
		return Log{}, fmt.Errorf("写入任务日志失败：%w", err)
	}
	id, _ := result.LastInsertId()
	return Log{ID: id, TaskID: taskID, Level: level, Message: message, CreatedAt: now}, nil
}

// ListTaskLogs 读取本地任务日志。
// taskID 为任务 ID，limit 为最大返回数量。
func (db *DB) ListTaskLogs(taskID string, limit int) ([]Log, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.conn.Query(
		`SELECT id, task_id, level, message, created_at FROM local_task_logs WHERE task_id=? ORDER BY id DESC LIMIT ?`,
		taskID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("读取任务日志失败：%w", err)
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var item Log
		if err := rows.Scan(&item.ID, &item.TaskID, &item.Level, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		logs = append([]Log{item}, logs...)
	}
	return logs, rows.Err()
}

// ClearTaskLogs 清空本地任务日志。
// taskID 为任务 ID。
func (db *DB) ClearTaskLogs(taskID string) error {
	if _, err := db.GetTask(taskID); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`DELETE FROM local_task_logs WHERE task_id=?`, taskID); err != nil {
		return fmt.Errorf("清空任务日志失败：%w", err)
	}
	return nil
}

// SaveCandidate 保存/更新本地候选人（全部结构化字段）。
// taskID 为任务 ID，candidate 为候选人数据。
func (db *DB) SaveCandidate(taskID string, candidate map[string]any) (result map[string]any, resultErr error) {
	defer func() {
		if r := recover(); r != nil {
			resultErr = fmt.Errorf("保存候选人时发生内部错误：%v", r)
			result = nil
		}
	}()
	if _, err := db.GetTask(taskID); err != nil {
		return nil, err
	}
	now := nowISO()
	candidateID := stringOr(candidate["id"], uuid.NewString())
	candidate["id"] = candidateID
	candidate["task_id"] = taskID
	candidateName := stringOr(candidate["candidate_name"], stringOr(candidate["name"], ""))
	status := stringOr(candidate["status"], "")
	_, err := db.conn.Exec(`
INSERT INTO local_candidates(
    id, task_id, candidate_name, status,
    birth_ym, phone, email, work_region, work_years,
    expected_salary_min, expected_salary_max,
    personal_description, work_status, expected_position, online_status, education_level,
    basic_info, raw_text, filter_text,
    work_experiences, educations, certificates, honors, project_experiences, colleague_communications,
    resume_attachment_url, resume_attachment_extracted_text,
    ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score,
    ext, first_seen_at, detail_fetched_at, greeted_at,
    created_at, updated_at
) VALUES(?, ?, ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?,
    ?, ?, ?, ?, ?,
    ?, ?, ?,
    ?, ?, ?, ?, ?, ?,
    ?, ?,
    ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?,
    ?, ?)
ON CONFLICT(task_id, id) DO UPDATE SET
    candidate_name=excluded.candidate_name,
    status=excluded.status,
    birth_ym=excluded.birth_ym,
    phone=excluded.phone,
    email=excluded.email,
    work_region=excluded.work_region,
    work_years=excluded.work_years,
    expected_salary_min=excluded.expected_salary_min,
    expected_salary_max=excluded.expected_salary_max,
    personal_description=excluded.personal_description,
    work_status=excluded.work_status,
    expected_position=excluded.expected_position,
    online_status=excluded.online_status,
    education_level=excluded.education_level,
    basic_info=excluded.basic_info,
    raw_text=excluded.raw_text,
    filter_text=excluded.filter_text,
    work_experiences=excluded.work_experiences,
    educations=excluded.educations,
    certificates=excluded.certificates,
    honors=excluded.honors,
    project_experiences=excluded.project_experiences,
    colleague_communications=excluded.colleague_communications,
    resume_attachment_url=excluded.resume_attachment_url,
    resume_attachment_extracted_text=excluded.resume_attachment_extracted_text,
    ai_detail_reason=excluded.ai_detail_reason,
    ai_detail_score=excluded.ai_detail_score,
    ai_greet_reason=excluded.ai_greet_reason,
    ai_greet_score=excluded.ai_greet_score,
    ai_review_reason=excluded.ai_review_reason,
    ai_review_score=excluded.ai_review_score,
    ext=excluded.ext,
    first_seen_at=excluded.first_seen_at,
    detail_fetched_at=excluded.detail_fetched_at,
    greeted_at=excluded.greeted_at,
    candidate_name=excluded.candidate_name,
    updated_at=excluded.updated_at`,
		candidateID, taskID, candidateName, status,
		strVal(candidate, "birth_ym"), strVal(candidate, "phone"), strVal(candidate, "email"),
		strVal(candidate, "work_region"), strVal(candidate, "work_years"),
		intOrNil(candidate, "expected_salary_min"), intOrNil(candidate, "expected_salary_max"),
		strVal(candidate, "personal_description"), strVal(candidate, "work_status"),
		strVal(candidate, "expected_position"), strVal(candidate, "online_status"),
		strVal(candidate, "education_level"),
		strVal(candidate, "basic_info"), strVal(candidate, "raw_text"), strVal(candidate, "filter_text"),
		jsonOrArray(candidate, "work_experiences"), jsonOrArray(candidate, "educations"),
		jsonOrArray(candidate, "certificates"), jsonOrArray(candidate, "honors"),
		jsonOrArray(candidate, "project_experiences"), jsonOrArray(candidate, "colleague_communications"),
		strVal(candidate, "resume_attachment_url"), strVal(candidate, "resume_attachment_extracted_text"),
		strVal(candidate, "ai_detail_reason"), floatOrNil(candidate, "ai_detail_score"),
		strVal(candidate, "ai_greet_reason"), floatOrNil(candidate, "ai_greet_score"),
		strVal(candidate, "ai_review_reason"), floatOrNil(candidate, "ai_review_score"),
		jsonOrMap(candidate, "ext"), strVal(candidate, "first_seen_at"),
		strVal(candidate, "detail_fetched_at"), strVal(candidate, "greeted_at"),
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("保存候选人失败：%w", err)
	}
	return candidate, nil
}

// ListCandidates 读取本地候选人列表（结构化字段）。
// taskID 为任务 ID，返回候选人列表。
func (db *DB) ListCandidates(taskID string) ([]map[string]any, error) {
	rows, err := db.conn.Query(`SELECT candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE task_id=? ORDER BY updated_at DESC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("读取候选人失败：%w", err)
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var cName, cStatus, birthYM, phone, email, workRegion, workYears string
		var salMin, salMax *int
		var personalDesc, workStatus, expectedPos, onlineStatus, eduLevel string
		var basicInfo, rawText, filterText string
		var workExps, edus, certs, honors, projExps, comms string
		var resumeURL, resumeText string
		var aiDetailReason, aiGreetReason, aiReviewReason string
		var aiDetailScore, aiGreetScore, aiReviewScore *float64
		var ext, firstSeen, detailFetched, greeted, createdAt, updatedAt string
		err := rows.Scan(&cName, &cStatus, &birthYM, &phone, &email, &workRegion, &workYears,
			&salMin, &salMax, &personalDesc, &workStatus, &expectedPos, &onlineStatus, &eduLevel,
			&basicInfo, &rawText, &filterText,
			&workExps, &edus, &certs, &honors, &projExps, &comms,
			&resumeURL, &resumeText,
			&aiDetailReason, &aiDetailScore, &aiGreetReason, &aiGreetScore, &aiReviewReason, &aiReviewScore,
			&ext, &firstSeen, &detailFetched, &greeted, &createdAt, &updatedAt)
		if err != nil {
			return nil, err
		}
		item := candidateRowToMap(cName, cStatus, birthYM, phone, email, workRegion, workYears,
			salMin, salMax, personalDesc, workStatus, expectedPos, onlineStatus, eduLevel,
			basicInfo, rawText, filterText,
			workExps, edus, certs, honors, projExps, comms,
			resumeURL, resumeText,
			aiDetailReason, aiDetailScore, aiGreetReason, aiGreetScore, aiReviewReason, aiReviewScore,
			ext, firstSeen, detailFetched, greeted, createdAt, updatedAt)
		item["task_id"] = taskID
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListCandidatesFiltered 按条件读取本地候选人分页列表（结构化字段）。
// filter 为筛选条件，返回候选人列表、总数和错误信息。
func (db *DB) ListCandidatesFiltered(filter CandidateFilter) ([]map[string]any, int, error) {
	rows, err := db.conn.Query(`SELECT task_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates ORDER BY updated_at DESC`)
	if err != nil {
		return nil, 0, fmt.Errorf("读取候选人失败：%w", err)
	}
	defer rows.Close()
	all := []map[string]any{}
	for rows.Next() {
		var cName, cStatus, birthYM, phone, email, workRegion, workYears string
		var salMin, salMax *int
		var personalDesc, workStatus, expectedPos, onlineStatus, eduLevel string
		var basicInfo, rawText, filterText string
		var workExps, edus, certs, honors, projExps, comms string
		var resumeURL, resumeText string
		var aiDetailReason, aiGreetReason, aiReviewReason string
		var aiDetailScore, aiGreetScore, aiReviewScore *float64
		var ext, firstSeen, detailFetched, greeted, createdAt, updatedAt string
		var rowTaskID string
		err := rows.Scan(&rowTaskID, &cName, &cStatus, &birthYM, &phone, &email, &workRegion, &workYears,
			&salMin, &salMax, &personalDesc, &workStatus, &expectedPos, &onlineStatus, &eduLevel,
			&basicInfo, &rawText, &filterText,
			&workExps, &edus, &certs, &honors, &projExps, &comms,
			&resumeURL, &resumeText,
			&aiDetailReason, &aiDetailScore, &aiGreetReason, &aiGreetScore, &aiReviewReason, &aiReviewScore,
			&ext, &firstSeen, &detailFetched, &greeted, &createdAt, &updatedAt)
		if err != nil {
			return nil, 0, err
		}
		item := candidateRowToMap(cName, cStatus, birthYM, phone, email, workRegion, workYears,
			salMin, salMax, personalDesc, workStatus, expectedPos, onlineStatus, eduLevel,
			basicInfo, rawText, filterText,
			workExps, edus, certs, honors, projExps, comms,
			resumeURL, resumeText,
			aiDetailReason, aiDetailScore, aiGreetReason, aiGreetScore, aiReviewReason, aiReviewScore,
			ext, firstSeen, detailFetched, greeted, createdAt, updatedAt)
		item["task_id"] = rowTaskID
		if matchCandidateFilter(item, filter) {
			all = append(all, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	total := len(all)
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []map[string]any{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

// GetCandidate 读取本地候选人详情（结构化字段）。
// candidateID 为候选人 ID，taskID 为空时会在全部任务中查找。
func (db *DB) GetCandidate(candidateID string, taskID string) (map[string]any, error) {
	if strings.TrimSpace(candidateID) == "" {
		return nil, fmt.Errorf("候选人 ID 不能为空")
	}
	var row *sql.Row
	if strings.TrimSpace(taskID) != "" {
		row = db.conn.QueryRow(`SELECT task_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE task_id=? AND id=?`, taskID, candidateID)
	} else {
		row = db.conn.QueryRow(`SELECT task_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE id=? ORDER BY updated_at DESC LIMIT 1`, candidateID)
	}
	var rowTaskID, cName, cStatus, birthYM, phone, email, workRegion, workYears string
	var salMin, salMax *int
	var personalDesc, workStatus, expectedPos, onlineStatus, eduLevel string
	var basicInfo, rawText, filterText string
	var workExps, edus, certs, honors, projExps, comms string
	var resumeURL, resumeText string
	var aiDetailReason, aiGreetReason, aiReviewReason string
	var aiDetailScore, aiGreetScore, aiReviewScore *float64
	var ext, firstSeen, detailFetched, greeted, createdAt, updatedAt string
	err := row.Scan(&rowTaskID, &cName, &cStatus, &birthYM, &phone, &email, &workRegion, &workYears,
		&salMin, &salMax, &personalDesc, &workStatus, &expectedPos, &onlineStatus, &eduLevel,
		&basicInfo, &rawText, &filterText,
		&workExps, &edus, &certs, &honors, &projExps, &comms,
		&resumeURL, &resumeText,
		&aiDetailReason, &aiDetailScore, &aiGreetReason, &aiGreetScore, &aiReviewReason, &aiReviewScore,
		&ext, &firstSeen, &detailFetched, &greeted, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("候选人不存在")
		}
		return nil, fmt.Errorf("读取候选人详情失败：%w", err)
	}
	item := candidateRowToMap(cName, cStatus, birthYM, phone, email, workRegion, workYears,
		salMin, salMax, personalDesc, workStatus, expectedPos, onlineStatus, eduLevel,
		basicInfo, rawText, filterText,
		workExps, edus, certs, honors, projExps, comms,
		resumeURL, resumeText,
		aiDetailReason, aiDetailScore, aiGreetReason, aiGreetScore, aiReviewReason, aiReviewScore,
		ext, firstSeen, detailFetched, greeted, createdAt, updatedAt)
	item["task_id"] = rowTaskID
	return item, nil
}

// ClearCandidates 清空本地候选人数据。
// 返回删除的候选人数量和错误信息。
func (db *DB) ClearCandidates() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM local_candidates`)
	if err != nil {
		return 0, fmt.Errorf("清空候选人失败：%w", err)
	}
	deleted, _ := result.RowsAffected()
	return deleted, nil
}

// DeleteCandidate 删除本地任务候选人。
// taskID 为任务 ID，candidateID 为候选人 ID。
func (db *DB) DeleteCandidate(taskID string, candidateID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_candidates WHERE task_id=? AND id=?`, taskID, candidateID)
	if err != nil {
		return fmt.Errorf("删除候选人失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("候选人不存在")
	}
	return nil
}

// matchCandidateFilter 判断候选人是否满足筛选条件。
// item 为候选人数据，filter 为筛选条件。
func matchCandidateFilter(item map[string]any, filter CandidateFilter) bool {
	if filter.TaskID != "" && stringOr(item["task_id"], "") != filter.TaskID {
		return false
	}
	if filter.PositionID != "" && stringOr(item["position_id"], "") != filter.PositionID {
		return false
	}
	keyword := strings.TrimSpace(strings.ToLower(filter.Keyword))
	if keyword == "" {
		return true
	}
	raw, _ := json.Marshal(item)
	return strings.Contains(strings.ToLower(string(raw)), keyword)
}

// scanTask 从数据库行扫描任务。
// scanner 为 QueryRow 或 Rows。
func scanTask(scanner interface{ Scan(dest ...any) error }) (Task, error) {
	var task Task
	var enableSound int
	var positionJSON string
	var enableThinking int
	err := scanner.Scan(
		&task.ID, &task.Name, &task.PlatformID, &task.PlatformAccountID, &task.PositionID, &task.Mode,
		&task.MatchLimit, &task.Status, &task.ScannedCount, &task.GreetedCount, &task.SkippedCount,
		&task.FailedCount, &enableSound, &enableThinking, &positionJSON, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	task.EnableSound = enableSound == 1
	task.EnableThinking = enableThinking == 1
	task.PositionSnapshot = map[string]any{}
	_ = json.Unmarshal([]byte(positionJSON), &task.PositionSnapshot)
	return task, nil
}

// nowISO 返回当前 UTC 时间字符串。
// 返回值用于数据库 created_at 和 updated_at。
func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// stringOr 将值转换为字符串，空值使用默认值。
// value 为原始值，fallback 为默认值。
func stringOr(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}

// intValue 将值转换为整数。
// value 为原始值。
func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		v, _ := typed.Int64()
		return int(v)
	default:
		return 0
	}
}

// intValueOr 将值转换为整数，空值使用默认值。
// value 为原始值，fallback 为默认值。
func intValueOr(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	converted := intValue(value)
	if converted == 0 {
		return fallback
	}
	return converted
}

// boolValue 将值转换为布尔值。
// value 为原始值。
func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	default:
		return false
	}
}

// boolValueOr 将值转换为布尔值，空值使用默认值。
// value 为原始值，fallback 为默认值。
func boolValueOr(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return boolValue(value)
}

// mapValue 将值转换为 map。
// value 为原始值。
func mapValue(value any) map[string]any {
	if item, ok := value.(map[string]any); ok && item != nil {
		return item
	}
	return map[string]any{}
}

// mapValueOr 将值转换为 map，空值使用默认值。
// value 为原始值，fallback 为默认 map。
func mapValueOr(value any, fallback map[string]any) map[string]any {
	if value == nil {
		return fallback
	}
	converted := mapValue(value)
	if len(converted) == 0 {
		return fallback
	}
	return converted
}

// boolInt 将布尔值转换为 SQLite 整数。
// value 为布尔值。
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// maxInt 返回两个整数中的较大值。
// a 和 b 为参与比较的整数。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// candidateRowToMap 将 SQLite 行数据转换为 map，供 API 返回。
func candidateRowToMap(cName, cStatus, birthYM, phone, email, workRegion, workYears string,
	salMin, salMax *int, personalDesc, workStatus, expectedPos, onlineStatus, eduLevel string,
	basicInfo, rawText, filterText string,
	workExps, edus, certs, honors, projExps, comms string,
	resumeURL, resumeText string,
	aiDetailReason string, aiDetailScore *float64, aiGreetReason string, aiGreetScore *float64,
	aiReviewReason string, aiReviewScore *float64,
	ext, firstSeen, detailFetched, greeted, createdAt, updatedAt string) map[string]any {
	item := map[string]any{
		"candidate_name":                   cName,
		"status":                           cStatus,
		"birth_ym":                         birthYM,
		"phone":                            phone,
		"email":                            email,
		"work_region":                      workRegion,
		"work_years":                       workYears,
		"expected_salary_min":              salMin,
		"expected_salary_max":              salMax,
		"personal_description":             personalDesc,
		"work_status":                      workStatus,
		"expected_position":                expectedPos,
		"online_status":                    onlineStatus,
		"education_level":                  eduLevel,
		"basic_info":                       basicInfo,
		"raw_text":                         rawText,
		"filter_text":                      filterText,
		"resume_attachment_url":            resumeURL,
		"resume_attachment_extracted_text": resumeText,
		"ai_detail_reason":                 aiDetailReason,
		"ai_detail_score":                  aiDetailScore,
		"ai_greet_reason":                  aiGreetReason,
		"ai_greet_score":                   aiGreetScore,
		"ai_review_reason":                 aiReviewReason,
		"ai_review_score":                  aiReviewScore,
		"ext":                              ext,
		"first_seen_at":                    firstSeen,
		"detail_fetched_at":                detailFetched,
		"greeted_at":                       greeted,
		"created_at":                       createdAt,
		"updated_at":                       updatedAt,
	}
	// JSON 字段解析为数组或字典
	if workExps != "" && workExps != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(workExps), &parsed) == nil {
			item["work_experiences"] = parsed
		}
	}
	if edus != "" && edus != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(edus), &parsed) == nil {
			item["educations"] = parsed
		}
	}
	if certs != "" && certs != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(certs), &parsed) == nil {
			item["certificates"] = parsed
		}
	}
	if honors != "" && honors != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(honors), &parsed) == nil {
			item["honors"] = parsed
		}
	}
	if projExps != "" && projExps != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(projExps), &parsed) == nil {
			item["project_experiences"] = parsed
		}
	}
	if comms != "" && comms != "[]" {
		var parsed []any
		if json.Unmarshal([]byte(comms), &parsed) == nil {
			item["colleague_communications"] = parsed
		}
	}
	if ext != "" && ext != "{}" {
		var parsed map[string]any
		if json.Unmarshal([]byte(ext), &parsed) == nil {
			item["ext"] = parsed
		}
	}
	return item
}

// strVal 从 map 中读取字符串值，不存在或非字符串返回空字符串。
func strVal(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return ""
}

// intOrNil 从 map 中读取 int 指针，不存在时返回 nil。
func intOrNil(m map[string]any, key string) *int {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case int:
		return &n
	case float64:
		i := int(n)
		return &i
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return nil
		}
		ii := int(i)
		return &ii
	default:
		return nil
	}
}

// floatOrNil 从 map 中读取 float64 指针，不存在时返回 nil。
func floatOrNil(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

// jsonOrArray 将 map 中的数组值序列化为 JSON 字符串，用于 SQLite 存储。
// 如果是字符串则原样返回，如果是数组则 JSON 序列化。
func jsonOrArray(m map[string]any, key string) string {
	if m == nil {
		return "[]"
	}
	v, ok := m[key]
	if !ok || v == nil {
		return "[]"
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return "[]"
		}
		return val
	case []any:
		b, err := json.Marshal(val)
		if err != nil {
			return "[]"
		}
		return string(b)
	default:
		return "[]"
	}
}

// jsonOrMap 将 map 中的子 map 序列化为 JSON 字符串，用于 SQLite 存储。
func jsonOrMap(m map[string]any, key string) string {
	if m == nil {
		return "{}"
	}
	v, ok := m[key]
	if !ok || v == nil {
		return "{}"
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return "{}"
		}
		return val
	case map[string]any:
		b, err := json.Marshal(val)
		if err != nil {
			return "{}"
		}
		return string(b)
	default:
		return "{}"
	}
}
