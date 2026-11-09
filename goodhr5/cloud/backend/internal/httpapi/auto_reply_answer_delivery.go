// 本文件依据真实候选人来信派发HR已确认的公开补答，并在服务器按会话和答复版本核对收据。
package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
)

// replyAnswerDelivery 是允许补答的当前会话、最新提问来源和当前版公开事实。
type replyAnswerDelivery struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	PositionID     string `json:"position_id"`
	PlatformID     string `json:"platform_id"`
	AccountID      string `json:"platform_account_id"`
	ThreadID       string `json:"platform_thread_id"`
	SourceKey      string `json:"source_message_key"`
	Answer         string `json:"answer"`
}

// answerSources 每个岗位问题版本与会话取最新一条真实入站来源，自动忽略删除、关停和变更归属的会话。
const answerSources = `WITH source AS (
 SELECT DISTINCT ON(q.id,c.id) encode(sha256(convert_to(q.id::text||':'||q.version::text||':'||c.user_id::text||':'||c.id,'UTF8')),'hex') delivery_id,
 q.id AS question_id,q.version,c.id AS conversation_id,q.position_id,coalesce(c.payload->>'platform_id','') AS platform,coalesce(c.payload->>'platform_account_id','') AS account,
 coalesce(c.payload->>'platform_thread_id','') AS thread,coalesce(nullif(m.payload->>'platform_message_id',''),nullif(m.payload->>'fingerprint',''),m.id) AS source_key,q.fact
 FROM auto_reply_cloud_questions q JOIN auto_reply_cloud_question_sources x ON x.question_id=q.id AND x.tenant_id=q.tenant_id
 JOIN auto_reply_cloud_entities m ON m.tenant_id=x.tenant_id AND m.user_id=x.user_id AND m.kind='messages' AND m.id=x.message_id
 JOIN auto_reply_cloud_entities c ON c.tenant_id=m.tenant_id AND c.user_id=m.user_id AND c.kind='conversations' AND c.id=m.payload->>'conversation_id' AND c.payload->>'position_id'=q.position_id::text
 JOIN position_auto_reply_configs cfg ON cfg.position_id=q.position_id AND cfg.tenant_id=q.tenant_id AND cfg.enabled
 JOIN positions p ON p.id=q.position_id AND p.user_id=x.user_id
	WHERE q.tenant_id=$1 AND x.user_id=$2 AND q.status='resolved' AND q.applied_version=q.version AND char_length(q.fact) BETWEEN 1 AND 200
	 AND (q.publish_target NOT IN ('position','both') OR strpos(E'\n'||btrim(cfg.position_description)||E'\n',E'\n'||q.fact||E'\n')>0)
	 AND (q.publish_target NOT IN ('company','both') OR EXISTS(SELECT 1 FROM tenant_company_profiles co WHERE co.id=cfg.company_profile_id AND co.id=q.applied_company_profile_id AND co.tenant_id=q.tenant_id AND strpos(E'\n'||btrim(co.extra_info)||E'\n',E'\n'||q.fact||E'\n')>0))
 ORDER BY q.id,c.id,m.message_order DESC,m.sequence DESC,m.id DESC) `

