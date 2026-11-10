// Package boss 本文件按稳定会话编号核对全部联系人，仅补读发生变化的可见行。
package boss

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readBossContacts 每轮只读姓名、原生编号与前后核验过的卡片正文；头像仅补无编号置顶行，不让置顶数量清空原生行缓存。
func (r *Runtime) readBossContacts(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	selector, err := common.RequiredSelector(cfg, "message.contact_item")
	if err != nil {
		return nil, err
	}
	thread, hasThread := cfg.ConversationFields["thread_id"]
	name, hasName := cfg.ConversationFields["name"]
	if !hasThread || !hasName {
		return nil, fmt.Errorf("BOSS联系人缺少原生会话编号或姓名配置，暂不采集")
	}
	limit := cfg.MaxItems
	if limit <= 0 {
		limit = 100
	}
	request := contract.ElementFindAllRequest{Selector: selector, Fields: map[string]contract.SelectorSpec{"thread_id": thread, "name": name}, MaxItems: min(5000, limit), ExpectedMissing: true}
	for attempt := 0; attempt < 2; attempt++ {
		rows, readErr := readBossContactSnapshot(ctx, browser, request)
		if readErr != nil {
			return nil, readErr
		}
		for i, row := range rows {
			if strings.TrimSpace(row.Fields["name"]) == "" || strings.TrimSpace(row.Text) == "" {
				return nil, fmt.Errorf("BOSS联系人姓名或卡片正文尚未读完整，暂不采集")
			}
			fresh := cloneBossContactRow(row)
			id := strings.TrimSpace(row.Fields["thread_id"])
			if old, ok := r.contactRows[id]; id != "" && ok && sameBossContactRow(old, row) && strings.TrimSpace(old.Fields["name"]) == strings.TrimSpace(row.Fields["name"]) {
				fresh = cloneBossContactRow(old)
				fresh.Index, fresh.TotalMatches, fresh.Text = row.Index, row.TotalMatches, row.Text
				fresh.Fields["thread_id"], fresh.Fields["name"] = row.Fields["thread_id"], row.Fields["name"]
			}
			// 卡片正文仅用于列表活动摘要和水位；真正的候选人消息仍由聊天历史读取，不能将摘要当入站正文。
			fresh.Fields["last_message"] = row.Text
			rows[i] = fresh
		}
		stable, readErr := readBossPinnedContactAvatars(ctx, browser, cfg, request, rows)
		if readErr != nil {
			return nil, readErr
		}
		if !stable {
			continue
		}
		conversations, parseErr := bossConversations(rows)
		if parseErr != nil {
			return nil, parseErr
		}
		cache := make(map[string]contract.FindAllItem, len(rows))
		for _, row := range rows {
			if id := strings.TrimSpace(row.Fields["thread_id"]); id != "" {
				cache[id] = cloneBossContactRow(row)
			}
		}
		r.contactRows = cache
		return conversations, nil
	}
	return nil, fmt.Errorf("BOSS联系人列表还在变化，未核实置顶行身份，稍后再试")
}

