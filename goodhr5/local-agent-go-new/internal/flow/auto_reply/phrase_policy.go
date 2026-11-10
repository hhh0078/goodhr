// Package auto_reply 本文件统一处理文字场景的空值与删除语义；静默不影响资料保存、沟通目标或平台原生资料动作资格。
package auto_reply

import (
	"context"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// modernReplyPhrases 只有旧服务完全未提供话术库时才允许旧内置文字，现代空列表不能恢复默认。
func modernReplyPhrases(config cloud.PositionAutoReplyConfig) bool {
	return config.Phrases != nil || config.PhrasesVersion != ""
}

// replyScenePhrase 保留条目存在与正文为空的区别；继承已由服务器合并，Flow不再自行补管理员默认。
func replyScenePhrase(config cloud.PositionAutoReplyConfig, id string) (cloud.AutoReplyPhrase, bool) {
	for _, phrase := range config.Phrases {
		if phrase.ID == id {
			return phrase, true
		}
	}
	return cloud.AutoReplyPhrase{}, false
}

// silentReplyScene 只控制场景文字；原生索要继续由need、平台资格和已问已得拒绝状态决定。
func silentReplyScene(config cloud.PositionAutoReplyConfig, id string) bool {
	if id == "" || !modernReplyPhrases(config) {
		return false
	}
	phrase, found := replyScenePhrase(config, id)
	return !found || phrase.Deleted || strings.TrimSpace(phrase.Content) == ""
}

// fixedCandidateReplyScene 仅识别无独立问题的固定收讫、结束和纯问候；混合问题仍由模型逐条绑定，不能整批吞掉正常问答。
func fixedCandidateReplyScene(input ReplyContext) string {
	if id := fixedReceiptID(input); id != "" {
		return id
	}
	if len(input.PendingMessages) != 1 || input.PendingMessages[0].Direction != "candidate" || input.PendingMessages[0].MessageType != "text" {
		return ""
	}
	text := strings.TrimSpace(input.PendingMessages[0].TextContent)
	if looksLikeCandidateQuestion(text) {
		return ""
	}
	simple := strings.Trim(text, " \t\r\n，,。.!！?？～~呀啊哦呢")
	if oneOf(simple, "不考虑", "不考虑了", "暂不考虑", "暂时不考虑", "暂时不考虑了", "我暂时不考虑", "我暂时不考虑了", "不感兴趣", "不用联系", "不用联系了", "不要联系", "不要联系了", "不找工作", "暂时不找工作") {
		return "end_conversation"
	}
	if oneOf(simple, "你好", "您好", "嗨", "哈喽", "在吗", "hello", "hi") {
		return "greeting"
	}
	return ""
}

// applyReplyScenePolicy 对可判固定场景优先绑定，其余按工具显式phrase_id处理；不声称能由任意自由文本证明所有自定义语义。
func applyReplyScenePolicy(input ReplyContext, message ReplyMessage) (ReplyMessage, bool) {
	message.PhraseID = strings.TrimSpace(message.PhraseID)
	if message.Type == "answer" && message.PhraseID != "required_mismatch" {
		if fixed := fixedCandidateReplyScene(input); fixed != "" {
			message.PhraseID = fixed
			message.RequestsResume = false
		}
	}
	if message.PhraseID == "" {
		switch {
		case message.RequestsResume || asksForResume(message.Content, fixedReplyPhrase(input.Position.Config, "request_resume")):
			message.PhraseID = "request_resume"
		case asksForPhone(message.Content, fixedReplyPhrase(input.Position.Config, "request_phone")):
			message.PhraseID = "request_phone"
		case asksForWechat(message.Content, fixedReplyPhrase(input.Position.Config, "request_wechat")):
			message.PhraseID = "request_wechat"
		}
	}
	if silentReplyScene(input.Position.Config, message.PhraseID) {
		return message, false
	}
	if phrase, found := replyScenePhrase(input.Position.Config, message.PhraseID); found && !phrase.Deleted {
		body := strings.TrimSpace(phrase.Content)
		// 无变量的话术正文必须采用HR当前内容；变量仍交既有占位符和真实上下文校验，不能回显占位符。
		if !strings.ContainsAny(body, "{}｛｝") {
			message.Content = body
		}
	}
	return message, true
}

// applyReplyDecisionScenes 在真正发送前再次应用相同策略，覆盖固定话术、模型替身和恢复路径，不把静默当作失败。
func applyReplyDecisionScenes(input ReplyContext, decision *ReplyDecision) {
	kept := make([]ReplyMessage, 0, len(decision.Messages))
	for _, message := range decision.Messages {
		message, allowed := applyReplyScenePolicy(input, message)
		if !allowed {
			decision.TextSuppressed = true
			continue
		}
		kept = append(kept, message)
	}
	decision.Messages = kept
}

// completeSilentReply 正常推进静默批次；原生动作只保留已提议待办，不跳过先答后索或平台双向沟通资格。
func (f *Flow) completeSilentReply(ctx context.Context, prepared shared.PreparedTask, input ReplyContext, request model.CandidateInfoRequest, allowNative bool) error {
	if allowNative && input.NativeInfoRequests && !candidateEndedAllContact(input.PendingMessages) && (request.RequestResume || request.RequestPhone || request.RequestWechat) {
		if err := f.Cloud.PlanAutoReplyInfoRequests(ctx, input.Credentials, input.Conversation.ID, input.BasedOnMessageKey, request); err != nil {
			return err
		}
	}
	if err := f.completeReplyBatch(ctx, prepared, input, "completed"); err != nil {
		return err
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		Kind: "auto_reply", PositionID: input.Position.Position.ID, Phase: "result", Stage: "silent", Terminal: true,
		CandidateName: input.PageSnapshot.CandidateName, Reason: "按话术设置，本轮不发文字", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return nil
}
