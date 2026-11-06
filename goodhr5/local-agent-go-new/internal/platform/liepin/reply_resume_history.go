// Package liepin 本文件只为简历采集加载来源卡片，保留当前位置供附件滚轮继续，不执行普通聊天的回最新端动作。
package liepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// prepareLiepinResumeSource 用标准元素探测及真实滚轮寻找简历，出现卡片后才完整读一次并核验来源指纹。
func prepareLiepinResumeSource(ctx context.Context, browser model.Browser, cfg model.Config, sourceMessageID string) error {
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return fmt.Errorf("简历来源消息还没确认，暂不打开附件")
	}
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return err
	}
	entry.State, entry.TimeoutMS = "attached", 600
	anchor, err := common.RequiredSelector(cfg, "message.history_scroll")
	if err != nil {
		return err
	}
	previous, checked := "", ""
	stable, blocked := 0, 0
	var progress contract.ScrollResult
	var fullItems []contract.FindAllItem
	var full liepinConversationHistory
	for round := 0; round < 120; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		head, tail, signature, err := readLiepinHistoryEdges(ctx, browser, cfg)
		if err != nil {
			return err
		}
		if round > 0 {
			if progress.ProgressTargetVisible == nil || progress.ProgressTargetMoved == nil {
				return fmt.Errorf("浏览器工具没有返回简历历史的真实滚轮位置")
			}
			if !*progress.ProgressTargetMoved && signature == previous {
				if *progress.ProgressTargetVisible {
					stable++
					blocked = 0
				} else {
					stable = 0
					blocked++
				}
			} else {
				stable, blocked = 0, 0
			}
			if blocked >= liepinHistoryStableReadAttempts {
				return fmt.Errorf("简历历史尚未到顶且滚轮已停住，暂不继续采集")
			}
		}
		atTop := stable >= liepinHistoryStableReadAttempts
		probe, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: entry, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
		if err != nil && !common.IsElementMissing(err) {
			return err
		}
		if len(probe) > 0 {
			if signature != checked {
				fullItems, full, err = readLiepinFullHistory(ctx, browser, cfg)
				if err != nil {
					return err
				}
				checked = signature
			}
			last := fullItems[len(fullItems)-1]
			if last.TotalMatches != tail.TotalMatches || liepinHistoryItemKey(last) != liepinHistoryItemKey(tail) || (fullItems[0].Index == head.Index && liepinHistoryItemKey(fullItems[0]) != liepinHistoryItemKey(head)) {
				previous, checked = "", ""
				stable, blocked = 0, 0
				continue
			}
			resumeIndex := -1
			for index := len(full.Messages) - 1; index >= 0; index-- {
				if full.Messages[index].Direction == "candidate" && full.Messages[index].MessageType == "resume" {
					resumeIndex = index
					break
				}
			}
			if resumeIndex < 0 {
				return fmt.Errorf("附件入口没有对应到候选人的简历消息，暂不采集")
			}
			message := full.Messages[resumeIndex]
			if atTop || message.PlatformMessageID != "" || resumeIndex >= liepinLeadingMessageGroupEnd(full.Messages) {
				if firstLiepinValue(message.PlatformMessageID, message.Key) != sourceMessageID {
					return fmt.Errorf("页面最后一份简历与本次来源不一致，请重新同步会话")
				}
				return nil
			}
		}
		if atTop {
			return fmt.Errorf("已核实聊天顶部，但没有找到本次简历来源")
		}
		if tail.TotalMatches >= 5000 {
			return fmt.Errorf("简历来源超出当前可核实的历史范围，暂不猜测来源")
		}
		headSelector, err := common.IndexedSelector(cfg, "message.item", head.Index)
		if err != nil {
			return err
		}
		waitMS := 300
		if progress.ProgressTargetVisible != nil && *progress.ProgressTargetVisible {
			waitMS = 800
		}
		previous = signature
		progress, err = browser.Scroll(ctx, contract.ScrollRequest{WheelAnchor: &anchor, ProgressTarget: &headSelector, Distance: -liepinHistoryWheelDistance, MaxAttempts: 1, WaitMS: waitMS})
		if err != nil {
			return fmt.Errorf("加载简历来源历史失败：%w", err)
		}
	}
	return fmt.Errorf("还没核实简历来源，稍后继续")
}
