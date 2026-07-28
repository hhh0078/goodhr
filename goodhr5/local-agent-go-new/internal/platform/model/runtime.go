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
	PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error)
	ClosePage(context.Context) error
}

// Config 保存云端下发的平台 URL、选择器和行为参数。
type Config struct {
	ID                  string                           `json:"id"`
	Name                string                           `json:"name"`
	LoginURL            string                           `json:"login_url"`
	EntryURL            string                           `json:"entry_url"`
	MessagesURL         string                           `json:"messages_url"`
	Selectors           map[string]contract.SelectorSpec `json:"selectors"`
	CandidateFields     map[string]contract.SelectorSpec `json:"candidate_fields"`
	ConversationFields  map[string]contract.SelectorSpec `json:"conversation_fields"`
	LoginInitActions    []ConfiguredAction               `json:"login_init_actions"`
	GreetingInitActions []ConfiguredAction               `json:"greeting_init_actions"`
	MessageInitActions  []ConfiguredAction               `json:"message_init_actions"`
	FilterActions       []ConfiguredAction               `json:"filter_actions"`
	Behavior            Behavior                         `json:"behavior"`
	ScrollDistance      int                              `json:"scroll_distance"`
	MaxItems            int                              `json:"max_items"`
}

// ConfiguredAction 表示平台配置驱动的一步点击或输入动作。
type ConfiguredAction struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	SelectorKey string `json:"selector_key"`
	Value       string `json:"value"`
	Optional    bool   `json:"optional"`
}

// Behavior 表示平台翻页、详情和岗位选择行为。
type Behavior struct {
	SupportsPaging            bool `json:"supports_paging"`
	SkipPositionSelection     bool `json:"skip_position_selection"`
	DirectPositionSelection   bool `json:"direct_position_selection"`
	SelectFirstPositionResult bool `json:"select_first_position_result"`
	NeedsDetail               bool `json:"needs_detail"`
}

// Position 表示平台准备阶段所需的岗位信息。
type Position struct {
	ID                        string `json:"id"`
	Name                      string `json:"name"`
	Keyword                   string `json:"keyword"`
	RequestPhone              bool   `json:"request_phone"`
	RequestWechat             bool   `json:"request_wechat"`
	RequestResume             bool   `json:"request_resume"`
	HLiepinShortcutSearchName string `json:"hliepin_shortcut_search_name"`
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

// CandidateInfoRequest 表示打招呼后需要执行的信息索要和追加文案。
type CandidateInfoRequest struct {
	RequestPhone  bool   `json:"request_phone"`
	RequestWechat bool   `json:"request_wechat"`
	RequestResume bool   `json:"request_resume"`
	Message       string `json:"message"`
}

// GreetRequest 表示打招呼文案和打招呼后是否保留沟通窗口。
type GreetRequest struct {
	Message              string `json:"message"`
	KeepConversationOpen bool   `json:"keep_conversation_open"`
}

// DetailBrowser 定义部分平台在 AI 判断前需要执行的拟人详情浏览能力。
type DetailBrowser interface {
	BrowseCandidateDetail(context.Context, Browser, Config, Candidate) error
}

// Conversation 表示未读会话摘要。
type Conversation struct {
	Index   int               `json:"index"`
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Summary string            `json:"summary"`
	Fields  map[string]string `json:"fields"`
}

// Runtime 定义各平台必须显式实现的页面、候选人和消息能力。
type Runtime interface {
	PlatformID() string
	OpenLoginPage(context.Context, Browser, Config) error
	InitializeLoginPage(context.Context, Browser, Config) error
	OpenGreetingPage(context.Context, Browser, Config) error
	InitializeGreetingPage(context.Context, Browser, Config) error
	SelectPosition(context.Context, Browser, Config, Position) error
	ApplyBasicFilters(context.Context, Browser, Config, Position) error
	FindCandidates(context.Context, Browser, Config) ([]Candidate, error)
	ScrollToCandidate(context.Context, Browser, Config, Candidate) error
	OpenCandidateDetail(context.Context, Browser, Config, Candidate) error
	ExtractCandidateDetail(context.Context, Browser, Config, Candidate) (CandidateDetail, error)
	CleanCandidateDetailText(string) string
	CloseCandidateDetail(context.Context, Browser, Config, Candidate) error
	GreetCandidate(context.Context, Browser, Config, Candidate, GreetRequest) error
	RequestCandidateInfo(context.Context, Browser, Config, Candidate, CandidateInfoRequest) error
	FavoriteCandidate(context.Context, Browser, Config, Candidate) error
	RejectCandidate(context.Context, Browser, Config, Candidate) error
	NextCandidatePage(context.Context, Browser, Config) (bool, error)
	ScrollCandidates(context.Context, Browser, Config) error
	OpenMessagesPage(context.Context, Browser, Config) error
	InitializeMessagesPage(context.Context, Browser, Config) error
	ScanUnreadConversations(context.Context, Browser, Config) ([]Conversation, error)
	ReadConversation(context.Context, Browser, Config, Conversation) (string, error)
	ReplyConversation(context.Context, Browser, Config, Conversation, string) error
}
