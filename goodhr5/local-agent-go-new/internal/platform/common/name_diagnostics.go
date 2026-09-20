// Package common 文件作用：记录候选人姓名读取的两轮结果和失败后的页码，不记录个人资料。
package common

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// logCandidateNameRead 按同一次列表读取编号记录两轮缺失位置，序号从零开始。
func logCandidateNameRead(platformID string, readID int64, attempt int, items []contract.FindAllItem, elapsed time.Duration) {
	missing := make([]int, 0)
	for _, item := range items {
		if strings.TrimSpace(item.Fields["name"]) == "" {
			missing = append(missing, item.Index)
		}
	}
	log.Printf("event=candidate_name_read platform=%s read_id=%d attempt=%d returned_count=%d missing_count=%d missing_indices=%v duration_ms=%d", platformID, readID, attempt, len(items), len(missing), missing, elapsed.Milliseconds())
}

// logCandidatePage 仅在确定将停止后读取平台配置的当前页码，探测失败不改变原始错误。
func logCandidatePage(ctx context.Context, browser model.Browser, cfg model.Config, platformID string, readID int64) {
	selector, ok := cfg.Selectors["candidate.current_page"]
	if !ok || len(selector.Target.Selectors) == 0 {
		return
	}
	selector.TimeoutMS = 200
	items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, MaxItems: 1, ExpectedMissing: true})
	page := 0
	if err == nil && len(items) > 0 {
		page, _ = strconv.Atoi(strings.TrimSpace(items[0].Text))
	}
	log.Printf("event=candidate_name_page platform=%s read_id=%d observed_after_failure=true page=%d page_known=%t probe_failed=%t", platformID, readID, page, page > 0, err != nil)
}
