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
	replyThreadSummary
	Threads           []replyThreadView  `json:"threads"`
	ThreadID          string             `json:"thread_id"`
	Messages          []replyMessageView `json:"messages"`
	Events            []replyEventView   `json:"events"`
	Goals             []replyGoalView    `json:"goals"`
	NextMessageCursor string             `json:"next_message_cursor"`
	NextEventCursor   string             `json:"next_event_cursor"`
}
type replyThreadView struct {
	replyThreadSummary
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
	StatusLabel string `json:"status_label,omitempty"`
	OccurredAt  string `json:"occurred_at"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}
type replyGoalView struct {
	SourceType  string   `json:"source_type,omitempty"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Evidence    string   `json:"evidence"`
	EvidenceIDs []string `json:"evidence_message_ids,omitempty"`
	UpdatedAt   string   `json:"updated_at"`
}

// candidateReplyThreadScope 对简历历史和原件下载共用已授权的团队成员、云简历与会话关联。
const candidateReplyThreadScope = ` FROM auto_reply_cloud_entities c
 JOIN candidate_profiles profile ON profile.tenant_id=c.tenant_id AND profile.id::text=$2
 JOIN users u ON u.tenant_id=c.tenant_id AND u.id=c.user_id
 LEFT JOIN auto_reply_cloud_entities l ON l.tenant_id=c.tenant_id AND l.user_id=c.user_id AND l.kind='cloud_links' AND l.id='candidate:'||(c.payload->>'candidate_id')
 LEFT JOIN auto_reply_cloud_entities lc ON lc.tenant_id=c.tenant_id AND lc.user_id=c.user_id AND lc.kind='candidates' AND lc.id=c.payload->>'candidate_id'
 LEFT JOIN positions p ON p.id::text=c.payload->>'position_id'
 WHERE c.tenant_id=$1 AND c.kind='conversations' AND (l.payload->>'value'=$2 OR lc.payload#>>'{online_profile,candidate_id}'=$2)
 AND ($3 OR lower(u.email)=lower($4))`

