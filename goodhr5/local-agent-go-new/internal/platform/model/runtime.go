// Package model 定义 Go 主流程与招聘平台适配层之间的强类型接口和数据模型。
package model

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

var (
	// ErrCandidateAlreadyContacted 表示候选人卡片已经进入继续沟通状态，本轮无需重复打招呼。
	ErrCandidateAlreadyContacted = errors.New("候选人已经沟通过")
	// ErrCandidateDetailUnavailable 表示当前候选人卡片可以点击，但平台没有打开详情。
	ErrCandidateDetailUnavailable = errors.New("候选人详情暂时不可用")
	// ErrCandidateDetailCleanupFailed 表示已出现的详情弹窗未能确认关闭，任务必须停止以免操作被遮挡的列表。
	ErrCandidateDetailCleanupFailed = errors.New("详情弹窗还没有关好，岗位先停一下；重新开始后再试")
	// ErrGreetingUnconfirmed 表示点击后未获得发送成功证据，必须停止以免重复发送或虚增计数。
	ErrGreetingUnconfirmed = errors.New("尚未确认招呼发送成功，请检查平台权益或提示后再继续")
	// ErrAutoReplyConversationNotOpen 仅表示平台明确未找到当前聊天，不代表身份冲突或浏览器读取失败。
	ErrAutoReplyConversationNotOpen = errors.New("当前聊天暂未打开")
	// ErrAutoReplySupersededBeforeSend 表示已核实新来信到达且还没触发任何可能的发送。
	ErrAutoReplySupersededBeforeSend = errors.New("候选人又发来了消息，上一条回复已放下")
	// ErrAutoReplyResumeRequestUnavailable 仅表示发送前已确认平台索要入口不可用，可以改用文本索要。
	ErrAutoReplyResumeRequestUnavailable = errors.New("当前平台简历索要入口不可用，尚未触发发送")
)

// Browser 定义平台适配层可以调用的 Worker 封装能力。
type Browser interface {
	OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error)
	ListPages(context.Context) (contract.PageListResult, error)
	UsePage(context.Context, contract.PageUseRequest) (contract.PageInfo, error)
	FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error)
	Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error)
	Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error)
	Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error)
	Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error)
	PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error)
	ClosePage(context.Context) error
}

