// Package localdb 负责管理本地岗位运行、日志和候选人数据。
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

// Position 表示本地岗位运行记录。
type Position struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	PlatformID        string         `json:"platform_id"`
	PlatformAccountID string         `json:"platform_account_id"`
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

// Log 表示本地岗位运行日志。
type Log struct {
	ID         int64  `json:"id"`
	PositionID string `json:"position_id"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

// CandidateFilter 表示本地候选人筛选条件。
type CandidateFilter struct {
	PositionID string
	Keyword    string
	Page       int
	PageSize   int
}

// CreatePosition 创建本地岗位运行。
// payload 为岗位运行参数，返回新建岗位运行。
func (db *DB) CreatePosition(payload map[string]any) (Position, error) {
	now := nowISO()
	position := Position{
		ID:                stringOr(payload["id"], uuid.NewString()),
		Name:              stringOr(payload["name"], ""),
		PlatformID:        stringOr(payload["platform_id"], "boss"),
		PlatformAccountID: stringOr(payload["platform_account_id"], ""),
		Mode:              stringOr(payload["mode"], "ai"),
		MatchLimit:        maxInt(0, intValue(payload["match_limit"])),
		Status:            "pending",
		EnableSound:       boolValue(payload["enable_sound"]),
		EnableThinking:    boolValue(payload["enable_thinking"]),
		PositionSnapshot:  mapValue(payload["position_snapshot"]),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	positionJSON, err := json.Marshal(position.PositionSnapshot)
	if err != nil {
		return Position{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
INSERT INTO local_positions (
    id, name, platform_id, platform_account_id, mode, match_limit,
    status, enable_sound, enable_thinking, position_snapshot, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		position.ID, position.Name, position.PlatformID, position.PlatformAccountID, position.Mode,
		position.MatchLimit, position.Status, boolInt(position.EnableSound), boolInt(position.EnableThinking), string(positionJSON), position.CreatedAt, position.UpdatedAt,
	)
	if err != nil {
		return Position{}, fmt.Errorf("创建本地岗位运行失败：%w", err)
	}
	return position, nil
}

// UpsertPositionSnapshot 保存云端岗位运行在本地运行所需的轻量快照。
// payload 为云端岗位运行字段，返回本地岗位运行记录；已有岗位运行只更新基础字段，不清空运行日志。
func (db *DB) UpsertPositionSnapshot(payload map[string]any) (Position, error) {
	positionID := strings.TrimSpace(stringOr(payload["id"], ""))
	if positionID == "" {
		return Position{}, fmt.Errorf("岗位运行 ID 不能为空")
	}
	if existing, err := db.GetPosition(positionID); err == nil {
		updated := map[string]any{
			"name":                stringOr(payload["name"], existing.Name),
			"platform_id":         stringOr(payload["platform_id"], existing.PlatformID),
			"platform_account_id": stringOr(payload["platform_account_id"], existing.PlatformAccountID),
			"mode":                stringOr(payload["mode"], existing.Mode),
			"match_limit":         intValueOr(payload["match_limit"], existing.MatchLimit),
			"enable_sound":        boolValueOr(payload["enable_sound"], existing.EnableSound),
			"enable_thinking":     boolValueOr(payload["enable_thinking"], existing.EnableThinking),
			"position_snapshot":   mapValueOr(payload["position_snapshot"], existing.PositionSnapshot),
		}
		return db.UpdatePosition(positionID, updated)
	}
	return db.CreatePosition(payload)
}

// ListPositions 读取本地岗位运行列表。
// 返回值按创建时间倒序排列。
func (db *DB) ListPositions() ([]Position, error) {
	rows, err := db.conn.Query(`SELECT id, name, platform_id, platform_account_id, mode, match_limit, status, scanned_count, greeted_count, skipped_count, failed_count, enable_sound, enable_thinking, position_snapshot, created_at, updated_at FROM local_positions ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取本地岗位运行失败：%w", err)
	}
	defer rows.Close()
	positions := []Position{}
	for rows.Next() {
		position, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		positions = append(positions, position)
	}
	return positions, rows.Err()
}

