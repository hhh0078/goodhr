// Package hliepin 文件作用：实现猎聘猎头端登录页、找人页、消息页和页面初始化。
package hliepin

import (
	"context"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenLoginPage 打开猎聘猎头端登录页面。
func (r *Runtime) OpenLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenPage(ctx, browser, r.PlatformID(), cfg.LoginURL, "登录页")
}

// InitializeLoginPage 执行猎聘猎头端登录页配置的初始化动作。
func (r *Runtime) InitializeLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.LoginInitActions)
}

// OpenGreetingPage 打开猎聘猎头端找人页面。
func (r *Runtime) OpenGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.EntryURL, "打招呼页")
}

// InitializeGreetingPage 关闭猎聘猎头端找人页可选弹框。
func (r *Runtime) InitializeGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.GreetingInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.GreetingInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "entry.dismiss")
}

// OpenMessagesPage 打开猎聘猎头端消息页面。
func (r *Runtime) OpenMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.MessagesURL, "消息页")
}

// InitializeMessagesPage 关闭猎聘猎头端消息页可选提示。
func (r *Runtime) InitializeMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.MessageInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.MessageInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "message.dismiss")
}
