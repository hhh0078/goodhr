// Package auto_reply 实现与主动打招呼完全独立的自动回复主流程。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

const defaultCheckpointLimit = 3

// Browser 定义自动回复主流程需要的浏览器会话和平台标准能力。
type Browser interface {
	model.Browser
	StartBrowser(context.Context, contract.BrowserStartRequest) (contract.BrowserStatus, error)
}

// Responder 定义阶段6实现的 AI 工具循环入口。
type Responder interface {
	Reply(context.Context, ReplyContext) (ReplyDecision, error)
}

// ReplyContext 表示一次自动回复决策已经完成同步和身份校验的全部上下文。
type ReplyContext struct {
	TaskID            string
	Credentials       cloud.AgentCredentials
	Position          cloud.AutoReplyPositionSnapshot
	Conversation      cloud.AutoReplyConversation
	CandidateState    cloud.AutoReplyCandidateState
	Messages          []cloud.AutoReplyMessage
	ConfirmationItems []cloud.CandidateConfirmationItem
	PageSnapshot      model.AutoReplyConversationSnapshot
	Resume            *model.AutoReplyResumeBundle
	BasedOnMessageKey string
}

// ReplyDecision 表示 AI 工具循环最终决定发送回复或转人工。
type ReplyDecision struct {
	Reply        string
	ManualReason string
	ReasonKey    string
}

// Flow 组装自动回复主流程依赖。
type Flow struct {
	Browser        Browser
	Store          *storage.Store
	Cloud          *cloud.Client
	Responder      Responder
	Logger         shared.Logger
	DownloadsDir   string
	ExtensionPaths func() []string
}

type flowStep struct {
	name  string
	label string
	run   func(context.Context) error
}

// Run 按平铺步骤复用浏览器、确认当前平台、初始化页面并持续处理未读会话。
func (f *Flow) Run(ctx context.Context, prepared shared.PreparedTask, runtime model.Runtime) (shared.Stats, error) {
	stats := shared.Stats{}
	replyRuntime, ok := runtime.(model.AutoReplyRuntime)
	if !ok {
		return stats, fmt.Errorf("%s 自动回复页面能力还没有准备完整", prepared.Platform.Name)
	}
	if err := f.validateDependencies(); err != nil {
		return stats, err
	}
	steps := []flowStep{
		{name: "start_browser", label: "启动增强浏览器", run: func(ctx context.Context) error {
			return f.startBrowser(ctx, prepared)
		}},
		{name: "select_current_platform_page", label: "确认当前招聘页面", run: func(ctx context.Context) error {
			return f.selectCurrentPlatformPage(ctx, prepared.Platform)
		}},
		{name: "initialize_auto_reply_page", label: "整理自动回复页面", run: func(ctx context.Context) error {
			return replyRuntime.InitializeAutoReplyPage(ctx, f.Browser, prepared.Platform)
		}},
		{name: "process_auto_reply_loop", label: "处理候选人新消息", run: func(ctx context.Context) error {
			return f.runLoop(ctx, prepared, runtime, replyRuntime, &stats)
		}},
	}
	for _, step := range steps {
		if shared.GracefulStopRequested(ctx) {
			return stats, nil
		}
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, step.name, "start", startedAt, nil)
		if err := step.run(ctx); err != nil {
			f.log(prepared.Request.TaskID, step.name, "failed", startedAt, err)
			return stats, fmt.Errorf("%s没处理成功：%w", step.label, err)
		}
		f.log(prepared.Request.TaskID, step.name, "success", startedAt, nil)
	}
	return stats, nil
}

// validateDependencies 在流程开始前确认必需依赖已经组装。
func (f *Flow) validateDependencies() error {
	if f.Browser == nil || f.Store == nil || f.Cloud == nil {
		return fmt.Errorf("自动回复运行组件没有准备完整")
	}
	if f.Responder == nil {
		return fmt.Errorf("自动回复 AI 处理器没有准备完整")
	}
	return nil
}

// startBrowser 使用当前 Profile 启动或复用 CloakBrowser，不新建第二个浏览器窗口。
func (f *Flow) startBrowser(ctx context.Context, prepared shared.PreparedTask) error {
	headless := prepared.Request.Headless
	humanize := true
	_, err := f.Browser.StartBrowser(ctx, contract.BrowserStartRequest{
		UserDataDir: prepared.ProfilePath, DownloadsPath: f.DownloadsDir,
		Headless: &headless, Humanize: &humanize, Locale: "zh-CN", Timezone: "Asia/Shanghai",
		ExtensionPaths: f.extensionPaths(),
	})
	return err
}

// extensionPaths 返回本次启动浏览器时发现的有效扩展目录。
func (f *Flow) extensionPaths() []string {
	if f.ExtensionPaths == nil {
		return nil
	}
	return f.ExtensionPaths()
}

// credentials 返回本次任务访问自动回复敏感接口所需的登录和设备凭证。
func credentials(prepared shared.PreparedTask) cloud.AgentCredentials {
	return cloud.AgentCredentials{
		Token:     strings.TrimSpace(prepared.Request.Token),
		MachineID: strings.TrimSpace(prepared.MachineID),
	}
}

// log 输出自动回复步骤日志。
func (f *Flow) log(taskID string, step string, status string, startedAt time.Time, err error) {
	if f.Logger != nil {
		f.Logger.Step(taskID, "auto_reply", step, status, startedAt, err)
	}
}