// candidateHistory 从已核实的云端简历关联寻找会话，其他账号的同名、同线程号都不能越权使用。
func (s *PostgresAutoReplyStore) candidateHistory(ctx context.Context, scope CandidateLibraryScope, candidate string, args url.Values) (candidateReplyHistory, error) {
	out := candidateReplyHistory{replyThreadSummary: emptyReplyThreadSummary(), Threads: []replyThreadView{}, Messages: []replyMessageView{}, Events: []replyEventView{}, Goals: []replyGoalView{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT c.user_id::text||':'||c.id,coalesce(p.name,''),coalesce(c.payload->>'platform_id',''),coalesce(c.payload->>'candidate_name',''),coalesce(c.payload->>'status',''),coalesce(nullif(c.payload->>'updated_at',''),c.updated_at),
 coalesce(c.payload->'info_requests','{}'::jsonb),coalesce(lc.payload->>'Phone',lc.payload->>'phone',''),coalesce(lc.payload->>'Wechat',lc.payload->>'wechat',''),
 (lc.payload->'online_profile') #- '{profile,raw_text}' #- '{profile,screenshots}',
 (SELECT cs.online_profile #- '{profile,raw_text}' FROM candidate_profile_sources cs WHERE cs.tenant_id=c.tenant_id AND cs.candidate_id=profile.id
  AND cs.platform_id=c.payload->>'platform_id' AND cs.online_profile->>'conversation_id'=c.id
  AND cs.online_profile->>'profile_key'=lc.payload#>>'{online_profile,profile_key}' ORDER BY cs.updated_at DESC,cs.id LIMIT 1),
 u.email,coalesce(c.payload->>'platform_account_id','')
`+candidateReplyThreadScope+` ORDER BY 6 DESC,1`, scope.TenantID, candidate, scope.IsAdmin, scope.UserEmail)
	if err != nil {
		return out, err
	}
	requested := args.Get("thread_id")
	for rows.Next() {
		var t replyThreadView
		var info, online, source []byte
		var phone, wechat, email, account string
		if err := rows.Scan(&t.ID, &t.PositionName, &t.Platform, &t.CandidateName, &t.Status, &t.UpdatedAt, &info, &phone, &wechat, &online, &source, &email, &account); err != nil {
			rows.Close()
			return out, err
		}
		_, conversation, _ := strings.Cut(t.ID, ":")
		t.replyThreadSummary = replyHistorySummary(info, online, source, phone, wechat, candidate, conversation, t.Platform, email, account)
		out.Threads = append(out.Threads, t)
		if (requested == "" && out.ThreadID == "") || requested == t.ID {
			out.ThreadID = t.ID
			out.replyThreadSummary = t.replyThreadSummary
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
 )
 UNION ALL
 SELECT 'contact-received:'||(m.payload#>>'{card_content,info_type}')||':'||m.id,'received_contacts',
 jsonb_build_object('action',m.payload#>>'{card_content,info_type}','status','received',
 'description',CASE WHEN m.payload#>>'{card_content,info_type}'='phone' THEN '候选人已提供手机号' ELSE '候选人已提供微信' END,
 'occurred_at',coalesce(nullif(m.payload->>'platform_sent_at',''),nullif(m.payload->>'ingested_at',''),m.updated_at)),m.updated_at,'action'
 FROM auto_reply_cloud_entities m WHERE m.tenant_id=$1 AND m.user_id=$2 AND m.kind='messages' AND m.payload->>'conversation_id'=$3
 AND jsonb_typeof(m.payload->'card_content')='object' AND m.payload#>>'{card_content,info_status}'='received'
 AND ((m.payload#>>'{card_content,info_type}'='phone' AND m.payload#>>'{card_content,phone}' ~ '^[+]?[0-9]{6,20}$')
   OR (m.payload#>>'{card_content,info_type}'='wechat' AND m.payload#>>'{card_content,wechat}' ~ '^[a-zA-Z0-9][a-zA-Z0-9_-]{4,31}$'))
 ), ev AS (
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
 CASE WHEN (payload->>'duration_ms') ~ '^[0-9]{1,16}$' THEN (payload->>'duration_ms')::bigint END,coalesce(payload->>'message_key',''),coalesce(payload->>'resume_offer_message_id','')
 FROM ev WHERE $5='' OR (ts,eid)<(SELECT ts,eid FROM ev WHERE eid=$5) ORDER BY ts DESC,eid DESC LIMIT 51`, tenant, user, thread, group, cursor)
	if err != nil {
		return err
	}
	defer rows.Close()
	titles := map[string]string{"runtime_commands": "招聘页面操作", "runtime_events": "运行进度", "runs": "AI 对话判断", "sends": "发送消息收据", "tools": "AI 工具调用", "attachments": "收到简历附件", "recommendations": "岗位推荐", "reports": "推荐报告已生成"}
	actions := map[string]string{"initialize": "打开消息页", "scan": "查看会话列表", "inspect": "核对候选人与岗位", "open": "打开候选人会话", "messages": "读取聊天记录", "latest": "检查最新消息", "open_latest": "检查当前会话的新消息", "verify": "核对当前会话身份", "request_resume": "请求简历卡", "request_info": "索要候选人资料", "read_online_profile": "读取在线简历", "collect_resume": "下载简历附件", "send": "发送平台消息", "close": "关闭会话", "avatar": "读取候选人头像"}
	stages := map[string]string{"sync": "同步聊天记录", "ai": "AI 分析进度", "tool": "AI 工具进度", "reply": "准备回复", "sent": "消息已回复", "online_profile": "整理在线简历", "request_info_resume": "索要简历", "request_info_phone": "索要手机号", "request_info_wechat": "索要微信", "resume_ai": "AI 整理简历", "resume_saved": "保存简历资料", "resume_published": "发布简历", "resume_publish_pending": "简历等待发布", "resume_review": "复核沟通目标", "process_conversation": "处理候选人会话", "finish_review": "完成简历复核", "upsert_confirmation_items": "更新沟通目标", "archive_confirmation_items": "删除沟通目标", "goal_followup": "主动确认信息", "waiting_hr": "等待 HR 补充", "awaiting_hr_notice": "待确认提醒排队", "send_messages": "拟定回复", "notify_hr": "登记待确认问题", "suggest_config_change": "建议更新岗位资料"}
	stages["resume_offer"], stages["resume_offer_received"] = "接收简历申请", "已接收附件"
	stages["联系方式待核对"] = "联系方式待核对"
	for rows.Next() {
		var e replyEventView
		var kind, label, started, ended, messageKey, offerKey string
		var occurred time.Time
		if err = rows.Scan(&e.ID, &kind, &e.Group, &label, &e.Status, &e.Description, &occurred, &started, &ended, &e.DurationMS, &messageKey, &offerKey); err != nil {
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
		if kind == "runtime_commands" && label == "request_info" && offerKey != "" {
			e.Title = "接收简历申请"
		}
		if kind == "received_contacts" {
			e.Title = map[string]string{"phone": "已收到手机号", "wechat": "已收到微信"}[label]
		}
		if title, description, technical := replyHistoryInfoSend(kind, messageKey, e.Description); technical {
			e.Title, e.Description = title, description
			if strings.HasPrefix(messageKey, "resume-offer:") && e.Status == "sent" {
				e.Title = "已接收附件"
			}
		}
		e.StatusLabel = replyHistoryStatusLabel(e.Status)
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
	rows, err := tx.QueryContext(ctx, `SELECT g.id,coalesce(g.payload->>'content',''),coalesce(g.payload->>'status',''),coalesce(g.payload->>'evidence_text',''),coalesce(g.payload->>'updated_at',g.updated_at),coalesce((SELECT m.id FROM auto_reply_cloud_entities m WHERE m.tenant_id=g.tenant_id AND m.user_id=g.user_id AND m.kind='messages' AND m.payload->>'conversation_id'=$3 AND g.payload->>'source_ref' IN (m.id,m.payload->>'platform_message_id',m.payload->>'fingerprint') AND nullif(g.payload->>'evidence_text','') IS NOT NULL AND strpos(m.payload->>'text_content',g.payload->>'evidence_text')>0 ORDER BY m.sequence LIMIT 1),''),coalesce(nullif(g.payload->>'source_type',''),'position') FROM auto_reply_cloud_entities g WHERE g.tenant_id=$1 AND g.user_id=$2 AND g.kind='confirmations' AND g.payload->>'conversation_id'=$3 AND nullif(g.payload->>'archived_at','') IS NULL ORDER BY g.sequence,g.id`, tenant, user, thread)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var g replyGoalView
		var id string
		if err = rows.Scan(&g.ID, &g.Title, &g.Status, &g.Evidence, &g.UpdatedAt, &id, &g.SourceType); err != nil {
			return err
		}
		if id != "" {
			g.EvidenceIDs = []string{id}
		}
		out.Goals = append(out.Goals, g)
	}
	return rows.Err()
}
