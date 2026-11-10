// Package auto_reply 本文件把目标证据绑定到本轮模型可见的真实来源，避免模型拼造消息编号；已知来源冲突仍须纠错。
package auto_reply

import (
	"encoding/json"
	"strings"
)

// normalizeConfirmationSource 只为完全未知的引用绑定唯一逐字命中的当前候选人消息，岗位与简历来源不能互相冒用。
func (s *toolExecutionState) normalizeConfirmationSource(ref, quote string, allowPosition bool) (string, error) {
	ref, quote = strings.TrimSpace(ref), strings.TrimSpace(quote)
	visible := append(replyHistoryWithoutPending(s.input), s.input.PendingMessages...)
	matching := map[string]bool{}
	knownMessage, matchingMessage := false, ""
	for _, message := range visible {
		canonical := firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)
		known := ref != "" && (ref == message.ID || ref == message.PlatformMessageID || ref == message.Fingerprint)
		knownMessage = knownMessage || known
		// 消息由当前会话提供；明确带有其他会话编号的记录不能用于兜底或纠错提示。
		if message.Direction != "candidate" || canonical == "" || message.ConversationID != "" && message.ConversationID != s.input.Conversation.ID {
			continue
		}
		if quote != "" && strings.Contains(message.TextContent, quote) {
			matching[canonical] = true
			if known {
				matchingMessage = canonical
			}
		}
	}
	unique := ""
	if len(matching) == 1 {
		for key := range matching {
			unique = key
		}
	}
	fail := func(reason string) (string, error) {
		if unique != "" {
			return "", argumentError(reason + "；该原文唯一对应的source_ref=" + unique + "，请原样使用")
		}
		return "", argumentError(reason + "；该原文在本轮候选人消息中无法唯一判定，请核对原文和source_ref")
	}
	if ref == "" || quote == "" {
		return fail("目标证据需要真实source_ref和逐字原文evidence_text")
	}
	switch strings.ToLower(ref) {
	case "position":
		if allowPosition && strings.Contains(s.input.Position.Config.PositionDescription, quote) {
			return "position", nil
		}
		return fail("position证据没有对应当前岗位原文，不能据此判断候选人符合")
	case "online_profile":
		if s.input.CandidateState.Candidate != nil && s.input.CandidateState.Candidate.OnlineProfile != nil && strings.Contains(s.input.CandidateState.Candidate.OnlineProfile.Profile.RawText, quote) {
			return "online_profile", nil
		}
		return fail("online_profile证据没有对应当前在线资料原文")
	case "resume":
		if s.input.Resume != nil && strings.Contains(s.input.Resume.OnlineResumeText, quote) {
			return "resume", nil
		}
		if s.input.CandidateState.Candidate != nil && s.input.CandidateState.Candidate.ResumeStructured {
			raw, _ := json.Marshal(s.input.CandidateState.Candidate)
			if strings.Contains(string(raw), quote) {
				return "resume", nil
			}
		}
		return fail("resume证据没有对应当前简历原文")
	}
	if knownMessage {
		if matchingMessage != "" {
			return matchingMessage, nil
		}
		return fail("source_ref已指向一条消息，但该消息不是当前候选人的这段原文，不能自动改换来源")
	}
	if unique != "" {
		return unique, nil
	}
	return fail("source_ref未识别，不能绑定不存在或重复出现的证据")
}
