// Package auto_reply 本文件限制自动回复上下文大小，只保留本轮消息、近期历史与有用资料，避免反复发送全文。
package auto_reply

import (
	"encoding/json"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

const maxReplyContextBytes = 128 * 1024

// boundedReplyContext 复制并压缩模型输入，完整保留本轮批次；超预算时明确停止而不悄悄遗漏本轮问题。
func boundedReplyContext(input ReplyContext) (ReplyContext, error) {
	result := input
	result.Messages = append([]cloud.AutoReplyMessage(nil), input.Messages...)
	result.PendingMessages = append([]cloud.AutoReplyMessage(nil), input.PendingMessages...)
	if len(result.PendingMessages) == 0 && !input.ConditionsOnly {
		latest, found := findBasedOnMessage(result.Messages, result.BasedOnMessageKey)
		if !found {
			return result, fmt.Errorf("本轮候选人消息暂时没有读完整，请重新读取后再回复")
		}
		result.PendingMessages = []cloud.AutoReplyMessage{latest}
	}
	pendingBytes, _ := json.Marshal(result.PendingMessages)
	if len(pendingBytes) > maxReplyContextBytes/3 {
		return result, fmt.Errorf("本轮候选人消息较长，已暂停自动回答，请人工看一下")
	}
	if len(result.Messages) > 80 {
		result.Messages = result.Messages[len(result.Messages)-80:]
	}
	// 证据验证和模型共用同一份可见消息，不允许引用未给模型看的历史。
	present := make(map[string]bool, len(result.Messages))
	for _, message := range result.Messages {
		present[firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)] = true
	}
	for _, message := range result.PendingMessages {
		key := firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)
		if !present[key] {
			result.Messages = append(result.Messages, message)
			present[key] = true
		}
	}
	if input.ConditionsOnly {
		message, found := findBasedOnMessage(input.Messages, input.BasedOnMessageKey)
		if !found {
			return result, fmt.Errorf("简历补审缺少来源消息，暂时不能继续")
		}
		key := firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)
		if !present[key] {
			result.Messages = append(result.Messages, message)
		}
	}
	result.CandidateState.Attachments = nil
	result.CandidateState.RecentMessageKeys = nil
	result.CandidateState.Conversation = nil
	result.CandidateState.Identity = nil
	if input.CandidateState.Candidate != nil {
		candidate := *input.CandidateState.Candidate
		candidate.RawText = boundedText(candidate.RawText, 12000)
		candidate.BasicInfo = boundedText(candidate.BasicInfo, 4000)
		candidate.PersonalDescription = boundedText(candidate.PersonalDescription, 4000)
		candidate.Communications = nil
		candidate.AvatarURL = ""
		result.CandidateState.Candidate = &candidate
	}
	if input.Resume != nil {
		resume := *input.Resume
		resume.OnlineResumeText = boundedText(resume.OnlineResumeText, 8000)
		result.Resume = &resume
	}
	result.Position.Config.PositionDescription = boundedText(result.Position.Config.PositionDescription, 6000)
	result.Position.CompanyProfile.Overview = boundedText(result.Position.CompanyProfile.Overview, 4000)
	result.Position.CompanyProfile.ExtraInfo = boundedText(result.Position.CompanyProfile.ExtraInfo, 4000)
	for {
		messages, err := initialToolMessages(result)
		if err != nil {
			return result, err
		}
		encoded, err := json.Marshal(messages)
		if err != nil {
			return result, err
		}
		if len(encoded) <= maxReplyContextBytes {
			return result, nil
		}
		removed := false
		for index, message := range result.Messages {
			key := firstNonEmpty(message.PlatformMessageID, message.Fingerprint, message.ID)
			isPending := key == input.BasedOnMessageKey || message.ID == input.BasedOnMessageKey
			for _, pending := range result.PendingMessages {
				if key == firstNonEmpty(pending.PlatformMessageID, pending.Fingerprint, pending.ID) {
					isPending = true
					break
				}
			}
			if !isPending {
				result.Messages = append(result.Messages[:index], result.Messages[index+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return result, fmt.Errorf("岗位或候选人资料超过本轮处理预算，请缩短资料或人工处理")
		}
	}
}

// boundedText 按字符限制资料长度，避免截断中文字符或修改共享输入。
func boundedText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
