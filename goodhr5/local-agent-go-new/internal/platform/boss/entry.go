// Package boss 文件作用：实现 Boss 登录页、打招呼页、消息页和页面初始化。
package boss

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenLoginPage 打开 Boss 登录页面。
func (r *Runtime) OpenLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenPage(ctx, browser, r.PlatformID(), cfg.LoginURL, "登录页")
}

// InitializeLoginPage 执行 Boss 登录页配置的初始化动作。
func (r *Runtime) InitializeLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.LoginInitActions)
}

// OpenGreetingPage 打开 Boss 推荐牛人页面。
func (r *Runtime) OpenGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.EntryURL, "打招呼页")
}

// InitializeGreetingPage 关闭 Boss 推荐页可能出现的确认弹框。
func (r *Runtime) InitializeGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.GreetingInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.GreetingInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "entry.dismiss")
}

// OpenMessagesPage 打开 Boss 消息页面。
func (r *Runtime) OpenMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.MessagesURL, "消息页")
}

// InitializeMessagesPage 关闭 Boss 消息页可能出现的提示。
func (r *Runtime) InitializeMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.MessageInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.MessageInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "message.dismiss")
}
