// Package boss 本文件通过已核实原页、候选人和末条编号执行回车优先发送，不在未知送达时补点。
package boss

import (
	"context"
	"fmt"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// sendBossAutoReplyMessage 复用公共焦点与草稿守卫，发送前不再重新打开可能已经切换的聊天。
func sendBossAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, message string) error {
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return err
	}
	page, err := bossAutoReplyPageURL(ctx, browser, cfg)
	if err != nil {
		return err
	}
	if snapshot.DeliveryPageURL != "" && page != snapshot.DeliveryPageURL {
		return fmt.Errorf("准备发送期间招聘页面已变化")
	}
	inspect := func() (model.ConversationMessage, error) {
		if err := verifyBossOpenAction(ctx, browser, cfg, snapshot, page); err != nil {
			return model.ConversationMessage{}, err
		}
		return readBossLatestAutoReplyMessage(ctx, browser, cfg)
	}
	return common.SendAutoReplyEnterFirst(ctx, browser, cfg, page, message, snapshot.ExpectedSendMessageKey, inspect)
}

// verifyBossOpenAction 只核对原标签与当前聊天，不因错人、关窗或切页而偷偷恢复旧会话。
func verifyBossOpenAction(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, page string) error {
	if page == "" || (snapshot.DeliveryPageURL != "" && page != snapshot.DeliveryPageURL) {
		return fmt.Errorf("招聘页面已变化，这一步暂不继续")
	}
	// 同一个实时核验已经覆盖原页、姓名、原生编号和岗位，避免每个动作重复读三轮身份。
	snapshot.DeliveryPageURL = page
	matched, err := bossAutoReplyConversationMatches(ctx, browser, cfg, snapshot)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("当前聊天身份或岗位已变化，这一步暂不继续")
	}
	return nil
}
