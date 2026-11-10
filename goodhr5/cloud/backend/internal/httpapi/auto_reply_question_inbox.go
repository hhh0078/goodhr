// 本文件提供当前招聘成员可处理的待确认汇总与游标分页，不依赖本机连接，也不提前加载其他页原话。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

type replyInboxPosition struct {
	PositionID   string `json:"position_id"`
	PositionName string `json:"position_name"`
	PlatformID   string `json:"platform_id"`
	IsOpen       bool   `json:"is_open"`
	PendingCount int    `json:"pending_count"`
}

type replyInboxSummary struct {
	OK           bool                 `json:"ok"`
	PendingCount int                  `json:"pending_count"`
	Positions    []replyInboxPosition `json:"positions"`
}

type replyInboxPage struct {
	OK           bool            `json:"ok"`
	Questions    []replyQuestion `json:"questions"`
	NextCursor   string          `json:"next_cursor"`
	PendingCount int             `json:"pending_count"`
}

type replyInboxCursor struct {
	Position  string    `json:"p"`
	View      string    `json:"v"`
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"t"`
	Revision  string    `json:"r"`
}

// questionInboxScope 与岗位答复沿用同一成员归属，不因团队岗位列表可见而扩大处理权限。
const questionInboxScope = ` FROM auto_reply_cloud_questions q JOIN positions p ON p.id=q.position_id
 JOIN users u ON u.id=p.user_id AND u.tenant_id=q.tenant_id WHERE q.tenant_id=$1 AND u.email=$2 AND q.question<>''`

// questionInboxStale 与答复领取的两分钟期限一致；进程中断后仍让 HR 看见并重试原答复。
const questionInboxStale = `(q.status='processing' AND q.updated_at<=now()-interval '2 minutes')`
const questionInboxPending = `(q.status IN ('pending','failed') OR ` + questionInboxStale + `)`
const questionInboxColumns = `q.id,q.position_id,q.question,q.answer,CASE WHEN ` + questionInboxStale + ` THEN 'failed' ELSE q.status END,q.publish_target,CASE WHEN ` + questionInboxStale + ` THEN '上次整理没完成，原答复还在，可以重新保存。' ELSE q.error END,q.version,q.updated_at`

// QuestionSummary 只读取一次服务器汇总；未接本机或关闭岗位不会隐藏仍待处理的问题。
func (s *AutoReplyService) QuestionSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请使用读取方式查看待确认数量")
		return
	}
	access, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	if access.Session.Kind == extensionSessionKind {
		writeAutoReplyError(w, 403, "SESSION_NOT_ALLOWED", "请从管理页面查看待确认问题")
		return
	}
	out, err := s.store.questionInboxSummary(r.Context(), access.Tenant.ID, access.Session.Email)
	if err != nil {
		writeAutoReplyStoreError(w, err, "待确认数量暂时没读出来，请稍后重试")
		return
	}
	writeJSON(w, 200, out)
}

// QuestionsInbox 先校验岗位本人权限，再按当前页读取问题与其原话，保留旧答复接口的版本校验。
func (s *AutoReplyService) QuestionsInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请使用读取方式查看待确认问题")
		return
	}
	position := strings.TrimSpace(r.URL.Query().Get("position_id"))
	if !screeningUUIDPattern.MatchString(position) {
		writeAutoReplyError(w, 400, "POSITION_REQUIRED", "先选择需要查看的岗位")
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "pending"
	}
	if view != "pending" && view != "all" {
		writeAutoReplyError(w, 400, "UNKNOWN_VIEW", "请选择待处理或全部问题")
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 50 {
			writeAutoReplyError(w, 400, "INVALID_LIMIT", "每页可以查看1到50个问题")
			return
		}
		limit = n
	}
	access, _, ok := s.positionRequestContext(w, r, position, false, false)
	if !ok {
		return
	}
	if access.Session.Kind == extensionSessionKind {
		writeAutoReplyError(w, 403, "SESSION_NOT_ALLOWED", "请从管理页面查看待确认问题")
		return
	}
	out, err := s.store.questionInboxPage(r.Context(), access.Tenant.ID, access.Session.Email, position, view, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeAutoReplyStoreError(w, err, "待确认列表暂时没读出来，请稍后重试")
		return
	}
	writeJSON(w, 200, out)
}

// questionInboxSummary 只返回可处理岗位和数量，不把候选人原话加载到顶部按钮。
func (s *PostgresAutoReplyStore) questionInboxSummary(ctx context.Context, tenant, email string) (replyInboxSummary, error) {
	out := replyInboxSummary{OK: true, Positions: []replyInboxPosition{}}
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,p.name,coalesce(p.platform_id,'boss'),p.is_open,count(*) FILTER(WHERE `+questionInboxPending+`)`+questionInboxScope+` GROUP BY p.id,p.name,p.platform_id,p.is_open ORDER BY count(*) FILTER(WHERE `+questionInboxPending+`) DESC,p.name,p.id`, tenant, email)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item replyInboxPosition
		if err = rows.Scan(&item.PositionID, &item.PositionName, &item.PlatformID, &item.IsOpen, &item.PendingCount); err != nil {
			return out, err
		}
		out.PendingCount += item.PendingCount
		out.Positions = append(out.Positions, item)
	}
	return out, rows.Err()
}