// GetPosition 读取单个本地岗位运行。
// positionID 为岗位运行 ID。
func (db *DB) GetPosition(positionID string) (Position, error) {
	row := db.conn.QueryRow(`SELECT id, name, platform_id, platform_account_id, mode, match_limit, status, scanned_count, greeted_count, skipped_count, failed_count, enable_sound, enable_thinking, position_snapshot, created_at, updated_at FROM local_positions WHERE id=?`, positionID)
	position, err := scanPosition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, fmt.Errorf("本地岗位运行不存在")
	}
	return position, err
}

// UpdatePosition 更新本地岗位运行基础信息。
// positionID 为岗位运行 ID，payload 为更新参数。
func (db *DB) UpdatePosition(positionID string, payload map[string]any) (Position, error) {
	existing, err := db.GetPosition(positionID)
	if err != nil {
		return Position{}, err
	}
	updated := existing
	updated.Name = stringOr(payload["name"], existing.Name)
	updated.PlatformID = stringOr(payload["platform_id"], existing.PlatformID)
	updated.PlatformAccountID = stringOr(payload["platform_account_id"], existing.PlatformAccountID)
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
		return Position{}, fmt.Errorf("岗位快照格式不正确：%w", err)
	}
	_, err = db.conn.Exec(`
UPDATE local_positions
SET name=?, platform_id=?, platform_account_id=?, mode=?,
    match_limit=?, enable_sound=?, enable_thinking=?, position_snapshot=?, updated_at=?
WHERE id=?`,
		updated.Name, updated.PlatformID, updated.PlatformAccountID, updated.Mode,
		updated.MatchLimit, boolInt(updated.EnableSound), boolInt(updated.EnableThinking), string(positionJSON),
		updated.UpdatedAt, positionID,
	)
	if err != nil {
		return Position{}, fmt.Errorf("更新本地岗位运行失败：%w", err)
	}
	return db.GetPosition(positionID)
}

