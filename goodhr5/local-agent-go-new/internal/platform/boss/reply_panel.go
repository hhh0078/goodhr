// Package boss 本文件保留正常会话，仅在明确收尾时关闭附件并一键收起联系人与聊天。
package boss

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// initializeBossAutoReplyPage 正常轮询复用原列表和聊天，只按Esc优先收起已核实的附件遮层。
func initializeBossAutoReplyPage(ctx context.Context, browser model.Browser, cfg model.Config, target *model.AutoReplyConversationSnapshot) error {
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if _, configured := cfg.Selectors["message.attachment_preview"]; !configured {
		return nil
	}
	var verify func(context.Context) error
	if target != nil {
		verify = func(ctx context.Context) error {
			return verifyBossOpenAction(ctx, browser, cfg, *target, page)
		}
	}
	return common.ClosePanelEscapeFirst(ctx, browser, cfg, "message.attachment_preview", "message.attachment_preview_close", page, verify)
}

// closeBossAutoReplyConversation 附件先Esc，再点列表的一键收起，同时以两块面板隐藏作为成功证据。
func closeBossAutoReplyConversation(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) error {
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if err = initializeBossAutoReplyPage(ctx, browser, cfg, &snapshot); err != nil {
		return err
	}
	drawer, chat, err := bossConversationPanels(ctx, browser, cfg, page)
	if err != nil || (!drawer && !chat) {
		return err
	}
	if chat {
		if err = verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
			return err
		}
	}
	if drawer {
		collapse, err := common.RequiredSelector(cfg, "message.drawer_collapse")
		if err != nil {
			return err
		}
		if _, err = browser.Click(ctx, contract.ElementClickRequest{Selector: collapse}); contract.IsSessionUnavailable(err) {
			return err
		}
		// 一键收起可能有很短的动画；立即查状态，只有仍在才做有上限复核。
		for attempt := 0; attempt < 4; attempt++ {
			drawer, chat, probeErr := bossConversationPanels(ctx, browser, cfg, page)
			if probeErr != nil {
				return probeErr
			}
			if !drawer && !chat {
				return nil
			}
			if err != nil || attempt == 3 {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(80 * time.Millisecond):
			}
		}
	}
	// 一键收起未生效才走各自的安全关闭备用，不再点第二次收起按钮。
	verify := func(ctx context.Context) error { return verifyBossOpenAction(ctx, browser, cfg, snapshot, page) }
	if err = common.ClosePanelEscapeFirst(ctx, browser, cfg, "candidate.chat_modal", "candidate.chat_close", page, verify); err != nil {
		return err
	}
	if err = common.ClosePanelEscapeFirst(ctx, browser, cfg, "message.drawer", "message.drawer_collapse", page, nil); err != nil {
		return err
	}
	drawer, chat, err = bossConversationPanels(ctx, browser, cfg, page)
	if err == nil && (drawer || chat) {
		return fmt.Errorf("联系人列表或聊天还没收好，暂不继续")
	}
	return err
}

// bossConversationPanels 只在原页探测联系人和聊天，不把切页当作面板已经关闭。
func bossConversationPanels(ctx context.Context, browser model.Browser, cfg model.Config, page string) (bool, bool, error) {
	current, err := common.UniqueCurrentPageURL(ctx, browser)
	if err != nil || current != page {
		if err == nil {
			err = fmt.Errorf("收起会话期间页面已变化")
		}
		return false, false, err
	}
	drawer, err := common.ProbeSelectorExists(ctx, browser, cfg, "message.drawer")
	if err != nil {
		return false, false, err
	}
	chat, err := common.ProbeSelectorExists(ctx, browser, cfg, "candidate.chat_modal")
	return drawer, chat, err
}
