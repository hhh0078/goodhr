// Package replybridge 本文件定义服务器与本机之间的白名单平台操作数据，显式保留身份和消息锚点。
package replybridge

import (
	"goodhr5/local-agent-go-new/internal/platform/model"
	"time"
)

// Request 仅承载当前平台动作的输入，不接受脚本或远端选择器。
type Request struct {
	Snapshot     model.AutoReplyConversationSnapshot `json:"snapshot"`
	Conversation model.Conversation                  `json:"conversation"`
	Known        []string                            `json:"known,omitempty"`
	MaxHistory   int                                 `json:"max_history,omitempty"`
	Message      string                              `json:"message,omitempty"`
	InfoRequest  model.CandidateInfoRequest          `json:"info_request,omitempty"`
}

const maxTransferFiles = 5
const maxTransferFileBytes = 20 << 20
const maxTransferTotalBytes = 64 << 20

// File 是带校验和的临时文件传输，不把本机文件路径当成服务器路径。
type File struct {
	Name   string `json:"name"`
	Data   []byte `json:"data"`
	SHA256 string `json:"sha256"`
}

// Response 保存一次平台操作的真实观察证据。
type Response struct {
	LocalPaths    []string                            `json:"-"`
	Snapshot      model.AutoReplyConversationSnapshot `json:"snapshot"`
	Conversations []model.Conversation                `json:"conversations,omitempty"`
	Messages      []model.ConversationMessage         `json:"messages,omitempty"`
	Latest        model.ConversationMessage           `json:"latest"`
	Opened        bool                                `json:"opened"`
	Complete      bool                                `json:"complete"`
	Resume        model.AutoReplyResumeBundle         `json:"resume"`
	Files         []File                              `json:"files,omitempty"`
	Avatar        []byte                              `json:"avatar,omitempty"`
	ObservedAt    time.Time                           `json:"observed_at"`
	InfoResult    model.AutoReplyInfoRequestResult    `json:"info_result,omitempty"`
	OnlineProfile model.AutoReplyOnlineProfile        `json:"online_profile,omitempty"`
}