// UpdatePositionStatus 更新岗位运行状态。
// positionID 为岗位运行 ID，status 为新状态。
func (db *DB) UpdatePositionStatus(positionID string, status string) (Position, error) {
	if status == "" {
		return Position{}, fmt.Errorf("岗位运行状态不能为空")
	}
	result, err := db.conn.Exec(`UPDATE local_positions SET status=?, updated_at=? WHERE id=?`, status, nowISO(), positionID)
	if err != nil {
		return Position{}, fmt.Errorf("更新岗位运行状态失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Position{}, fmt.Errorf("本地岗位运行不存在")
	}
	return db.GetPosition(positionID)
}

// IncrementPositionCounts 累加岗位运行统计数量。
// positionID 为岗位运行 ID，scanned/greeted/skipped/failed 为增量。
func (db *DB) IncrementPositionCounts(positionID string, scanned int, greeted int, skipped int, failed int) (Position, error) {
	result, err := db.conn.Exec(`
UPDATE local_positions
SET scanned_count=scanned_count+?,
    greeted_count=greeted_count+?,
    skipped_count=skipped_count+?,
    failed_count=failed_count+?,
    updated_at=?
WHERE id=?`,
		maxInt(0, scanned), maxInt(0, greeted), maxInt(0, skipped), maxInt(0, failed), nowISO(), positionID,
	)
	if err != nil {
		return Position{}, fmt.Errorf("更新岗位运行统计失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return Position{}, fmt.Errorf("本地岗位运行不存在")
	}
	return db.GetPosition(positionID)
}

// DeletePosition 删除本地岗位运行及关联数据。
// positionID 为岗位运行 ID。
func (db *DB) DeletePosition(positionID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_positions WHERE id=?`, positionID)
	if err != nil {
		return fmt.Errorf("删除本地岗位运行失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("本地岗位运行不存在")
	}
	return nil
}

// AddPositionLog 写入本地岗位运行日志。
// positionID 为岗位运行 ID，level 为日志级别，message 为日志内容。
func (db *DB) AddPositionLog(positionID string, level string, message string) (Log, error) {
	if _, err := db.GetPosition(positionID); err != nil {
		return Log{}, err
	}
	if level == "" {
		level = "info"
	}
	now := nowISO()
	result, err := db.conn.Exec(
		`INSERT INTO local_position_logs(position_id, level, message, created_at) VALUES(?, ?, ?, ?)`,
		positionID, level, message, now,
	)
	if err != nil {
		return Log{}, fmt.Errorf("写入岗位运行日志失败：%w", err)
	}
	id, _ := result.LastInsertId()
	_ = db.TrimPositionLogs(positionID, 1000)
	return Log{ID: id, PositionID: positionID, Level: level, Message: message, CreatedAt: now}, nil
}

// TrimPositionLogs 只保留指定岗位运行最近 limit 条日志。
// positionID 为岗位运行 ID，limit 为保留数量；清理失败时返回错误。
func (db *DB) TrimPositionLogs(positionID string, limit int) error {
	if limit <= 0 {
		limit = 1000
	}
	_, err := db.conn.Exec(
		`DELETE FROM local_position_logs
		 WHERE position_id=?
		   AND id NOT IN (
		     SELECT id FROM local_position_logs WHERE position_id=? ORDER BY id DESC LIMIT ?
		   )`,
		positionID,
		positionID,
		limit,
	)
	if err != nil {
		return fmt.Errorf("清理旧岗位运行日志失败：%w", err)
	}
	return nil
}

// ListPositionLogs 读取本地岗位运行日志。
// positionID 为岗位运行 ID，limit 为最大返回数量。
func (db *DB) ListPositionLogs(positionID string, limit int) ([]Log, error) {
	if limit <= 0 || limit > 5000 {
		limit = 100
	}
	rows, err := db.conn.Query(
		`SELECT id, position_id, level, message, created_at FROM local_position_logs WHERE position_id=? ORDER BY id DESC LIMIT ?`,
		positionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("读取岗位运行日志失败：%w", err)
	}
	defer rows.Close()
	logs := []Log{}
	for rows.Next() {
		var item Log
		if err := rows.Scan(&item.ID, &item.PositionID, &item.Level, &item.Message, &item.CreatedAt); err != nil {
			return nil, err
		}
		logs = append([]Log{item}, logs...)
	}
	return logs, rows.Err()
}

// ClearPositionLogs 清空本地岗位运行日志。
// positionID 为岗位运行 ID。
func (db *DB) ClearPositionLogs(positionID string) error {
	if _, err := db.GetPosition(positionID); err != nil {
		return err
	}
	if _, err := db.conn.Exec(`DELETE FROM local_position_logs WHERE position_id=?`, positionID); err != nil {
		return fmt.Errorf("清空岗位运行日志失败：%w", err)
	}
	return nil
}

// SaveCandidate 保存/更新本地候选人（全部结构化字段）。
// positionID 为岗位运行 ID，candidate 为候选人数据。
func (db *DB) SaveCandidate(positionID string, candidate map[string]any) (result map[string]any, resultErr error) {
	defer func() {
		if r := recover(); r != nil {
			resultErr = fmt.Errorf("保存候选人时发生内部错误：%v", r)
			result = nil
		}
	}()
	if _, err := db.GetPosition(positionID); err != nil {
		return nil, err
	}
	now := nowISO()
	candidateID := stringOr(candidate["id"], uuid.NewString())
	candidate["id"] = candidateID
	candidate["position_id"] = positionID
	candidateName := stringOr(candidate["candidate_name"], stringOr(candidate["name"], ""))
	status := stringOr(candidate["status"], "")
	_, err := db.conn.Exec(`
INSERT INTO local_candidates(
    id, position_id, candidate_name, status,
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
ON CONFLICT(position_id, id) DO UPDATE SET
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
		candidateID, positionID, candidateName, status,
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
// positionID 为岗位运行 ID，返回候选人列表。
func (db *DB) ListCandidates(positionID string) ([]map[string]any, error) {
	rows, err := db.conn.Query(`SELECT candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE position_id=? ORDER BY updated_at DESC`, positionID)
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
		item["position_id"] = positionID
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListCandidatesFiltered 按条件读取本地候选人分页列表（结构化字段）。
// filter 为筛选条件，返回候选人列表、总数和错误信息。
func (db *DB) ListCandidatesFiltered(filter CandidateFilter) ([]map[string]any, int, error) {
	rows, err := db.conn.Query(`SELECT position_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates ORDER BY updated_at DESC`)
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
		var rowPositionID string
		err := rows.Scan(&rowPositionID, &cName, &cStatus, &birthYM, &phone, &email, &workRegion, &workYears,
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
		item["position_id"] = rowPositionID
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
// candidateID 为候选人 ID，positionID 为空时会在全部岗位运行中查找。
func (db *DB) GetCandidate(candidateID string, positionID string) (map[string]any, error) {
	if strings.TrimSpace(candidateID) == "" {
		return nil, fmt.Errorf("候选人 ID 不能为空")
	}
	var row *sql.Row
	if strings.TrimSpace(positionID) != "" {
		row = db.conn.QueryRow(`SELECT position_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE position_id=? AND id=?`, positionID, candidateID)
	} else {
		row = db.conn.QueryRow(`SELECT position_id, candidate_name, status, birth_ym, phone, email, work_region, work_years, expected_salary_min, expected_salary_max, personal_description, work_status, expected_position, online_status, education_level, basic_info, raw_text, filter_text, work_experiences, educations, certificates, honors, project_experiences, colleague_communications, resume_attachment_url, resume_attachment_extracted_text, ai_detail_reason, ai_detail_score, ai_greet_reason, ai_greet_score, ai_review_reason, ai_review_score, ext, first_seen_at, detail_fetched_at, greeted_at, created_at, updated_at FROM local_candidates WHERE id=? ORDER BY updated_at DESC LIMIT 1`, candidateID)
	}
	var rowPositionID, cName, cStatus, birthYM, phone, email, workRegion, workYears string
	var salMin, salMax *int
	var personalDesc, workStatus, expectedPos, onlineStatus, eduLevel string
	var basicInfo, rawText, filterText string
	var workExps, edus, certs, honors, projExps, comms string
	var resumeURL, resumeText string
	var aiDetailReason, aiGreetReason, aiReviewReason string
	var aiDetailScore, aiGreetScore, aiReviewScore *float64
	var ext, firstSeen, detailFetched, greeted, createdAt, updatedAt string
	err := row.Scan(&rowPositionID, &cName, &cStatus, &birthYM, &phone, &email, &workRegion, &workYears,
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
	item["position_id"] = rowPositionID
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

// DeleteCandidate 删除本地岗位运行候选人。
// positionID 为岗位运行 ID，candidateID 为候选人 ID。
func (db *DB) DeleteCandidate(positionID string, candidateID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_candidates WHERE position_id=? AND id=?`, positionID, candidateID)
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

// scanPosition 从数据库行扫描岗位运行。
// scanner 为 QueryRow 或 Rows。
func scanPosition(scanner interface{ Scan(dest ...any) error }) (Position, error) {
	var position Position
	var enableSound int
	var positionJSON string
	var enableThinking int
	err := scanner.Scan(
		&position.ID, &position.Name, &position.PlatformID, &position.PlatformAccountID, &position.Mode,
		&position.MatchLimit, &position.Status, &position.ScannedCount, &position.GreetedCount, &position.SkippedCount,
		&position.FailedCount, &enableSound, &enableThinking, &positionJSON, &position.CreatedAt, &position.UpdatedAt,
	)
	if err != nil {
		return Position{}, err
	}
	position.EnableSound = enableSound == 1
	position.EnableThinking = enableThinking == 1
	position.PositionSnapshot = map[string]any{}
	_ = json.Unmarshal([]byte(positionJSON), &position.PositionSnapshot)
	return position, nil
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
