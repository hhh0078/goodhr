// Package liepin 本文件只为简历采集加载来源卡片，保留当前位置供附件滚轮继续，不执行普通聊天的回最新端动作。
package liepin

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// liepinRecentResumeSource 只读末八条，证明最新附件的发送人、删除时间和同分钟次序后，绑定实际将点击的那条消息。
// 消息窗口没有覆盖来源或旧平台配置无法约束点击时，由调用者继续原历史核验流程。
func liepinRecentResumeSource(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.Config, bool, error) {
	if err := ctx.Err(); err != nil {
		return cfg, false, err
	}
	if strings.TrimSpace(snapshot.ResumeSourceMessageID) == "" {
		return cfg, false, fmt.Errorf("简历来源消息还没确认，暂不打开附件")
	}
	row := cfg.Selectors["message.item"]
	entry := cfg.Selectors["message.resume_attachment_entry"]
	// 只使用同一聊天框内、以消息行为最后父层的 CSS 配置，兼容其他结构的旧配置。
	if len(row.Alternatives) != 0 || len(entry.Alternatives) != 0 || len(row.Target.Selectors) != 1 || row.Target.Selectors[0].Type != "css" ||
		len(entry.Parents) != len(row.Parents)+1 || !reflect.DeepEqual(row.Parents, entry.Parents[:len(row.Parents)]) {
		return cfg, false, nil
	}
	parent := entry.Parents[len(entry.Parents)-1]
	if len(parent.Selectors) != 1 || parent.Selectors[0].Type != "css" {
		return cfg, false, nil
	}
	items, err := common.ReadConfiguredConversationMessageTail(ctx, browser, cfg, 8)
	if err != nil {
		return cfg, false, err
	}
	if len(items) == 0 || items[len(items)-1].Index+1 != items[len(items)-1].TotalMatches {
		return cfg, false, nil
	}
	for i, item := range items {
		if item.Index != items[0].Index+i || item.TotalMatches != items[0].TotalMatches {
			return cfg, false, nil
		}
	}
	messages, err := liepinMessages(items)
	if err != nil {
		return cfg, false, err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.MessageType != "resume" {
			continue
		}
		item := items[i]
		if m.Direction != "candidate" || (snapshot.HistoryAfter != nil && (m.SentAt == nil || !m.SentAt.After(*snapshot.HistoryAfter))) {
			return cfg, false, fmt.Errorf("最后一张附件的发送人或时间与本次来源不符，暂不下载")
		}
		if cid := parseLiepinCandidateID(item.Fields["candidate_meta"]); cid != "" && cid != snapshot.PlatformCandidateID {
			return cfg, false, fmt.Errorf("附件上的候选人编号与当前聊天不符，暂不下载")
		}
		if m.PlatformMessageID == "" {
			// 首个已加载节点可能还有未翻出的同分钟、同文卡片，必须看到一个更早的分钟。
			covered := m.SentAt != nil && slices.ContainsFunc(messages[:i], func(previous model.ConversationMessage) bool {
				return previous.SentAt != nil && previous.SentAt.Before(*m.SentAt)
			})
			if !covered {
				return cfg, false, nil
			}
		}
		if firstLiepinValue(m.PlatformMessageID, m.Key) != snapshot.ResumeSourceMessageID {
			return cfg, false, fmt.Errorf("最后一份简历与本次来源不一致，请重新同步会话")
		}
		// 在“最后一张附件”上叠加当前消息行序号，核验后若又收到一张同文卡，两项条件就无法同时命中。
		parent.Selectors = slices.Clone(parent.Selectors)
		parent.Selectors[0].Value += fmt.Sprintf(":is(:nth-match(%s, %d))", row.Target.Selectors[0].Value, item.Index+1)
		if clock := strings.TrimSpace(item.Fields["message_time"]); clock != "" {
			parent.Texts = append(slices.Clone(parent.Texts), clock)
		}
		entry.Parents = slices.Clone(entry.Parents)
		entry.Parents[len(entry.Parents)-1] = parent
		cfg.Selectors = maps.Clone(cfg.Selectors)
		cfg.Selectors["message.resume_attachment_entry"] = entry
		return cfg, true, nil
	}
	return cfg, false, nil
}

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
