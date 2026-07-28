// Package model 定义 Go 主流程与招聘平台适配层之间的强类型接口和数据模型。
package model

import (
	"context"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// Browser 定义平台适配层可以调用的 Worker 封装能力。
type Browser interface {
	OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error)
	FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error)
	Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error)
	Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error)
	Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error)
	Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error)
	ClosePage(context.Context) error
}

// Config 保存云端下发的平台 URL、选择器和行为参数。
type Config struct {
	ID                 string                           `json:"id"`
	EntryURL           string                           `json:"entry_url"`
	MessagesURL        string                           `json:"messages_url"`
	Selectors          map[string]contract.SelectorSpec `json:"selectors"`
	CandidateFields    map[string]contract.SelectorSpec `json:"candidate_fields"`
	ConversationFields map[string]contract.SelectorSpec `json:"conversation_fields"`
	ScrollDistance     int                              `json:"scroll_distance"`
	MaxItems           int                              `json:"max_items"`
}

// Position 表示平台准备阶段所需的岗位信息。
type Position struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Keyword string `json:"keyword"`
}

// Candidate 表示从招聘平台页面读取的候选人摘要。
type Candidate struct {
	Index       int               `json:"index"`
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name"`
	Summary     string            `json:"summary"`
	Fields      map[string]string `json:"fields"`
}

// CandidateDetail 表示当前任务内短期使用的候选人详情。
type CandidateDetail struct {
	Text string `json:"text"`
}

// Conversation 表示未读会话摘要。
type Conversation struct {
	Index   int               `json:"index"`
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Summary string            `json:"summary"`
	Fields  map[string]string `json:"fields"`
}

// Runtime 定义主动打招呼和自动回复主流程使用的平台能力。
type Runtime interface {
	PrepareGreeting(context.Context, Browser, Config, Position) error
	ScanCandidates(context.Context, Browser, Config) ([]Candidate, error)
	ReadCandidateDetail(context.Context, Browser, Config, Candidate) (CandidateDetail, error)
	GreetCandidate(context.Context, Browser, Config, Candidate, string) error
	CloseCandidateDetail(context.Context, Browser, Config) error
	ScrollCandidates(context.Context, Browser, Config) error
	PrepareAutoReply(context.Context, Browser, Config) error
	ScanUnreadConversations(context.Context, Browser, Config) ([]Conversation, error)
	ReadConversation(context.Context, Browser, Config, Conversation) (string, error)
	ReplyConversation(context.Context, Browser, Config, Conversation, string) error
}
