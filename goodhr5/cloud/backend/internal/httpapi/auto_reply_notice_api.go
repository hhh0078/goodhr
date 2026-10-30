// 本文件发送自动回复人工接管的简短邮件，数据库只保存去重摘要和发送状态。
package httpapi

import (
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

// agentSendNotification 校验岗位归属后发送一次简短通知，重试相同标识不重复发送邮件。
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
	sum := sha256.Sum256([]byte(strings.Join([]string{position.ID, item.ConversationID, item.BasedOnMessageKey, item.ReasonKey}, "\x00")))
	dedupeKey := hex.EncodeToString(sum[:])
	var id string
	err := s.store.db.QueryRowContext(r.Context(), `INSERT INTO auto_reply_notice_summaries(tenant_id,position_id,recipient_email,dedupe_key) VALUES($1,$2,$3,$4)
	 ON CONFLICT(tenant_id,recipient_email,dedupe_key) DO UPDATE SET status='pending',updated_at=now() WHERE auto_reply_notice_summaries.status='failed' RETURNING id::text`, requestContext.Tenant.ID, position.ID, requestContext.Session.Email, dedupeKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
		return
	}
	if err != nil {
		writeAutoReplyStoreError(w, err, "这条提醒还没登记好，请稍后重试")
		return
	}
	plain := strings.Join([]string{"我小声提醒一下，这位候选人需要你接手。", "岗位：" + position.Name, "候选人：" + item.CandidateName, "原因：" + item.Reason, "详细消息保存在本机，其他候选人照常处理。"}, "\n")
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email_sent": status == "sent", "notification": map[string]any{"id": id, "status": status}})
}

// validateAutoReplyNotice 限制通知摘要长度，明确拒绝上传候选人的最新消息正文。
func validateAutoReplyNotice(item autoReplyNoticeRequest) error {
	if item.LatestMessage != "" {
		return newAutoReplyValidationError("候选人消息请留在本机，提醒只需一小段原因")
	}
	if strings.TrimSpace(item.BasedOnMessageKey) == "" || strings.TrimSpace(item.ReasonKey) == "" || strings.TrimSpace(item.Reason) == "" {
		return newAutoReplyValidationError("提醒还缺少原因或去重标识")
	}
	if len([]rune(item.Reason)) > 500 || len([]rune(item.CandidateName)) > 100 || len(item.BasedOnMessageKey) > 256 || len(item.ReasonKey) > 256 || len(item.ConversationID) > 256 {
		return newAutoReplyValidationError("提醒稍微有点长，请只留下必要摘要")
	}
	return nil
}