// Config 保存随本地程序发布的平台 URL、选择器和行为参数。
type Config struct {
	ID                    string                           `json:"id"`
	Name                  string                           `json:"name"`
	LoginURL              string                           `json:"login_url"`
	EntryURL              string                           `json:"entry_url"`
	ManualSearchURL       string                           `json:"manual_search_url"`
	MessagesURL           string                           `json:"messages_url"`
	LoginURLs             []string                         `json:"-"` // 仅数组格式启用多页面复用，旧字符串和对象继续使用单地址。
	EntryURLs             []string                         `json:"-"`
	MessagesURLs          []string                         `json:"-"`
	Selectors             map[string]contract.SelectorSpec `json:"selectors"`
	CandidateFields       map[string]contract.SelectorSpec `json:"candidate_fields"`
	ConversationFields    map[string]contract.SelectorSpec `json:"conversation_fields"`
	LoginInitActions      []ConfiguredAction               `json:"login_init_actions"`
	GreetingInitActions   []ConfiguredAction               `json:"greeting_init_actions"`
	MessageInitActions    []ConfiguredAction               `json:"message_init_actions"`
	FilterActions         []ConfiguredAction               `json:"filter_actions"`
	Behavior              Behavior                         `json:"behavior"`
	ScrollDistance        int                              `json:"scroll_distance"`
	MaxItems              int                              `json:"max_items"`
	CandidateBatchSize    int                              `json:"candidate_batch_size,omitempty"`    // 仅限制候选人整批读取，不改变会话或联系人数量；零沿用MaxItems。
	CandidateResumeFields []string                         `json:"candidate_resume_fields,omitempty"` // 平台选择已有的稳定身份字段，排除活跃标记等动态内容。
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
	AutoReplyAllContactInbox    bool   `json:"auto_reply_all_contact_inbox"`    // 扫描全部联系人，主流程按本机水位发现待核实的消息。
	AutoReplyNativeInfoRequests bool   `json:"auto_reply_native_info_requests"` // 资料索要使用平台原生能力，未满足平台条件时明确暂缓且不降级成文字。
	SupportsPostDeletionHistory bool   `json:"supports_post_deletion_history"`  // 可按稳定会话核身并且只读取删除后的消息。
	AutoReplyResumeLoadsHistory bool   `json:"auto_reply_resume_loads_history"` // 平台采集简历时自行加载并核验来源，公共流程无需先读历史再回到底部。
	AutoReplyAvatarContainerKey string `json:"auto_reply_avatar_container_key"` // 聊天历史内的头像使用已有滚动容器限定截图；页头头像无需配置。
	CandidateTopInset           int    `json:"candidate_top_inset"`             // 卡片操作需避让的固定页头高度，单位CSS像素；零表示无需平台额外避让。
	CandidateBottomInset        int    `json:"candidate_bottom_inset"`          // 实际操作按钮需避让的底部浮层高度，单位CSS像素；零保持旧平台行为。
	RequestPhoneMessage         string `json:"request_phone_message"`           // 不使用付费取号入口时，在已确认身份的聊天框发送的礼貌索要电话文案。
	RequestResumeMessage        string `json:"request_resume_message"`          // 平台邀请投递尚未验证时，在当前聊天框索要附件简历的文案。
	SupportsPaging              bool   `json:"supports_paging"`
	CandidateListMode           string `json:"candidate_list_mode"`
	AttachmentMode              string `json:"attachment_mode"`
	AttachmentAllowedScheme     string `json:"attachment_allowed_scheme"`
	AttachmentAllowedHost       string `json:"attachment_allowed_host"`
	AttachmentAllowedPath       string `json:"attachment_allowed_path"`
	AttachmentPreviewPath       string `json:"attachment_preview_path"` // 从当前可见预览文件地址验证的路径前缀，未知形态不得推测下载地址。
	SkipPositionSelection       bool   `json:"skip_position_selection"`
	DirectPositionSelection     bool   `json:"direct_position_selection"`
	SelectFirstPositionResult   bool   `json:"select_first_position_result"`
	NeedsDetail                 bool   `json:"needs_detail"`
	StitchDetailScreenshots     bool   `json:"stitch_detail_screenshots"`
	DetailCloseEscapeFirst      bool   `json:"detail_close_escape_first"`       // 先按 Escape 并确认详情消失，未关闭时才点击备用关闭按钮。
	AutoReplySendEnterFirst     bool   `json:"auto_reply_send_enter_first"`     // 自动回复优先用焦点已核验的回车发送。
	AutoReplySingleLineMessages bool   `json:"auto_reply_single_line_messages"` // 出站正文先把CRLF/LF换成普通空格，避免单行平台发送时直接连词。
	MessageSendEnterFallback    bool   `json:"message_send_enter_fallback"`     // 发送按钮明确尚未点击时，允许聚焦输入框后按回车兜底。
	MessageSendVerifyEmpty      bool   `json:"message_send_verify_empty"`       // 发送后确认输入框清空，避免把按键成功当成消息发送成功。

	AutoReplyOnlineProfileFragment          string `json:"auto_reply_online_profile_fragment"`           // 在线弹层允许的唯一URL片段；仅同源同路径同查询的空片段和此值可切换，空值保持原URL严格校验。
	AutoReplyOnlineProfileHidesConversation bool   `json:"auto_reply_online_profile_hides_conversation"` // 已实证的DOM在线弹层会隐藏原聊天；仅本次从原生核身入口打开后允许临时缺少聊天，关闭后仍须重新核身。
	AutoReplyMessageIdentityVersion         int    `json:"auto_reply_message_identity_version"`          // 平台消息键算法版本；零值不启用旧会话的一次性完整重采。
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
	Index              int               `json:"index"`
	Fingerprint        string            `json:"fingerprint"`
	Name               string            `json:"name"`
	Summary            string            `json:"summary"`
	Fields             map[string]string `json:"fields"`
	IdentityTexts      []string          `json:"identity_texts"`
	IdentityOccurrence int               `json:"identity_occurrence"`
	SkipReason         string            `json:"skip_reason,omitempty"` // 平台确认无需处理的卡片按正常跳过，不计入操作错误。
}

