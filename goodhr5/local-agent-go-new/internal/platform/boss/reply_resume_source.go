// Package boss 本文件将已核实的内部消息键定位到当前可见附件行，阻止旧附件或列表变化后的错位下载。
package boss

import (
	"context"
	"fmt"
	"maps"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// pinBossResumeSource 优先核对近期完整时间组，确有需要才补历史，并把附件入口限定到当前来源行。
func pinBossResumeSource(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot) (model.Config, error) {
	return pinBossResumeSourceWindow(ctx, browser, cfg, snapshot, true)
}

// pinBossResumeSourceWindow 复用同一来源校验；位置纠正后只读已加载消息，不能因来源消失再补滚历史。
func pinBossResumeSourceWindow(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, allowHistoryScroll bool) (model.Config, error) {
	if snapshot.ResumeSourceMessageID == "" {
		return cfg, fmt.Errorf("BOSS简历来源消息尚未核实，暂不下载")
	}
	if err := ensureBossAutoReplyConversation(ctx, browser, cfg, snapshot); err != nil {
		return cfg, err
	}
	items, messages, err := readBossRecentWindow(ctx, browser, cfg, 8)
	if err != nil {
		return cfg, err
	}
	matched := false
	windowVerified := len(items) > 0 // 非空原窗口已由readBossRecentWindow核对真实首尾，期间未执行任何输入。
	for _, message := range messages {
		matched = matched || message.Key == snapshot.ResumeSourceMessageID
	}
	if !matched {
		windowVerified = false
		if allowHistoryScroll {
			history, err := readBossHistoryWindow(ctx, browser, cfg, nil, 5000, snapshot.HistoryAfter)
			if err != nil {
				return cfg, err
			}
			for _, message := range history.Messages {
				matched = matched || message.Key == snapshot.ResumeSourceMessageID
			}
			if !matched {
				return cfg, fmt.Errorf("BOSS当前聊天没有找到本轮简历来源，暂不下载")
			}
		}
		items = nil
	}
	if len(items) == 0 {
		windowVerified = false
		items, err = common.ReadConfiguredConversationMessages(ctx, browser, cfg)
		if err != nil {
			return cfg, err
		}
	}
	full, err := bossMessages(items, time.Now())
	if err != nil {
		return cfg, err
	}
	index := -1
	for i, message := range full {
		if message.Key != snapshot.ResumeSourceMessageID {
			continue
		}
		if index >= 0 || message.Direction != "candidate" || message.MessageType != "resume" || (snapshot.HistoryAfter != nil && (message.SentAt == nil || !message.SentAt.After(*snapshot.HistoryAfter))) {
			return cfg, fmt.Errorf("BOSS附件的发送人、时间或来源无法核实，暂不下载")
		}
		index = i
	}
	if index < 0 {
		return cfg, fmt.Errorf("BOSS简历来源的时间组已变化，暂不下载")
	}
	if !windowVerified {
		// 补历史后重新读取了正文，这份新窗口必须取得自己的变化见证，不能沿用此前的首尾证明。
		edges, err := readBossHistoryEdges(ctx, browser, cfg)
		if err != nil {
			return cfg, err
		}
		if len(edges) != 2 || !sameBossMessageEdge(items[len(items)-1], edges[1]) {
			return cfg, fmt.Errorf("准备下载时BOSS聊天已变化，先重新核对来源")
		}
	}
	row, err := pinBossMessageRow(cfg, items[index], items[index].Fields["resume_card"], snapshot.PlatformCandidateID)
	if err != nil {
		return cfg, err
	}
	entry, err := common.RequiredSelector(cfg, "message.resume_attachment_entry")
	if err != nil {
		return cfg, err
	}
	entry.Parents = nil
	entry = entry.WithinSelector(row)
	cfg.Selectors = maps.Clone(cfg.Selectors)
	cfg.Selectors["message.resume_attachment_entry"] = entry
	return cfg, nil
}