// agentQuestionAnswers 只通过服务运行凭证、已登录的成员及当前绑定设备领取或更新补答。
func (s *AutoReplyService) agentQuestionAnswers(w http.ResponseWriter, r *http.Request, action string) {
	expected := http.MethodPost
	if action == "pending-answers" {
		expected = http.MethodGet
	}
	if r.Method != expected {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请使用对应的补答操作")
		return
	}
	secret := os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN")
	if secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(r.Header.Get("X-GoodHR-Runtime-Service"))) != 1 {
		writeAutoReplyError(w, 403, "RUNTIME_AUTH_REQUIRED", "补答只能由服务器自动回复流程领取")
		return
	}
	a, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	user, err := s.store.activeTenantUserID(r.Context(), a.Tenant.ID, a.Session.Email)
	if err != nil {
		writeAutoReplyStoreError(w, err, "成员信息暂时没读出来")
		return
	}
	machine := r.Header.Get("X-GoodHR-Machine-ID")
	if action == "pending-answers" {
		platform, account := r.URL.Query().Get("platform_id"), r.URL.Query().Get("platform_account_id")
		if platform == "" || account == "" {
			writeAutoReplyError(w, 400, "ACCOUNT_REQUIRED", "补答需要当前招聘账号")
			return
		}
		answers, err := s.store.pendingAnswers(r.Context(), a.Tenant.ID, user, machine, platform, account)
		if err != nil {
			writeAutoReplyStoreError(w, err, "待补答暂时没读出来")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "answers": answers})
		return
	}
	var in struct {
		ID           string `json:"id"`
		Conversation string `json:"conversation_id"`
		Status       string `json:"status"`
		Outbound     string `json:"outbound_message_key"`
	}
	if decodeAutoReplyJSON(w, r, &in) != nil {
		return
	}
	if action == "answer-claim" {
		claimed, err := s.store.claimAnswer(r.Context(), a.Tenant.ID, user, machine, in.ID, in.Conversation)
		if err != nil {
			writeAutoReplyStoreError(w, err, "补答暂时未领取")
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "claimed": claimed})
		return
	}
	err = s.store.finishAnswer(r.Context(), a.Tenant.ID, user, machine, in.ID, in.Conversation, in.Status, in.Outbound)
	if err != nil {
		writeAutoReplyStoreError(w, err, "发送回执没有核对上")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// pendingAnswers 保留本设备已领取但未交收据的记录，服务重启可以通过同一外发键继续核对。
func (s *PostgresAutoReplyStore) pendingAnswers(ctx context.Context, t, u, machine, platform, account string) ([]replyAnswerDelivery, error) {
	rows, err := s.db.QueryContext(ctx, answerSources+`SELECT s.delivery_id,s.conversation_id,s.position_id,s.platform,s.account,s.thread,s.source_key,s.fact FROM source s LEFT JOIN auto_reply_answer_receipts r ON r.tenant_id=$1 AND r.user_id=$2 AND r.delivery_id=s.delivery_id WHERE s.platform=$3 AND s.account=$4 AND (r.status IS NULL OR (r.status='claimed' AND (r.machine_id=$5 OR r.updated_at<now()-interval '90 seconds'))) ORDER BY s.delivery_id`, t, u, platform, account, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []replyAnswerDelivery{}
	for rows.Next() {
		var a replyAnswerDelivery
		if err = rows.Scan(&a.ID, &a.ConversationID, &a.PositionID, &a.PlatformID, &a.AccountID, &a.ThreadID, &a.SourceKey, &a.Answer); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// claimAnswer 再次核实当前版本仍可发，允许原设备恢复已领取但尚未确认的相同发送。
func (s *PostgresAutoReplyStore) claimAnswer(ctx context.Context, t, u, machine, id, conv string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var q string
	var v int
	err = tx.QueryRowContext(ctx, answerSources+`SELECT q.id,q.version FROM source s JOIN auto_reply_cloud_questions q ON q.id=s.question_id AND q.version=s.version AND q.status='resolved' WHERE s.delivery_id=$3 AND s.conversation_id=$4 FOR SHARE OF q`, t, u, id, conv).Scan(&q, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO auto_reply_answer_receipts(tenant_id,user_id,delivery_id,question_id,version,conversation_id,machine_id,status) VALUES($1,$2,$3,$4,$5,$6,$7,'claimed') ON CONFLICT(tenant_id,user_id,delivery_id) DO UPDATE SET machine_id=excluded.machine_id,updated_at=now() WHERE auto_reply_answer_receipts.status='claimed' AND (auto_reply_answer_receipts.machine_id=excluded.machine_id OR auto_reply_answer_receipts.updated_at<now()-interval '90 seconds')`, t, u, id, q, v, conv, machine)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, tx.Commit()
}

// finishAnswer 仅接受领取设备提交的相同发送回执，补答的发送结果不可改写。
func (s *PostgresAutoReplyStore) finishAnswer(ctx context.Context, t, u, machine, id, conv, status, key string) error {
	if (status != "sent" && status != "manual") || len(key) > 2048 || status == "sent" && strings.TrimSpace(key) == "" {
		return newAutoReplyValidationError("需要明确发送结果和实际出站消息编号")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE auto_reply_answer_receipts SET status=$6,outbound_message_key=$7,updated_at=now() WHERE tenant_id=$1 AND user_id=$2 AND machine_id=$3 AND delivery_id=$4 AND conversation_id=$5 AND (status='claimed' OR (status=$6 AND outbound_message_key=$7))`, t, u, machine, id, conv, status, key)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return errReplyStateConflict
	}
	return err
}