// CandidateListOptions 表示读取候选人列表时使用的任务级参数。
type CandidateListOptions struct {
	ReadConcurrency int                  `json:"read_concurrency"`
	Cursor          *CandidateListCursor `json:"-"`
}

// CandidateListCursor 保存当前任务滚动列表的读取位置，避免追加内容超过单批上限后重复读取开头。
type CandidateListCursor struct {
	StartIndex          int
	IdentityOccurrences map[string]int // 已读窗口中同名同龄候选人的数量，用于后续窗口稳定定位。
}

// CandidateDetail 表示当前任务内短期使用的候选人详情。
type CandidateDetail struct {
	Text string `json:"text"`
}

// CandidateInfoRequest 表示打招呼后需要执行的信息索要动作。
type CandidateInfoRequest struct {
	RequestPhone  bool `json:"request_phone"`
	RequestWechat bool `json:"request_wechat"`
	RequestResume bool `json:"request_resume"`
}

// GreetRequest 表示打招呼后是否保留沟通窗口。
type GreetRequest struct {
	KeepConversationOpen bool `json:"keep_conversation_open"`
}

// DetailBrowser 定义部分平台在 AI 判断前需要执行的拟人详情浏览能力。
type DetailBrowser interface {
	BrowseCandidateDetail(context.Context, Browser, Config, Candidate) error
}

// Conversation 表示未读会话摘要。
type Conversation struct {
	ReplyAfter            time.Time         `json:"reply_after,omitempty"`    // 首次采集时历史可读，但开始监听以前的消息不触发答复。
	WitnessDigest         string            `json:"witness_digest,omitempty"` // 已观察到变化的可见摘要，用于核对同分钟的新消息。
	Index                 int               `json:"index"`
	Key                   string            `json:"key"`
	Name                  string            `json:"name"`
	AvatarURL             string            `json:"avatar_url"`
	Gender                string            `json:"gender"`
	PlatformThreadID      string            `json:"platform_thread_id"`
	PlatformCandidateID   string            `json:"platform_candidate_id"`
	PlatformAccountID     string            `json:"platform_account_id"`
	CommunicationPosition string            `json:"communication_position"`
	Summary               string            `json:"summary"`
	Fields                map[string]string `json:"fields"`
}

// ConversationMessage 表示从招聘页面读取并准备同步云端的一条标准化聊天消息。
type ConversationMessage struct {
	Key               string          `json:"key"`
	PlatformMessageID string          `json:"platform_message_id"`
	Direction         string          `json:"direction"`
	MessageType       string          `json:"message_type"`
	TextContent       string          `json:"text_content"`
	CardContent       json.RawMessage `json:"card_content"`
	SenderName        string          `json:"sender_name"`
	SentAt            *time.Time      `json:"sent_at"`
}

// AutoReplyConversationSnapshot 表示打开会话后一次性读取的身份、岗位、消息和简历卡片状态。
type AutoReplyConversationSnapshot struct {
	DeliveryPageURL        string                `json:"delivery_page_url,omitempty"`         // 仅在单条发送调用内保存原页，确认送达时只允许读取这个页面的当前聊天尾部。
	ExpectedSendMessageKey string                `json:"expected_send_message_key,omitempty"` // 输入期间核对触发本次发送的候选人来信。
	HistoryAfter           *time.Time            `json:"history_after,omitempty"`             // 重新联系时只接受可信删除时间之后的消息，普通会话留空。
	Conversation           Conversation          `json:"conversation"`
	CandidateName          string                `json:"candidate_name"`
	AvatarURL              string                `json:"avatar_url"`
	Gender                 string                `json:"gender"`
	Phone                  string                `json:"phone"`
	PlatformThreadID       string                `json:"platform_thread_id"`
	PlatformCandidateID    string                `json:"platform_candidate_id"`
	PlatformAccountID      string                `json:"platform_account_id"`
	CommunicationPosition  string                `json:"communication_position"`
	Messages               []ConversationMessage `json:"messages"`
	HistoryComplete        bool                  `json:"history_complete"`
	ResumeCardAvailable    bool                  `json:"resume_card_available"`
	ResumeSourceMessageID  string                `json:"resume_source_message_id"`
	ResumeOfferMessageID   string                `json:"resume_offer_message_id,omitempty"` // 显式接收候选人本轮附件邀约的来源键，与实际文件卡来源分开。
}