// readBossPinnedContactAvatars 将连续无编号行合成窗口补读，逐行复核序号、总数、姓名和正文；头像不跨轮缓存。
func readBossPinnedContactAvatars(ctx context.Context, browser model.Browser, cfg model.Config, request contract.ElementFindAllRequest, rows []contract.FindAllItem) (bool, error) {
	for start := 0; start < len(rows); {
		if strings.TrimSpace(rows[start].Fields["thread_id"]) != "" {
			start++
			continue
		}
		avatar, ok := cfg.ConversationFields["avatar_url"]
		if !ok {
			return false, fmt.Errorf("BOSS置顶联系人缺少头像核身配置，暂不采集")
		}
		end := start + 1
		for end < len(rows) && strings.TrimSpace(rows[end].Fields["thread_id"]) == "" {
			end++
		}
		detail := request
		detail.StartIndex, detail.MaxItems = rows[start].Index, end-start
		detail.Fields = map[string]contract.SelectorSpec{"thread_id": request.Fields["thread_id"], "name": request.Fields["name"], "avatar_url": avatar}
		full, err := browser.FindAll(ctx, detail)
		if common.IsElementMissing(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if len(full) != end-start {
			return false, nil
		}
		for offset, row := range full {
			original := rows[start+offset]
			if row.Index != original.Index || row.TotalMatches != original.TotalMatches || !sameBossContactRow(original, row) || strings.TrimSpace(row.Fields["name"]) != strings.TrimSpace(original.Fields["name"]) || strings.TrimSpace(row.Fields["avatar_url"]) == "" {
				return false, nil
			}
			rows[start+offset].Fields["avatar_url"] = strings.TrimSpace(row.Fields["avatar_url"])
		}
		start = end
	}
	return true, nil
}

// cloneBossContactRow 复制临时行字段，补读失败或调用方修改结果时不能污染上一份已核实缓存。
func cloneBossContactRow(row contract.FindAllItem) contract.FindAllItem {
	fields := make(map[string]string, len(row.Fields))
	for key, value := range row.Fields {
		fields[key] = value
	}
	row.Fields = fields
	return row
}

// readBossContactSnapshot 只在超过首窗或发生重排时补一次完整读取，缺行和超上限不能当作完整列表。
func readBossContactSnapshot(ctx context.Context, browser model.Browser, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	for attempt := 0; attempt < 2; attempt++ {
		rows, err := browser.FindAll(ctx, request)
		if common.IsElementMissing(err) {
			return []contract.FindAllItem{}, nil
		}
		if err != nil {
			return rows, err
		}
		if len(rows) == 0 {
			return []contract.FindAllItem{}, nil
		}
		total := rows[0].TotalMatches
		if total > 5000 {
			return nil, fmt.Errorf("BOSS联系人超过单次可核实范围，稍后再试")
		}
		complete := total == len(rows)
		seen := make(map[string]bool, len(rows))
		for i, row := range rows {
			id := strings.TrimSpace(row.Fields["thread_id"])
			if row.Index != i || row.TotalMatches != total || (id != "" && seen[id]) {
				complete = false
			}
			seen[id] = true
		}
		if complete {
			return rows, nil
		}
		request.MaxItems = 5000
	}
	return nil, fmt.Errorf("BOSS联系人列表还在变化，未核实完整列表，稍后再试")
}

// sameBossContactRow 用会话编号和可见正文确认同一行，列表重排不会按旧序号复用详情。
func sameBossContactRow(a, b contract.FindAllItem) bool {
	id := strings.TrimSpace(a.Fields["thread_id"])
	return id == strings.TrimSpace(b.Fields["thread_id"]) && a.Text != "" && a.Text == b.Text
}

// bossConversations 保留全部联系人；置顶行无key时使用明确的本地键，打开后必须从页头核身。
func bossConversations(rows []contract.FindAllItem) ([]model.Conversation, error) {
	result, err := common.ConfiguredConversations(rows, "boss", false)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(result))
	for i := range result {
		item := &result[i]
		if item.PlatformThreadID == "" {
			item.PlatformThreadID = item.Key
		}
		if seen[item.PlatformThreadID] || item.Name == "" {
			return nil, fmt.Errorf("BOSS联系人姓名或稳定会话编号无法唯一核实")
		}
		seen[item.PlatformThreadID] = true
		item.Key = item.PlatformThreadID
		if item.Fields == nil {
			item.Fields = make(map[string]string)
		}
		item.Fields["contact_list_signature"] = common.AutoReplyContactListSignature(item.Name, item.Summary, rows[i].Text, item.Fields["contact_time"])
		if item.PlatformCandidateID == "" && !strings.HasPrefix(item.PlatformThreadID, "local:") {
			item.PlatformCandidateID = bossCandidateIDFromThread(item.PlatformThreadID)
		}
	}
	return result, nil
}