// decodeReplyInboxCursor 绑定岗位和筛选条件，不接受其他列表或无效时间的翻页位置。
func decodeReplyInboxCursor(raw, position, view string) (replyInboxCursor, error) {
	var c replyInboxCursor
	if raw == "" {
		return c, nil
	}
	if len(raw) > 2048 {
		return c, newAutoReplyValidationError("翻页位置已变化，请重新查看")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Position != position || c.View != view || !screeningUUIDPattern.MatchString(c.ID) || c.UpdatedAt.IsZero() || len(c.Revision) != 32 {
		return replyInboxCursor{}, newAutoReplyValidationError("翻页位置已变化，请重新查看")
	}
	if _, err := hex.DecodeString(c.Revision); err != nil {
		return replyInboxCursor{}, newAutoReplyValidationError("翻页位置已变化，请重新查看")
	}
	return c, nil
}

// questionInboxPage 在同一读取快照中分页、计数和关联原话，答复后重新加载第一页即可看到新状态。
func (s *PostgresAutoReplyStore) questionInboxPage(ctx context.Context, tenant, email, position, view, rawCursor string, limit int) (replyInboxPage, error) {
	out := replyInboxPage{OK: true, Questions: []replyQuestion{}}
	if limit < 1 || limit > 50 {
		return out, newAutoReplyValidationError("每页可以查看1到50个问题")
	}
	cursor, err := decodeReplyInboxCursor(rawCursor, position, view)
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	// 全部视图也只展示尚未完成的工作，已完成条目保留在服务器供索引、补答及审计使用。
	where := questionInboxScope + ` AND q.position_id=$3 AND q.status<>'resolved' AND ($4='all' OR ` + questionInboxPending + `)`
	// 覆盖尚未读到的行被修改后移到前页的情况；只比较当前筛选的轻量元数据，不加载原话。
	var revision string
	err = tx.QueryRowContext(ctx, `SELECT md5(coalesce(string_agg(q.id::text||':'||q.version::text||':'||q.updated_at::text||':'||q.status,',' ORDER BY q.id),''))`+where, tenant, email, position, view).Scan(&revision)
	if err != nil {
		return out, err
	}
	if rawCursor != "" && cursor.Revision != revision {
		return out, newAutoReplyValidationError("列表已更新，请重新查看第一页")
	}
	if rawCursor != "" {
		var found bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1`+where+` AND q.id=$5 AND q.updated_at=$6)`, tenant, email, position, view, cursor.ID, cursor.UpdatedAt).Scan(&found)
		if err != nil {
			return out, err
		}
		if !found {
			return out, newAutoReplyValidationError("列表已更新，请重新查看第一页")
		}
	}
	var stamp any
	var id any
	if rawCursor != "" {
		stamp = cursor.UpdatedAt
		id = cursor.ID
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+questionInboxColumns+where+` AND ($5::timestamptz IS NULL OR (q.updated_at,q.id)<($5::timestamptz,$6::uuid)) ORDER BY q.updated_at DESC,q.id DESC LIMIT $7`, tenant, email, position, view, stamp, id, limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		q, e := scanReplyQuestion(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Questions = append(out.Questions, q)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Questions) > limit {
		out.Questions = out.Questions[:limit]
		last := out.Questions[limit-1]
		b, e := json.Marshal(replyInboxCursor{Position: position, View: view, ID: last.ID, UpdatedAt: last.UpdatedAt, Revision: revision})
		if e != nil {
			return out, e
		}
		out.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	if err = readInboxQuestionSources(ctx, tx, tenant, out.Questions); err != nil {
		return out, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*)`+questionInboxScope+` AND `+questionInboxPending, tenant, email).Scan(&out.PendingCount); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// readInboxQuestionSources 只加载当前页问题的真实来信，重复来源和跨成员同名会话不会串入。
func readInboxQuestionSources(ctx context.Context, tx *sql.Tx, tenant string, questions []replyQuestion) error {
	if len(questions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(questions))
	byID := make(map[string]int, len(questions))
	for i, q := range questions {
		ids = append(ids, q.ID)
		byID[q.ID] = i
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.question_id,m.id,m.payload->>'text_content',coalesce(c.payload->>'candidate_name','') FROM auto_reply_cloud_question_sources s JOIN auto_reply_cloud_entities m ON m.tenant_id=s.tenant_id AND m.user_id=s.user_id AND m.kind='messages' AND m.id=s.message_id JOIN auto_reply_cloud_entities c ON c.tenant_id=m.tenant_id AND c.user_id=m.user_id AND c.kind='conversations' AND c.id=m.payload->>'conversation_id' WHERE s.tenant_id=$1 AND s.question_id=ANY($2::uuid[]) ORDER BY m.message_order,m.sequence,m.id`, tenant, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var source replyQuestionSource
		if err = rows.Scan(&id, &source.MessageID, &source.Text, &source.CandidateName); err != nil {
			return err
		}
		if i, ok := byID[id]; ok {
			questions[i].SourceMessages = append(questions[i].SourceMessages, source)
		}
	}
	return rows.Err()
}
