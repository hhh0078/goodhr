// Package model 本文件定义可选的平台原生资料索要能力与明确未执行的结果，不把平台限制写进公共流程。
package model

import "context"

const (
	AutoReplyInfoRequested        = "requested"
	AutoReplyInfoAlreadyRequested = "already_requested"
	AutoReplyInfoReceived         = "received"
	AutoReplyInfoDeclined         = "declined"
	AutoReplyInfoDeferred         = "deferred"
	AutoReplyInfoUnavailable      = "unavailable"
)

// AutoReplyInfoRequestResult 表示平台核实的单类索要结果；deferred和unavailable明确表示尚未点击发送。
type AutoReplyInfoRequestResult struct {
	Status            string `json:"status"`
	EvidenceMessageID string `json:"evidence_message_id,omitempty"`
	// EvidenceMessage 复用本次已读的真实原生回执，供服务器同步原消息；不改写方向、类型、正文或稳定键。
	EvidenceMessage *ConversationMessage `json:"evidence_message,omitempty"`
	// EvidenceMessages 仅在原生接收后回传本次已读的真实顺序窗口，保留文件插回旧卡位置与已知聊天锚点。
	EvidenceMessages []ConversationMessage `json:"evidence_messages,omitempty"`
	Reason           string                `json:"reason,omitempty"`
	Phone            string                `json:"phone,omitempty"`
	Wechat           string                `json:"wechat,omitempty"`
}

// AutoReplyInfoRequester 由支持原生按钮的平台选择实现；每次只能请求简历、手机或微信中的一种。
type AutoReplyInfoRequester interface {
	RequestAutoReplyCandidateInfo(context.Context, Browser, Config, AutoReplyConversationSnapshot, CandidateInfoRequest) (AutoReplyInfoRequestResult, error)
}

// CandidateInfoRequestKind 只接受明确的一种资料，避免多按钮执行中断后无法判断哪些动作已经发生。
func CandidateInfoRequestKind(request CandidateInfoRequest) string {
	count, kind := 0, ""
	for _, item := range []struct {
		selected bool
		kind     string
	}{{request.RequestResume, "resume"}, {request.RequestPhone, "phone"}, {request.RequestWechat, "wechat"}} {
		if item.selected {
			count++
			kind = item.kind
		}
	}
	if count != 1 {
		return ""
	}
	return kind
}
