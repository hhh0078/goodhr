// Package lifecycle 文件作用：为所有平台公开流程方法记录简短的成功、失败和异常日志。
package lifecycle

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// loggedRuntime 为平台实现增加逐方法日志，不改变平台操作和返回值。
type loggedRuntime struct {
	model.Runtime
	taskID string
	logger shared.Logger
}

// loggedDetailRuntime 只为原本支持详情浏览的平台保留该可选能力。
type loggedDetailRuntime struct {
	*loggedRuntime
	detailBrowser model.DetailBrowser
}

// newLoggedRuntime 创建日志封装，并保留底层平台已有的可选详情浏览能力。
func newLoggedRuntime(runtime model.Runtime, taskID string, logger shared.Logger) model.Runtime {
	logged := &loggedRuntime{Runtime: runtime, taskID: taskID, logger: logger}
	if detailBrowser, ok := runtime.(model.DetailBrowser); ok {
		return &loggedDetailRuntime{loggedRuntime: logged, detailBrowser: detailBrowser}
	}
	return logged
}

// BrowseCandidateDetail 转发平台详情浏览，并记录失败或异常，不改变返回结果。
func (l *loggedDetailRuntime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("browse_candidate_detail", time.Now(), &err)
	return l.detailBrowser.BrowseCandidateDetail(ctx, browser, config, candidate)
}

// record 在方法返回或发生 panic 时记录该方法的结果与耗时。
func (l *loggedRuntime) record(step string, startedAt time.Time, runErr *error) {
	if recovered := recover(); recovered != nil {
		if l.logger != nil {
			l.logger.Step(l.taskID, "platform", step, "panic", startedAt, fmt.Errorf("平台方法异常：%v", recovered))
		}
		panic(recovered)
	}
	if l.logger == nil {
		return
	}
	status := "success"
	var err error
	if runErr != nil {
		err = *runErr
	}
	if err != nil {
		status = "failed"
	}
	l.logger.Step(l.taskID, "platform", step, status, startedAt, err)
}

// PlatformID 记录读取当前平台编号的调用。
func (l *loggedRuntime) PlatformID() string {
	defer l.record("platform_id", time.Now(), nil)
	return l.Runtime.PlatformID()
}

// OpenLoginPage 记录打开登录页面的结果。
func (l *loggedRuntime) OpenLoginPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("open_login_page", time.Now(), &err)
	return l.Runtime.OpenLoginPage(ctx, browser, config)
}

// InitializeLoginPage 记录登录页面初始化结果。
func (l *loggedRuntime) InitializeLoginPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("initialize_login_page", time.Now(), &err)
	return l.Runtime.InitializeLoginPage(ctx, browser, config)
}

// OpenGreetingPage 记录打开打招呼页面的结果。
func (l *loggedRuntime) OpenGreetingPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("open_greeting_page", time.Now(), &err)
	return l.Runtime.OpenGreetingPage(ctx, browser, config)
}

// InitializeGreetingPage 记录打招呼页面初始化结果。
func (l *loggedRuntime) InitializeGreetingPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("initialize_greeting_page", time.Now(), &err)
	return l.Runtime.InitializeGreetingPage(ctx, browser, config)
}

// SelectPosition 记录岗位选择结果。
func (l *loggedRuntime) SelectPosition(ctx context.Context, browser model.Browser, config model.Config, position model.Position) (err error) {
	defer l.record("select_position", time.Now(), &err)
	return l.Runtime.SelectPosition(ctx, browser, config, position)
}

// ApplyBasicFilters 记录基础筛选结果。
func (l *loggedRuntime) ApplyBasicFilters(ctx context.Context, browser model.Browser, config model.Config, position model.Position) (err error) {
	defer l.record("apply_basic_filters", time.Now(), &err)
	return l.Runtime.ApplyBasicFilters(ctx, browser, config, position)
}

// FindCandidates 记录候选人列表查找结果。
func (l *loggedRuntime) FindCandidates(ctx context.Context, browser model.Browser, config model.Config, options model.CandidateListOptions) (items []model.Candidate, err error) {
	defer l.record("find_candidates", time.Now(), &err)
	return l.Runtime.FindCandidates(ctx, browser, config, options)
}

// ScrollToCandidate 记录滚动到候选人的结果。
func (l *loggedRuntime) ScrollToCandidate(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("scroll_to_candidate", time.Now(), &err)
	return l.Runtime.ScrollToCandidate(ctx, browser, config, candidate)
}

// OpenCandidateDetail 记录打开候选人详情的结果。
func (l *loggedRuntime) OpenCandidateDetail(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("open_candidate_detail", time.Now(), &err)
	return l.Runtime.OpenCandidateDetail(ctx, browser, config, candidate)
}

