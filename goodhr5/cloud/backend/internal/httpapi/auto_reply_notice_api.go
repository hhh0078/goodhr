// 本文件发送自动回复人工接管的简短邮件，数据库只保存去重摘要和发送状态。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"html"
	"net/http"
	"strings"
)

// autoReplyNoticeRequest 接收本地通知标识及必要摘要，兼容旧字段但拒收聊天正文。
type autoReplyNoticeRequest struct {
	ID                string `json:"id,omitempty"`
	PositionID        string `json:"position_id"`
	ConversationID    string `json:"conversation_id,omitempty"`
	BasedOnMessageKey string `json:"based_on_message_key"`
	ReasonKey         string `json:"reason_key"`
	CandidateName     string `json:"candidate_name"`
	Gender            string `json:"gender"`
	PlatformID        string `json:"platform_id"`
	Reason            string `json:"reason"`
	LatestMessage     string `json:"latest_message,omitempty"`
}

// agentSendNotification 校验岗位归属并复用通知状态，失败或过期领取允许后续重试。
func (s *AutoReplyService) agentSendNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "接管提醒这里只支持发送")
		return
	}
	var item autoReplyNoticeRequest
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	requestContext, position, ok := s.positionRequestContext(w, r, item.PositionID, true, true)
	if !ok {
		return
	}
	if err := validateAutoReplyNotice(item); err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "NOTICE_INVALID", err.Error())
		return
	}
	if isWechatAccount(requestContext.Session.Email) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email_sent": false, "warning": "微信账号暂不发送邮件，请到自动回复工作台查看待确认问题。"})
		return
	}
	dedupeKey := autoReplyNoticeDedupeKey(position.ID, item)
	id, existingStatus, err := s.store.claimAutoReplyNotice(r.Context(), requestContext.Tenant.ID, position.ID, requestContext.Session.Email, dedupeKey)
	if err != nil {
		writeAutoReplyStoreError(w, err, "这条提醒还没登记好，请稍后重试")
		return
	}
	if id == "" {
		warning := ""
		if existingStatus != "sent" {
			warning = "接手提醒仍在发送，稍后会继续确认。"
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true, "email_sent": existingStatus == "sent", "warning": warning})
		return
	}
	plain := strings.Join([]string{"我小声提醒一下，这位候选人需要你接手。", "岗位：" + position.Name, "候选人：" + item.CandidateName, "原因：" + item.Reason, "可以在岗位待确认问题或简历详情里查看沟通记录。"}, "\n")
	status := "failed"
	if s.mailer != nil {
		err = s.mailer.SendCustomHTML(requestContext.Session.Email, "GoodHR 自动回复需要接手："+position.Name, "<p>"+strings.ReplaceAll(html.EscapeString(plain), "\n", "<br>")+"</p>", plain)
		if err == nil {
			status = "sent"
		}
	}
	_, finishErr := s.store.db.ExecContext(r.Context(), `UPDATE auto_reply_notice_summaries SET status=$2,updated_at=now() WHERE id=$1`, id, status)
	if finishErr != nil {
		writeAutoReplyInternalError(w, "NOTICE_STATUS_FAILED", "邮件已尝试发送，状态暂时没记好，请稍后查看", finishErr)
		return
	}
	warning := ""
	if status != "sent" {
		warning = "接手提醒邮件暂时没发出去，已保留待重试；你也可以先查看本地运行记录。"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email_sent": status == "sent", "warning": warning, "notification": map[string]any{"id": id, "status": status}})
}

// claimAutoReplyNotice 原子领取新提醒、失败提醒或过期发送；未领取时返回真实状态，不能把pending当已发送。
func (s *PostgresAutoReplyStore) claimAutoReplyNotice(ctx context.Context, tenantID, positionID, email, key string) (id, existingStatus string, err error) {
	err = s.db.QueryRowContext(ctx, `INSERT INTO auto_reply_notice_summaries(tenant_id,position_id,recipient_email,dedupe_key) VALUES($1,$2,$3,$4)
	 ON CONFLICT(tenant_id,recipient_email,dedupe_key) DO UPDATE SET status='pending',updated_at=now()
	 WHERE auto_reply_notice_summaries.status='failed' OR (auto_reply_notice_summaries.status='pending' AND auto_reply_notice_summaries.updated_at < now()-interval '5 minutes')
	 RETURNING id::text`, tenantID, positionID, email, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = s.db.QueryRowContext(ctx, `SELECT status FROM auto_reply_notice_summaries WHERE tenant_id=$1 AND recipient_email=$2 AND dedupe_key=$3`, tenantID, email, key).Scan(&existingStatus)
	}
	return
}

// autoReplyNoticeDedupeKey 优先采用本地包含会话身份的通知摘要，避免不同候选人的相同消息互相去重。
func autoReplyNoticeDedupeKey(positionID string, item autoReplyNoticeRequest) string {
	identity := []string{positionID, "notice", strings.TrimSpace(item.ID)}
	if strings.TrimSpace(item.ID) == "" {
		identity = []string{positionID, "legacy", item.ConversationID, item.BasedOnMessageKey, item.ReasonKey}
	}
	sum := sha256.Sum256([]byte(strings.Join(identity, "\x00")))
	return hex.EncodeToString(sum[:])
}

// validateAutoReplyNotice 限制通知摘要长度，明确拒绝上传候选人的最新消息正文。
func validateAutoReplyNotice(item autoReplyNoticeRequest) error {
	if item.LatestMessage != "" {
		return newAutoReplyValidationError("提醒只需一小段原因，原话可以从沟通记录查看")
	}
	if strings.TrimSpace(item.BasedOnMessageKey) == "" || strings.TrimSpace(item.ReasonKey) == "" || strings.TrimSpace(item.Reason) == "" {
		return newAutoReplyValidationError("提醒还缺少原因或去重标识")
	}
	if strings.TrimSpace(item.ID) == "" && strings.TrimSpace(item.ConversationID) == "" {
		return newAutoReplyValidationError("提醒还缺少会话标识，请更新本地程序后重试")
	}
	if len(item.ID) > 256 || len([]rune(item.Reason)) > 500 || len([]rune(item.CandidateName)) > 100 || len(item.BasedOnMessageKey) > 256 || len(item.ReasonKey) > 256 || len(item.ConversationID) > 256 {
		return newAutoReplyValidationError("提醒稍微有点长，请只留下必要摘要")
	}
	return nil
}
