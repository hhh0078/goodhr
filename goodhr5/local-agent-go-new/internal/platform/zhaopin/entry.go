// Package zhaopin 文件作用：实现智联登录页、推荐页、消息页和页面初始化。
package zhaopin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenLoginPage 打开智联招聘登录页面。
func (r *Runtime) OpenLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenPage(ctx, browser, r.PlatformID(), cfg.LoginURL, "登录页", cfg.LoginURLs...)
}

// InitializeLoginPage 执行智联招聘登录页配置的初始化动作。
func (r *Runtime) InitializeLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.LoginInitActions)
}

// OpenGreetingPage 优先沿用已打开的智联推荐页或搜索页，没有匹配页面时仍打开原推荐页。
func (r *Runtime) OpenGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.EntryURL, "打招呼页", cfg.EntryURLs...)
}

// InitializeGreetingPage 关闭智联推荐页可选弹框。
func (r *Runtime) InitializeGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.GreetingInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.GreetingInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "entry.dismiss")
}

// OpenMessagesPage 打开智联招聘消息页面。
func (r *Runtime) OpenMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.MessagesURL, "消息页", cfg.MessagesURLs...)
}

// InitializeMessagesPage 关闭智联消息页可选提示。
func (r *Runtime) InitializeMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.MessageInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.MessageInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "message.dismiss")
}