// AutoReplyResumeBundle 表示平台读取的在线简历原文和本地已下载附件。
type AutoReplyResumeBundle struct {
	CandidateName         string   `json:"candidate_name"`
	Gender                string   `json:"gender"`
	Phone                 string   `json:"phone"`
	Email                 string   `json:"email"`
	Wechat                string   `json:"wechat"`
	BirthYM               string   `json:"birth_ym"`
	BirthYMPrecision      string   `json:"birth_ym_precision"`
	OnlineResumeText      string   `json:"online_resume_text"`
	AttachmentPaths       []string `json:"attachment_paths"`
	ResumeSourceMessageID string   `json:"resume_source_message_id"`
}

// AutoReplyIdentityVerifier 定义限制任务可选的实时身份核验能力，包装平台时必须保留，不能仅靠旧快照或姓名判断。
type AutoReplyIdentityVerifier interface {
	VerifyAutoReplyConversationIdentity(context.Context, Browser, Config, AutoReplyConversationSnapshot) error
}

// AutoReplyContactInspector 只打开并核实会话头，支持按 HistoryAfter 读取消息的平台才实现。
type AutoReplyContactInspector interface {
	InspectAutoReplyContact(context.Context, Browser, Config, Conversation) (AutoReplyConversationSnapshot, error)
}

// AutoReplyRuntime 定义自动回复独立主流程需要的平台页面能力。
type AutoReplyRuntime interface {
	InitializeAutoReplyPage(context.Context, Browser, Config, *AutoReplyConversationSnapshot) error
	OpenAutoReplyConversation(context.Context, Browser, Config, Conversation, []string, int) (AutoReplyConversationSnapshot, error)
	ReadAutoReplyMessages(context.Context, Browser, Config, AutoReplyConversationSnapshot, []string, int) ([]ConversationMessage, bool, error)
	RequestAutoReplyResume(context.Context, Browser, Config, AutoReplyConversationSnapshot) error
	CollectAutoReplyResume(context.Context, Browser, Config, AutoReplyConversationSnapshot) (AutoReplyResumeBundle, error)
	SendAutoReplyMessage(context.Context, Browser, Config, AutoReplyConversationSnapshot, string) error
	ReadLatestAutoReplyMessage(context.Context, Browser, Config, AutoReplyConversationSnapshot) (ConversationMessage, error)
	ReadOpenAutoReplyLatestMessage(context.Context, Browser, Config, AutoReplyConversationSnapshot) (ConversationMessage, bool, error)
	CloseAutoReplyConversation(context.Context, Browser, Config, AutoReplyConversationSnapshot) error
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
	FindCandidates(context.Context, Browser, Config, CandidateListOptions) ([]Candidate, error)
	ScrollToCandidate(context.Context, Browser, Config, Candidate) error
	OpenCandidateDetail(context.Context, Browser, Config, Candidate) error
	ExtractCandidateDetail(context.Context, Browser, Config, Candidate) (CandidateDetail, error)
	CleanCandidateDetailText(string) string
	CloseCandidateDetail(context.Context, Browser, Config, Candidate) error
	GreetCandidate(context.Context, Browser, Config, Candidate, GreetRequest) error
	EnsureCandidateConversation(context.Context, Browser, Config, Candidate) error
	RequestCandidateInfo(context.Context, Browser, Config, Candidate, CandidateInfoRequest) error
	SendCandidateMessage(context.Context, Browser, Config, Candidate, string) error
	CloseCandidateConversation(context.Context, Browser, Config, Candidate) error
	FavoriteCandidate(context.Context, Browser, Config, Candidate) error
	RejectCandidate(context.Context, Browser, Config, Candidate) error
	AdvanceCandidateList(context.Context, Browser, Config, []Candidate, CandidateListOptions) (bool, error)
	OpenMessagesPage(context.Context, Browser, Config) error
	InitializeMessagesPage(context.Context, Browser, Config) error
	ScanUnreadConversations(context.Context, Browser, Config) ([]Conversation, error)
	ReadConversation(context.Context, Browser, Config, Conversation) (string, error)
	ReplyConversation(context.Context, Browser, Config, Conversation, string) error
}
