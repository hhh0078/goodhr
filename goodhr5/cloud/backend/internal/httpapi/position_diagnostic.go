// 本文件负责验证并批量接收本地程序的结构化岗位诊断日志。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var diagnosticSensitiveKey = regexp.MustCompile(`(?i)cookie|authorization|token|password|secret|api[_-]?key|resume|candidate[_-]?name|phone|wechat|email|id[_-]?card|address|message[_-]?text|input[_-]?text`)
var diagnosticSecretText = regexp.MustCompile(`(?i)(bearer\s+|(?:token|password|secret|cookie|api[_-]?key)\s*[=:]\s*)[^\s&,;]+`)
var diagnosticURLQuery = regexp.MustCompile(`(https?://[^\s?"<>]+)\?[^\s"<>]*`)
var diagnosticPhoneText = regexp.MustCompile(`\b1[3-9][0-9]{9}\b`)
var diagnosticEmailText = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// positionDiagnosticEvent 表示对外分享时仍可安全阅读的一条岗位日志。
type positionDiagnosticEvent struct {
	Timestamp    string          `json:"timestamp"`
	Category     string          `json:"category"`
	IsError      bool            `json:"is_error"`
	ErrorMessage string          `json:"error_message"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	Message      string          `json:"message"`
	TaskID       string          `json:"task_id,omitempty"`
	Action       string          `json:"action,omitempty"`
	Step         string          `json:"step,omitempty"`
	Status       string          `json:"status,omitempty"`
	DurationMS   int64           `json:"duration_ms,omitempty"`
}

// Replace 验证岗位归属，并用最多最新 1000 条结构化日志替换旧记录。
func (s *PositionLogService) Replace(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	positionID, ok := positionIDFromLogsPath(w, r.URL.Path)
	if !ok {
		return
	}
	tenantID, isAdmin := s.getTenantInfo(session.Email)
	position, err := s.positions.PositionByID(tenantID, session.Email, positionID, isAdmin)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "position not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load position")
		return
	}
	ownerEmail := strings.TrimSpace(position.UserEmail)
	if ownerEmail == "" {
		ownerEmail = session.Email
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var body struct {
		Logs []positionDiagnosticEvent `json:"logs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid diagnostic logs")
		return
	}
	if len(body.Logs) == 0 {
		writeError(w, http.StatusBadRequest, "logs are required")
		return
	}
	if len(body.Logs) > maxPositionLogsPerPosition {
		body.Logs = body.Logs[len(body.Logs)-maxPositionLogsPerPosition:]
	}
	items := make([]PositionLog, 0, len(body.Logs))
	for _, event := range body.Logs {
		createdAt, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil || len(event.Category) == 0 || len(event.Category) > 80 || len(event.Message) > 2000 || len(event.ErrorMessage) > 2000 || event.DurationMS < 0 {
			writeError(w, http.StatusBadRequest, "invalid diagnostic log entry")
			return
		}
		event.Message = sanitizeDiagnosticText(event.Message)
		event.ErrorMessage = sanitizeDiagnosticText(event.ErrorMessage)
		event.Input, err = sanitizeDiagnosticObject(event.Input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid diagnostic input")
			return
		}
		event.Output, err = sanitizeDiagnosticObject(event.Output)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid diagnostic output")
			return
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid diagnostic log entry")
			return
		}
		level := "info"
		if event.IsError {
			level = "error"
		}
		items = append(items, PositionLog{PositionID: positionID, UserEmail: ownerEmail, Level: level, Message: string(encoded), CreatedAt: createdAt.UTC()})
	}
	if err := s.logStore.ReplacePositionLogs(positionID, ownerEmail, items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save diagnostic logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(items)})
}

// sanitizeDiagnosticObject 清理入参或出参中的敏感字段，并限制单项内容长度。
func sanitizeDiagnosticObject(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`), nil
	}
	if len(raw) > 16<<10 {
		return nil, errors.New("object too large")
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.New("object required")
	}
	encoded, err := json.Marshal(sanitizeDiagnosticValue(value))
	return json.RawMessage(encoded), err
}

// sanitizeDiagnosticValue 递归隐藏凭据及候选人敏感字段。
func sanitizeDiagnosticValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			if diagnosticSensitiveKey.MatchString(key) {
				item[key] = "[已隐藏]"
			} else {
				item[key] = sanitizeDiagnosticValue(child)
			}
		}
	case []any:
		if len(item) > 100 {
			item = item[:100]
		}
		for index, child := range item {
			item[index] = sanitizeDiagnosticValue(child)
		}
		return item
	case string:
		return sanitizeDiagnosticText(item)
	}
	return value
}

// sanitizeDiagnosticText 清理自由文本中的凭据和 URL 查询参数。
func sanitizeDiagnosticText(value string) string {
	value = diagnosticSecretText.ReplaceAllString(value, "${1}[已隐藏]")
	value = diagnosticURLQuery.ReplaceAllString(value, "${1}?[已隐藏]")
	value = diagnosticPhoneText.ReplaceAllString(value, "[手机号已隐藏]")
	value = diagnosticEmailText.ReplaceAllString(value, "[邮箱已隐藏]")
	value = strings.TrimSpace(value)
	if len(value) > 1000 {
		value = value[:1000] + "…"
	}
	return value
}
