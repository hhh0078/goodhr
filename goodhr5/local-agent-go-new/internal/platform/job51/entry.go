// Package job51 文件作用：实现前程无忧登录页、推荐页、消息页和页面初始化。
package job51

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenLoginPage 打开前程无忧登录页面。
func (r *Runtime) OpenLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenPage(ctx, browser, r.PlatformID(), cfg.LoginURL, "登录页", cfg.LoginURLs...)
}

// InitializeLoginPage 执行前程无忧登录页配置的初始化动作。
func (r *Runtime) InitializeLoginPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.LoginInitActions)
}

// OpenGreetingPage 打开前程无忧推荐页面。
func (r *Runtime) OpenGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.EntryURL, "打招呼页", cfg.EntryURLs...)
}

// InitializeGreetingPage 关闭遗留弹层并等待当前推荐页或手动搜索结果准备好。
func (r *Runtime) InitializeGreetingPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	manualSearch, err := common.CurrentPageUsesManualSearch(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if err := r.CloseCandidateDetail(ctx, browser, cfg, model.Candidate{}); err != nil {
		return fmt.Errorf("前程无忧上一份简历还没收好，请关闭详情后再开始：%w", err)
	}
	if err := common.CloseCandidatePanels(ctx, browser, cfg); err != nil {
		return fmt.Errorf("前程无忧遗留聊天还没收好，请关闭后再开始：%w", err)
	}
	if manualSearch {
		return waitForCandidateResults(ctx, browser, cfg)
	}
	if len(cfg.GreetingInitActions) > 0 {
		if err := common.ApplyConfiguredActions(ctx, browser, cfg, cfg.GreetingInitActions); err != nil {
			return err
		}
	} else if err := common.ClickOptional(ctx, browser, cfg, "entry.dismiss"); err != nil {
		return err
	}
	if err := waitForGreetingPageReady(ctx, browser, cfg); err != nil {
		return err
	}
	return waitForCandidateResults(ctx, browser, cfg)
}

// waitForGreetingPageReady 等待SPA当前页面的岗位标题或明确空态，不能把DOM加载事件当成业务页面可操作。
func waitForGreetingPageReady(ctx context.Context, browser model.Browser, cfg model.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	for {
		current, found, err := common.ReadOptional(ctx, browser, cfg, "position.current")
		if err != nil {
			return err
		}
		if found && strings.TrimSpace(current) != "" {
			return nil
		}
		empty, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.empty")
		if err != nil {
			return err
		}
		if empty {
			loading, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.loading")
			if err != nil {
				return err
			}
			if !loading {
				return nil
			}
		}
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("前程无忧岗位页面还没准备好，请确认已登录后再试：%w", ctx.Err())
		case <-timer.C:
		}
	}
}

// OpenMessagesPage 打开前程无忧消息页面。
func (r *Runtime) OpenMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	return common.OpenVerifiedPage(ctx, browser, r.PlatformID(), cfg.MessagesURL, "消息页", cfg.MessagesURLs...)
}

// InitializeMessagesPage 关闭前程无忧消息页可选提示。
func (r *Runtime) InitializeMessagesPage(ctx context.Context, browser model.Browser, cfg model.Config) error {
	if len(cfg.MessageInitActions) > 0 {
		return common.ApplyConfiguredActions(ctx, browser, cfg, cfg.MessageInitActions)
	}
	return common.ClickOptional(ctx, browser, cfg, "message.dismiss")
}
