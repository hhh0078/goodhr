// Package httpapi 文件作用：把云端自动回复权威账本中的安全AI请求元数据投影到岗位日志和分享，不双写、不被本机日志覆盖。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/cloudruntime"
)

// listPositionLogs 在原日志查询后合并有真实账号岗位归属的AI事件，普通日志存储、替换和旧故障补领保持原路径。
func (s *PositionLogService) listPositionLogs(tenantID, email, positionID string, isAdmin bool, query PositionLogQuery) ([]PositionLog, bool, error) {
	ordinary, more, err := s.logStore.ListPositionLogs(tenantID, email, positionID, isAdmin, query)
	if err != nil || s.runtimeStore == nil || s.runtimeStore.db == nil {
		return ordinary, more, err
	}
	position, err := s.positions.PositionByID(tenantID, email, positionID, isAdmin)
	if err != nil {
		return nil, false, err
	}
	owner := strings.TrimSpace(position.UserEmail)
	if owner == "" {
		owner = email
	}
	runtime, err := s.runtimeStore.aiRequestPositionLogs(tenantID, owner, positionID, query)
	if err != nil {
		return nil, false, err
	}
	items := append(ordinary, runtime...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	limit := normalizePositionLogLimit(query.Limit)
	if len(items) > limit {
		items, more = items[:limit], true
	}
	return items, more, nil
}

// aiRequestPositionLogs 只读当前租户真实岗位所有者的最近千条AI元数据，旧记录和坏时间不会打断整页查询。
func (s *PostgresAutoReplyStore) aiRequestPositionLogs(tenantID, owner, positionID string, query PositionLogQuery) ([]PositionLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var cutoffText string
	err := s.db.QueryRowContext(ctx, `SELECT coalesce(e.payload->>'cleared_at','') FROM auto_reply_cloud_entities e
		JOIN users u ON u.id=e.user_id AND u.tenant_id=e.tenant_id
		JOIN positions p ON p.user_id=u.id AND p.id::text=$3
		WHERE e.tenant_id=$1 AND u.email=$2 AND e.kind='runtime_events' AND e.id=$4
		AND e.payload->>'title'='ai_request_log_cutoff' AND e.payload->>'position_id'=p.id::text`, tenantID, owner, positionID, "ai-log-cutoff:"+positionID).Scan(&cutoffText)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	cutoff, _ := time.Parse(time.RFC3339Nano, cutoffText)
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.payload FROM auto_reply_cloud_entities e
		JOIN users u ON u.id=e.user_id AND u.tenant_id=e.tenant_id
		JOIN positions p ON p.user_id=u.id AND p.id::text=$3
		WHERE e.tenant_id=$1 AND u.email=$2 AND e.kind='runtime_events'
		AND e.payload->>'title'='ai_request_attempt' AND e.payload->>'position_id'=p.id::text
		ORDER BY e.updated_at DESC,e.id DESC LIMIT $4`, tenantID, owner, positionID, maxPositionLogsPerPosition)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PositionLog{}
	for rows.Next() {
		var id string
		var raw json.RawMessage
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		item, ok := aiRequestPositionLog(id, owner, positionID, raw)
		if !ok || !cutoff.IsZero() && !item.CreatedAt.After(cutoff) || query.Since != nil && item.CreatedAt.Before(*query.Since) || query.Before != nil && !item.CreatedAt.Before(*query.Before) {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// clearAIRequestPositionLogs 通过既有账号版本事务追加可见性截止时间，保留权威事件，不改变发送和故障补领水位。
func (s *PostgresAutoReplyStore) clearAIRequestPositionLogs(tenantID, owner, positionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	at := time.Now().UTC().Format(time.RFC3339Nano)
	id := "ai-log-cutoff:" + positionID
	for attempt := 0; attempt < 3; attempt++ {
		var userID string
		var version, revision int64
		var hasAI bool
		err := s.db.QueryRowContext(ctx, `SELECT u.id,coalesce(o.version,0),coalesce(e.revision,0),
			EXISTS(SELECT 1 FROM auto_reply_cloud_entities a WHERE a.tenant_id=u.tenant_id AND a.user_id=u.id AND a.kind='runtime_events' AND a.payload->>'title'='ai_request_attempt' AND a.payload->>'position_id'=p.id::text) FROM users u
			JOIN positions p ON p.user_id=u.id AND p.id::text=$3
			LEFT JOIN auto_reply_state_owners o ON o.tenant_id=u.tenant_id AND o.user_id=u.id
			LEFT JOIN auto_reply_cloud_entities e ON e.tenant_id=u.tenant_id AND e.user_id=u.id AND e.kind='runtime_events' AND e.id=$4
			WHERE u.tenant_id=$1 AND u.email=$2 AND u.status='active'`, tenantID, owner, positionID, id).Scan(&userID, &version, &revision, &hasAI)
		if err != nil {
			return err
		}
		if !hasAI {
			return nil
		}
		raw, _ := json.Marshal(map[string]string{"title": "ai_request_log_cutoff", "position_id": positionID, "cleared_at": at})
		_, err = s.CommitAutoReplyState(ctx, tenantID, userID, version, []AutoReplyStateChange{{Kind: "runtime_events", ID: id, Payload: raw, Revision: revision + 1, UpdatedAt: at}})
		if !errors.Is(err, errReplyStateConflict) {
			return err
		}
	}
	return errReplyStateConflict
}

// aiRequestPositionLog 只投影明确的强类型AI请求，忽略原事件description及所有额外正文，摘要由固定元数据重新生成。
func aiRequestPositionLog(id, owner, positionID string, raw json.RawMessage) (PositionLog, bool) {
	var value struct {
		Title      string                        `json:"title"`
		TaskID     string                        `json:"task_id"`
		PositionID string                        `json:"position_id"`
		OccurredAt string                        `json:"occurred_at"`
		Attempt    cloudruntime.AIRequestAttempt `json:"ai_request_attempt"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return PositionLog{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, value.OccurredAt)
	event := value.Attempt
	if err != nil || value.Title != "ai_request_attempt" || value.PositionID != positionID || value.TaskID != "" && safeAITraceID(value.TaskID) != value.TaskID || !validRuntimeAIAttempt(event) {
		return PositionLog{}, false
	}
	message := cloudruntime.AIRequestAttemptMessage(event)
	category, level, errorMessage := "运行", "info", ""
	if event.Result == "failed" {
		category, level, errorMessage = "错误", "error", message
	}
	output, _ := json.Marshal(map[string]cloudruntime.AIRequestAttempt{"ai_request_attempt": event})
	output, err = sanitizeDiagnosticObject(output)
	if err != nil {
		return PositionLog{}, false
	}
	encoded, _ := json.Marshal(positionDiagnosticEvent{Timestamp: at.UTC().Format(time.RFC3339Nano), Category: category, IsError: event.Result == "failed", ErrorMessage: errorMessage, Message: message, TaskID: value.TaskID, Action: "ai", Step: "ai_request_attempt", Status: event.Result, DurationMS: event.ElapsedMS, Input: json.RawMessage(`{}`), Output: output})
	return PositionLog{ID: "runtime_ai_" + id, PositionID: positionID, UserEmail: owner, Level: level, Message: string(encoded), CreatedAt: at.UTC()}, true
}

// validRuntimeAIAttempt 验证来源、关联、阶段和有限分类，未知元数据只跳过该条，不影响业务或其它日志。
func validRuntimeAIAttempt(v cloudruntime.AIRequestAttempt) bool {
	if strings.ContainsAny(v.Operation+v.Reason+v.StopReason, "|") {
		return false
	}
	if v.Layer != "client" && v.Layer != "server" || v.Phase != "start" && v.Phase != "end" && v.Phase != "retry_wait_stopped" || v.ClientRequestID == "" || safeAITraceID(v.ClientRequestID) != v.ClientRequestID || v.AnalysisGroupID == "" || safeAITraceID(v.AnalysisGroupID) != v.AnalysisGroupID || v.ClientAttempt < 1 || v.ClientAttempt > 3 || v.MaxAttempts < 1 || v.MaxAttempts > 4 || v.ServerAttempt < 0 || v.ServerAttempt > 4 || v.ElapsedMS < 0 || v.BackoffMS < 0 || v.HTTPStatus != 0 && (v.HTTPStatus < 100 || v.HTTPStatus > 599) || safeAIModelName(v.Model) != v.Model || safeAITraceID(v.ProviderRequestID) != v.ProviderRequestID {
		return false
	}
	if !strings.Contains("|AI 请求|候选人详情评分|候选人基础预分析|候选人图片评分|生成聊天回复|AI 工具调用|AI 推荐分析|AI 在线简历识别|AI 简历摘要|AI 图片转录|AI 简历解析|", "|"+v.Operation+"|") || v.Operation == "" {
		return false
	}
	if v.Provider != "builtin" && v.Provider != "external" && v.Provider != "primary" && v.Provider != "fallback" {
		return false
	}
	if v.Result != "started" && v.Result != "success" && v.Result != "failed" {
		return false
	}
	allowed := "|request_started|success|http_status|transport_error|dns_error|connection_refused|connection_reset|connection_closed|tls_error|timeout|cancelled|read_body|read_stream|response_too_large|response_json_invalid|response_invalid|tool_json_invalid|tool_index_invalid|model_json_invalid|stream_invalid|response_empty|billing_failed|billing_total_invalid|retry_stopped|upstream_error|"
	if !strings.Contains(allowed, "|"+v.Reason+"|") || v.Reason == "" {
		return false
	}
	stops := "||billing_failed|billing_total_invalid|retry_stopped|cancelled|timeout|not_retryable|early_score_available|attempts_exhausted|"
	if !strings.Contains(stops, "|"+v.StopReason+"|") {
		return false
	}
	if v.RetryReason != "" && v.RetryReason != v.Reason {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, v.StartedAt)
	if err != nil {
		return false
	}
	if v.Layer == "client" && (v.ServerAttempt != 0 || v.ClientAttempt > v.MaxAttempts) || v.Layer == "server" && (v.ServerAttempt < 1 || v.ServerAttempt > v.MaxAttempts) {
		return false
	}
	if v.Phase == "start" && v.Result != "started" || v.Phase != "start" && v.Result == "started" {
		return false
	}
	if v.Phase != "start" {
		if finish, err := time.Parse(time.RFC3339Nano, v.FinishedAt); err != nil || finish.Before(start) {
			return false
		}
	}
	return true
}
