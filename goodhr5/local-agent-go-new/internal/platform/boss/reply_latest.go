// Package boss 本文件从聊天末尾按需扩窗，完整核对时间组后才返回可用于发送防重的内部消息键。
package boss

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readBossLatestAutoReplyMessage 只读取末个双方消息的完整时间组，系统通知不会成为候选人新来信。
func readBossLatestAutoReplyMessage(ctx context.Context, browser model.Browser, cfg model.Config) (model.ConversationMessage, error) {
	_, messages, err := readBossRecentWindow(ctx, browser, cfg, 1)
	if err != nil {
		return model.ConversationMessage{}, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Direction == "candidate" || messages[i].Direction == "self" {
			return messages[i], nil
		}
	}
	return model.ConversationMessage{}, fmt.Errorf("BOSS聊天尾部没有可核实的双方消息")
}

// readBossRecentMessages 为资料申请读取最近完整时间组，重复卡片不会因小窗口被重编号。
func readBossRecentMessages(ctx context.Context, browser model.Browser, cfg model.Config, count int) ([]model.ConversationMessage, error) {
	_, messages, err := readBossRecentWindow(ctx, browser, cfg, count)
	return messages, err
}

// readBossRecentWindow 先读少量末条，时间组不完整时扩窗，确实无法区分才向上核实历史顶部。
func readBossRecentWindow(ctx context.Context, browser model.Browser, cfg model.Config, count int) ([]contract.FindAllItem, []model.ConversationMessage, error) {
	count = min(5000, max(1, count))
	for attempt := 0; attempt < 3; attempt++ {
		for size := count; ; size = min(5000, max(size+1, size*2)) {
			items, err := common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, size)
			if err != nil {
				return nil, nil, err
			}
			if len(items) == 0 {
				return nil, nil, fmt.Errorf("BOSS聊天暂时没有读到消息")
			}
			messages, err := bossMessages(items, time.Now())
			if err != nil {
				return nil, nil, err
			}
			start := bossStableGroupStart(items, messages, false)
			last := -1
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Direction == "candidate" || messages[i].Direction == "self" {
					last = i
					break
				}
			}
			if last >= start {
				edges, err := readBossHistoryEdges(ctx, browser, cfg)
				if err != nil {
					return nil, nil, err
				}
				if len(edges) == 2 && sameBossMessageEdge(items[len(items)-1], edges[1]) {
					return items, messages[start:], nil
				}
				break // 读正文期间出现新消息，重新扩窗，不把旧尾部当作依据。
			}
			if len(items) < size || size == 5000 {
				// 页面只加载同一个时间组时，窗口序号不足以证明重复次数，先用真实滚轮核实顶部。
				full, err := readBossHistoryWindow(ctx, browser, cfg, nil, 5000, nil)
				if err != nil {
					return nil, nil, err
				}
				if !full.HistoryComplete {
					return nil, nil, fmt.Errorf("BOSS最后时间组尚未完整，暂不猜测重复消息编号")
				}
				return nil, full.Messages, nil
			}
		}
	}
	return nil, nil, fmt.Errorf("BOSS聊天尾部仍在变化，稍后再核对")
}
