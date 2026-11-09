// 本文件在已获准访问的简历及招聘成员范围内，分页展示云端真实对话、AI步骤、外发收据和沟通目标。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

// candidateReplyHistory 是简历详情的独立分页数据，消息与操作记录可以分别翻页。
type candidateReplyHistory struct {
	Threads           []replyThreadView  `json:"threads"`
	ThreadID          string             `json:"thread_id"`
	Messages          []replyMessageView `json:"messages"`
	Events            []replyEventView   `json:"events"`
	Goals             []replyGoalView    `json:"goals"`
	NextMessageCursor string             `json:"next_message_cursor"`
	NextEventCursor   string             `json:"next_event_cursor"`
}
type replyThreadView struct {
	ID            string `json:"id"`
	PositionName  string `json:"position_name"`
	Platform      string `json:"platform"`
	CandidateName string `json:"candidate_name"`
	Status        string `json:"status"`
	UpdatedAt     string `json:"updated_at"`
}
type replyMessageView struct {
	Attachments []replyAttachmentView `json:"attachments,omitempty"`
	ID          string                `json:"id"`
	Side        string                `json:"side"`
	SenderType  string                `json:"sender_type"`
	Text        string                `json:"text"`
	SentAt      string                `json:"sent_at"`
	MessageType string                `json:"message_type"`
}
type replyEventView struct {
	Group       string `json:"group,omitempty"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	OccurredAt  string `json:"occurred_at"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}
type replyGoalView struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence"`
	EvidenceIDs []string `json:"evidence_message_ids,omitempty"`
	UpdatedAt   string   `json:"updated_at"`
}

// candidateReplyThreadScope 对简历历史和原件下载共用已授权的团队成员、云简历与会话关联。
const candidateReplyThreadScope = ` FROM auto_reply_cloud_entities l JOIN auto_reply_cloud_entities c ON c.tenant_id=l.tenant_id AND c.user_id=l.user_id AND c.kind='conversations' AND l.id='candidate:'||(c.payload->>'candidate_id')
 JOIN candidate_profiles profile ON profile.tenant_id=l.tenant_id AND profile.id::text=l.payload->>'value'
 JOIN users u ON u.tenant_id=c.tenant_id AND u.id=c.user_id LEFT JOIN positions p ON p.id::text=c.payload->>'position_id'
 WHERE l.tenant_id=$1 AND l.kind='cloud_links' AND l.payload->>'value'=$2 AND ($3 OR lower(u.email)=lower($4))`

