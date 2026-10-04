// Package httpapi 本文件负责本地 Agent 写入 AI 总记录、工具调用、配置建议和人工接管通知。
package httpapi

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// agentStartAIRun 创建一次基于候选人最新消息的 AI 总记录。
func (s *AutoReplyService) agentStartAIRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "AI 总记录这里只支持创建")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyAIRun
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.StartAutoReplyAIRun(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "AI 总记录没创建成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ai_run": saved})
}

// agentFinishAIRun 保存 AI 完整返回、错误和 Token 使用量。
func (s *AutoReplyService) agentFinishAIRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "AI 总记录这里只支持完成")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyAIRun
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.FinishAutoReplyAIRun(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "AI 总记录没完成保存")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ai_run": saved})
}

// agentSaveToolCall 幂等保存一次 AI 工具调用的参数、结果和错误。
func (s *AutoReplyService) agentSaveToolCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "AI 工具记录这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyToolCall
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.SaveAutoReplyToolCall(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "AI 工具记录没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tool_call": saved})
}

// agentSaveSuggestion 保存 AI 从聊天中学习到、等待 HR 审核的岗位或公司资料建议。
func (s *AutoReplyService) agentSaveSuggestion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "配置建议这里只支持保存")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyConfigSuggestion
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	saved, err := s.store.SaveAutoReplyConfigSuggestion(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "配置建议没保存成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "suggestion": saved})
}

// agentSendNotification 幂等发送无法自动处理的人工接管邮件，不让邮件失败暂停本地流程。
func (s *AutoReplyService) agentSendNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "人工接管通知这里只支持发送")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	var item AutoReplyNotification
	if err := decodeAutoReplyJSON(w, r, &item); err != nil {
		return
	}
	item.TenantID = requestContext.Tenant.ID
	item.RecipientEmail = requestContext.Session.Email
	latestMessage := strings.TrimSpace(item.LatestMessage)
	claimed, created, err := s.store.ClaimAutoReplyNotification(r.Context(), item)
	if err != nil {
		writeAutoReplyStoreError(w, err, "人工接管通知没登记成功")
		return
	}
	if !created {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "notification": claimed, "email_sent": claimed.Status == "sent", "duplicate": true,
		})
		return
	}
	claimed.LatestMessage = latestMessage
	positionName := "暂时没认出来"
	if claimed.PositionID != "" {
		if position, positionErr := s.positions.PositionByID(requestContext.Tenant.ID, requestContext.Session.Email, claimed.PositionID, false); positionErr == nil {
			positionName = position.Name
		}
	}
	mailErr := s.sendAutoReplyManualNotice(claimed, positionName)
	if mailErr != nil {
		claimed.Status = "failed"
		claimed.ErrorMessage = mailErr.Error()
		claimed, _ = s.store.FinishAutoReplyNotification(r.Context(), claimed)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "notification": claimed, "email_sent": false,
			"warning": "邮件这次没发出去，我已经记下来了，本地流程可以继续",
		})
		return
	}
	claimed.Status = "sent"
	claimed.ErrorMessage = ""
	claimed, err = s.store.FinishAutoReplyNotification(r.Context(), claimed)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "notification": claimed, "email_sent": true,
			"warning": "邮件已经发出，但发送记录暂时没盖上章，本地流程可以继续",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "notification": claimed, "email_sent": true})
}

// sendAutoReplyManualNotice 发送包含岗位、候选人、性别、平台和原因的人工接管邮件。
func (s *AutoReplyService) sendAutoReplyManualNotice(item AutoReplyNotification, positionName string) error {
	if s.mailer == nil {
		return fmt.Errorf("邮件服务没有初始化")
	}
	candidateName := firstNonEmpty(strings.TrimSpace(item.CandidateName), "暂时没认出来")
	gender := firstNonEmpty(strings.TrimSpace(item.Gender), "暂时没看到")
	platform := firstNonEmpty(strings.TrimSpace(item.PlatformID), "暂时没认出来")
	latestMessage := truncateAutoReplyText(firstNonEmpty(strings.TrimSpace(item.LatestMessage), "暂时没有可读文字"), 200)
	reason := truncateAutoReplyText(strings.TrimSpace(item.Reason), 500)
	plain := strings.Join([]string{
		"我小声提醒一下，这条候选人消息需要你接手。",
		"岗位名称：" + positionName,
		"候选人：" + candidateName,
		"性别：" + gender,
		"招聘平台：" + platform,
		"候选人最新消息：" + latestMessage,
		"我没敢自动回复的原因：" + reason,
		"我没有暂停其他候选人，你处理这条就行。",
	}, "\n")
	htmlBody := "<p>我小声提醒一下，这条候选人消息需要你接手。</p><ul>" +
		"<li>岗位名称：" + html.EscapeString(positionName) + "</li>" +
		"<li>候选人：" + html.EscapeString(candidateName) + "</li>" +
		"<li>性别：" + html.EscapeString(gender) + "</li>" +
		"<li>招聘平台：" + html.EscapeString(platform) + "</li>" +
		"<li>候选人最新消息：" + html.EscapeString(latestMessage) + "</li>" +
		"<li>我没敢自动回复的原因：" + html.EscapeString(reason) + "</li></ul>" +
		"<p>我没有暂停其他候选人，你处理这条就行。</p>"
	return s.mailer.SendCustomHTML(item.RecipientEmail, "GoodHR 自动回复需要人工接管："+positionName+" / "+candidateName, htmlBody, plain)
}

// truncateAutoReplyText 截断邮件和日志中的长文本，避免一条消息把通知撑得太长。
func truncateAutoReplyText(value string, limit int) string {
	chars := []rune(strings.TrimSpace(value))
	if len(chars) <= limit {
		return string(chars)
	}
	return string(chars[:limit]) + "…"
}