// ExtractCandidateDetail 记录提取候选人详情的结果，不写入简历正文。
func (l *loggedRuntime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (detail model.CandidateDetail, err error) {
	defer l.record("extract_candidate_detail", time.Now(), &err)
	return l.Runtime.ExtractCandidateDetail(ctx, browser, config, candidate)
}

// CleanCandidateDetailText 记录详情正文清理调用，不写入正文。
func (l *loggedRuntime) CleanCandidateDetailText(value string) string {
	defer l.record("clean_candidate_detail_text", time.Now(), nil)
	return l.Runtime.CleanCandidateDetailText(value)
}

// CloseCandidateDetail 记录关闭候选人详情的结果。
func (l *loggedRuntime) CloseCandidateDetail(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("close_candidate_detail", time.Now(), &err)
	return l.Runtime.CloseCandidateDetail(ctx, browser, config, candidate)
}

// GreetCandidate 记录打招呼动作结果。
func (l *loggedRuntime) GreetCandidate(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate, request model.GreetRequest) (err error) {
	defer l.record("greet_candidate", time.Now(), &err)
	return l.Runtime.GreetCandidate(ctx, browser, config, candidate, request)
}

// EnsureCandidateConversation 记录候选人会话确认结果。
func (l *loggedRuntime) EnsureCandidateConversation(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("ensure_candidate_conversation", time.Now(), &err)
	return l.Runtime.EnsureCandidateConversation(ctx, browser, config, candidate)
}

// RequestCandidateInfo 记录索要候选人资料的结果。
func (l *loggedRuntime) RequestCandidateInfo(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate, request model.CandidateInfoRequest) (err error) {
	defer l.record("request_candidate_info", time.Now(), &err)
	return l.Runtime.RequestCandidateInfo(ctx, browser, config, candidate, request)
}

// SendCandidateMessage 记录发送候选人消息结果，不记录消息正文。
func (l *loggedRuntime) SendCandidateMessage(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate, message string) (err error) {
	defer l.record("send_candidate_message", time.Now(), &err)
	return l.Runtime.SendCandidateMessage(ctx, browser, config, candidate, message)
}

// CloseCandidateConversation 记录关闭候选人会话结果。
func (l *loggedRuntime) CloseCandidateConversation(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("close_candidate_conversation", time.Now(), &err)
	return l.Runtime.CloseCandidateConversation(ctx, browser, config, candidate)
}

// FavoriteCandidate 记录收藏候选人的结果。
func (l *loggedRuntime) FavoriteCandidate(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("favorite_candidate", time.Now(), &err)
	return l.Runtime.FavoriteCandidate(ctx, browser, config, candidate)
}

// RejectCandidate 记录忽略候选人的结果。
func (l *loggedRuntime) RejectCandidate(ctx context.Context, browser model.Browser, config model.Config, candidate model.Candidate) (err error) {
	defer l.record("reject_candidate", time.Now(), &err)
	return l.Runtime.RejectCandidate(ctx, browser, config, candidate)
}

// AdvanceCandidateList 记录进入下一批候选人的结果。
func (l *loggedRuntime) AdvanceCandidateList(ctx context.Context, browser model.Browser, config model.Config, candidates []model.Candidate, options model.CandidateListOptions) (advanced bool, err error) {
	defer l.record("advance_candidate_list", time.Now(), &err)
	return l.Runtime.AdvanceCandidateList(ctx, browser, config, candidates, options)
}

// OpenMessagesPage 记录打开消息页面的结果。
func (l *loggedRuntime) OpenMessagesPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("open_messages_page", time.Now(), &err)
	return l.Runtime.OpenMessagesPage(ctx, browser, config)
}

// InitializeMessagesPage 记录消息页面初始化结果。
func (l *loggedRuntime) InitializeMessagesPage(ctx context.Context, browser model.Browser, config model.Config) (err error) {
	defer l.record("initialize_messages_page", time.Now(), &err)
	return l.Runtime.InitializeMessagesPage(ctx, browser, config)
}

// ScanUnreadConversations 记录未读会话扫描结果。
func (l *loggedRuntime) ScanUnreadConversations(ctx context.Context, browser model.Browser, config model.Config) (conversations []model.Conversation, err error) {
	defer l.record("scan_unread_conversations", time.Now(), &err)
	return l.Runtime.ScanUnreadConversations(ctx, browser, config)
}

// ReadConversation 记录读取会话结果，不记录聊天正文。
func (l *loggedRuntime) ReadConversation(ctx context.Context, browser model.Browser, config model.Config, conversation model.Conversation) (message string, err error) {
	defer l.record("read_conversation", time.Now(), &err)
	return l.Runtime.ReadConversation(ctx, browser, config, conversation)
}

// ReplyConversation 记录回复会话结果，不记录回复内容。
func (l *loggedRuntime) ReplyConversation(ctx context.Context, browser model.Browser, config model.Config, conversation model.Conversation, message string) (err error) {
	defer l.record("reply_conversation", time.Now(), &err)
	return l.Runtime.ReplyConversation(ctx, browser, config, conversation, message)
}