// candidateHistory 从已核实的云端简历关联寻找会话，其他账号的同名、同线程号都不能越权使用。
func (s *PostgresAutoReplyStore) candidateHistory(ctx context.Context, scope CandidateLibraryScope, candidate string, args url.Values) (candidateReplyHistory, error) {
	out := candidateReplyHistory{Threads: []replyThreadView{}, Messages: []replyMessageView{}, Events: []replyEventView{}, Goals: []replyGoalView{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT c.user_id::text||':'||c.id,coalesce(p.name,''),coalesce(c.payload->>'platform_id',''),coalesce(c.payload->>'candidate_name',''),coalesce(c.payload->>'status',''),coalesce(nullif(c.payload->>'updated_at',''),c.updated_at)
`+candidateReplyThreadScope+` ORDER BY 6 DESC,1`, scope.TenantID, candidate, scope.IsAdmin, scope.UserEmail)
	if err != nil {
		return out, err
	}
	requested := args.Get("thread_id")
	for rows.Next() {
		var t replyThreadView
		if err := rows.Scan(&t.ID, &t.PositionName, &t.Platform, &t.CandidateName, &t.Status, &t.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Threads = append(out.Threads, t)
		if (requested == "" && out.ThreadID == "") || requested == t.ID {
			out.ThreadID = t.ID
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	if requested != "" && out.ThreadID == "" {
		return out, ErrNotFound
	}
	if out.ThreadID == "" {
		return out, tx.Commit()
	}
	user, thread, _ := strings.Cut(out.ThreadID, ":")
	if err = readHistoryMessages(ctx, tx, scope.TenantID, user, thread, args.Get("message_cursor"), &out); err != nil {
		return out, err
	}
	if err = readHistoryEvents(ctx, tx, scope.TenantID, user, thread, args.Get("event_cursor"), &out, args.Get("event_group")); err != nil {
		return out, err
	}
	if err = readHistoryGoals(ctx, tx, scope.TenantID, user, thread, &out); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// readHistoryMessages 以稳定三元组向前读取一页；只有平台出站编号和成功收据吻合才标记为AI发送。
func readHistoryMessages(ctx context.Context, tx *sql.Tx, tenant, user, thread, cursor string, out *candidateReplyHistory) error {
	const base = ` FROM auto_reply_cloud_entities m WHERE m.tenant_id=$1 AND m.user_id=$2 AND m.kind='messages' AND m.payload->>'conversation_id'=$3`
	var order float64
	var seq int64
	if cursor != "" {
		err := tx.QueryRowContext(ctx, "SELECT m.message_order,m.sequence"+base+" AND m.id=$4", tenant, user, thread, cursor).Scan(&order, &seq)
		if errors.Is(err, sql.ErrNoRows) {
			return newAutoReplyValidationError("聊天翻页位置已经变化，请重新打开")
		}
		if err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.id,coalesce(m.payload->>'direction',''),coalesce(m.payload->>'text_content',''),
 coalesce(m.payload->>'platform_sent_at',m.payload->>'ingested_at',m.updated_at),coalesce(m.payload->>'message_type',''),EXISTS(
 SELECT 1 FROM auto_reply_cloud_entities s WHERE s.tenant_id=m.tenant_id AND s.user_id=m.user_id AND s.kind='sends'
 AND s.payload->>'conversation_id'=$3 AND s.payload->>'status'='sent' AND nullif(s.payload->>'outbound_message_key','')=coalesce(nullif(m.payload->>'platform_message_id',''),m.payload->>'fingerprint')
 AND s.payload->>'content'=m.payload->>'text_content')`+base+` AND ($4='' OR (m.message_order,m.sequence,m.id)<($5,$6,$4)) ORDER BY m.message_order DESC,m.sequence DESC,m.id DESC LIMIT 51`, tenant, user, thread, cursor, order, seq)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m replyMessageView
		var direction string
		var ai bool
		if err := rows.Scan(&m.ID, &direction, &m.Text, &m.SentAt, &m.MessageType, &ai); err != nil {
			return err
		}
		if len(out.Messages) == 50 {
			out.NextMessageCursor = out.Messages[49].ID
			break
		}
		m.Side, m.SenderType = "unknown", "unknown"
		if direction == "candidate" {
			m.Side, m.SenderType = "candidate", "human"
		} else if direction == "self" {
			m.Side = "recruiter"
			if ai {
				m.SenderType = "ai"
			}
		}
		out.Messages = append(out.Messages, m)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	slices.Reverse(out.Messages)
	return readHistoryAttachments(ctx, tx, tenant, user, thread, out.Messages)
}

// readHistoryEvents 按真实会话绑定汇总操作；只有同一次动作的起止时刻或明确的计时值才能显示耗时。
func readHistoryEvents(ctx context.Context, tx *sql.Tx, tenant, user, thread, cursor string, out *candidateReplyHistory, mode ...string) error {
	group := "primary"
	if len(mode) > 0 && mode[0] != "" {
		group = mode[0]
	}
	switch group {
	case "primary", "verification", "all":
	default:
		return newAutoReplyValidationError("操作记录分类不可用")
	}
	const events = `WITH scoped AS (
 SELECT e.kind||':'||e.id AS eid,e.kind,e.payload,e.updated_at,
 CASE WHEN e.kind='runtime_commands' AND e.payload->>'action' IN ('verify','latest','open_latest','scan') THEN 'verification'
      WHEN e.kind IN ('runtime_events','runs','tools') THEN 'progress' ELSE 'action' END AS event_group
 FROM auto_reply_cloud_entities e WHERE e.tenant_id=$1 AND e.user_id=$2 AND (
   (e.kind IN ('runs','sends','attachments','recommendations','runtime_events','runtime_commands') AND e.payload->>'conversation_id'=$3)
   OR (e.kind='tools' AND EXISTS (SELECT 1 FROM auto_reply_cloud_entities a WHERE a.tenant_id=e.tenant_id AND a.user_id=e.user_id AND a.kind='runs' AND a.id=e.payload->>'ai_run_id' AND a.payload->>'conversation_id'=$3))
   OR (e.kind='reports' AND e.payload->'report' IS NOT NULL AND EXISTS (SELECT 1 FROM auto_reply_cloud_entities j WHERE j.tenant_id=e.tenant_id AND j.user_id=e.user_id AND j.kind='recommendations' AND j.payload->>'input_hash'=e.id AND j.payload->>'conversation_id'=$3))
 )), ev AS (
 SELECT *, coalesce(nullif(payload->>'occurred_at',''),nullif(payload->>'issued_at',''),nullif(nullif(payload->>'started_at','0001-01-01T00:00:00Z'),''),
   nullif(payload#>>'{report,generated_at}',''),nullif(nullif(payload->>'created_at','0001-01-01T00:00:00Z'),''),nullif(payload->>'updated_at',''),nullif(updated_at,''),'1970-01-01T00:00:00Z')::timestamptz AS ts
 FROM scoped WHERE $4='all' OR ($4='verification' AND event_group='verification') OR ($4='primary' AND event_group<>'verification')) `
	if cursor != "" {
		var found bool
		if err := tx.QueryRowContext(ctx, events+"SELECT EXISTS(SELECT 1 FROM ev WHERE eid=$5)", tenant, user, thread, group, cursor).Scan(&found); err != nil {
			return err
		}
		if !found {
			return newAutoReplyValidationError("操作翻页位置已经变化，请重新打开")
		}
	}
	rows, err := tx.QueryContext(ctx, events+`SELECT eid,kind,event_group,coalesce(payload->>'action',payload->>'tool_name',payload->>'title',''),
 CASE WHEN kind='runtime_commands' AND payload->>'status'='completed' AND nullif(payload->>'result_code','') IS NOT NULL THEN 'failed'
      WHEN kind='reports' THEN 'completed' ELSE coalesce(payload->>'status','') END,
 coalesce(nullif(payload->>'error_message',''),nullif(payload->>'description',''),nullif(payload->>'content',''),payload->>'original_name',''),
 ts,coalesce(payload->>'started_at',''),
 CASE WHEN kind='runtime_commands' AND payload->>'status'='completed' THEN coalesce(payload->>'updated_at','') ELSE coalesce(payload->>'completed_at','') END,
 CASE WHEN (payload->>'duration_ms') ~ '^[0-9]{1,16}$' THEN (payload->>'duration_ms')::bigint END
 FROM ev WHERE $5='' OR (ts,eid)<(SELECT ts,eid FROM ev WHERE eid=$5) ORDER BY ts DESC,eid DESC LIMIT 51`, tenant, user, thread, group, cursor)
	if err != nil {
		return err
	}
	defer rows.Close()
	titles := map[string]string{"runtime_commands": "招聘页面操作", "runtime_events": "运行进度", "runs": "AI 对话判断", "sends": "发送消息收据", "tools": "AI 工具调用", "attachments": "收到简历附件", "recommendations": "岗位推荐", "reports": "推荐报告已生成"}
	actions := map[string]string{"initialize": "打开消息页", "scan": "查看会话列表", "inspect": "核对候选人与岗位", "open": "打开候选人会话", "messages": "读取聊天记录", "latest": "检查最新消息", "open_latest": "检查当前会话的新消息", "verify": "核对当前会话身份", "request_resume": "请求简历卡", "collect_resume": "下载简历附件", "send": "发送平台消息", "close": "关闭会话", "avatar": "读取候选人头像"}
	stages := map[string]string{"sync": "同步聊天记录", "ai": "AI 分析进度", "tool": "AI 工具进度", "reply": "准备回复", "sent": "消息已回复", "resume_ai": "AI 整理简历", "resume_saved": "保存简历资料", "resume_published": "发布简历", "resume_publish_pending": "简历等待发布", "resume_review": "复核沟通目标", "process_conversation": "处理候选人会话", "finish_review": "完成简历复核", "upsert_confirmation_items": "更新沟通目标", "send_messages": "拟定回复", "notify_hr": "登记待确认问题", "suggest_config_change": "建议更新岗位资料"}
	for rows.Next() {
		var e replyEventView
		var kind, label, started, ended string
		var occurred time.Time
		if err = rows.Scan(&e.ID, &kind, &e.Group, &label, &e.Status, &e.Description, &occurred, &started, &ended, &e.DurationMS); err != nil {
			return err
		}
		if len(out.Events) == 50 {
			out.NextEventCursor = out.Events[49].ID
			break
		}
		e.Title = titles[kind]
		if kind == "runtime_commands" && actions[label] != "" {
			e.Title = actions[label]
		} else if stages[label] != "" {
			e.Title = stages[label]
		}
		if occurred.Unix() != 0 {
			e.OccurredAt = occurred.UTC().Format(time.RFC3339Nano)
		}
		start, startErr := time.Parse(time.RFC3339Nano, started)
		end, endErr := time.Parse(time.RFC3339Nano, ended)
		if startErr == nil && endErr == nil && !start.IsZero() && !end.Before(start) {
			elapsed := end.Sub(start).Milliseconds()
			e.DurationMS = &elapsed
		}
		out.Events = append(out.Events, e)
	}
	return rows.Err()
}

// readHistoryGoals 从确认记录返回条件和引文；仅链接同一会话中由来源编号和原文共同验证的消息。
func readHistoryGoals(ctx context.Context, tx *sql.Tx, tenant, user, thread string, out *candidateReplyHistory) error {
	rows, err := tx.QueryContext(ctx, `SELECT g.id,coalesce(g.payload->>'content',''),coalesce(g.payload->>'status',''),coalesce(g.payload->>'evidence_text',''),coalesce(g.payload->>'updated_at',g.updated_at),coalesce((SELECT m.id FROM auto_reply_cloud_entities m WHERE m.tenant_id=g.tenant_id AND m.user_id=g.user_id AND m.kind='messages' AND m.payload->>'conversation_id'=$3 AND g.payload->>'source_ref' IN (m.id,m.payload->>'platform_message_id',m.payload->>'fingerprint') AND nullif(g.payload->>'evidence_text','') IS NOT NULL AND strpos(m.payload->>'text_content',g.payload->>'evidence_text')>0 ORDER BY m.sequence LIMIT 1),'') FROM auto_reply_cloud_entities g WHERE g.tenant_id=$1 AND g.user_id=$2 AND g.kind='confirmations' AND g.payload->>'conversation_id'=$3 ORDER BY g.sequence,g.id`, tenant, user, thread)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var g replyGoalView
		var id string
		if err = rows.Scan(&g.ID, &g.Title, &g.Status, &g.Evidence, &g.UpdatedAt, &id); err != nil {
			return err
		}
		if id != "" {
			g.EvidenceIDs = []string{id}
		}
		out.Goals = append(out.Goals, g)
	}
	return rows.Err()
}
