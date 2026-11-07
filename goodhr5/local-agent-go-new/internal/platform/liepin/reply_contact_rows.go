// Package liepin 文件作用：核对每个联系人的会话编号和可见正文，只补读有变化的行。
package liepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// readLiepinContacts 每轮都返回全部可见联系人行，联系人详情只在整份本轮快照成功后更新。
func (r *Runtime) readLiepinContacts(ctx context.Context, browser model.Browser, cfg model.Config) ([]model.Conversation, error) {
	selector, err := common.RequiredSelector(cfg, "message.contact_item")
	if err != nil {
		return nil, err
	}
	limit := cfg.MaxItems
	if limit < 1 {
		limit = 100
	}
	details := make(map[string]contract.SelectorSpec)
	for _, key := range []string{"name", "avatar_url", "last_message", "contact_time", "thread_meta"} {
		if spec, ok := cfg.ConversationFields[key]; ok {
			details[key] = spec
		}
	}
	request := contract.ElementFindAllRequest{Selector: selector, Fields: details, MaxItems: limit, ExpectedMissing: true}
	var rows []contract.FindAllItem
	complete := false
	if len(r.contactRows) > 0 {
		rows, complete, err = r.readChangedContactRows(ctx, browser, request)
		if err != nil {
			return nil, err
		}
	}
	if !complete {
		rows, err = readLiepinContactWindows(ctx, browser, request)
		if err != nil && !common.IsElementMissing(err) {
			return nil, err
		}
	}
	conversations, err := liepinConversations(rows)
	if err != nil {
		return nil, err
	}
	// 以实际线程号而非姓名建索引。缺编号、重号或没有正文的视图下次仍走完整读取。
	cache := make(map[string]contract.FindAllItem, len(rows))
	for _, row := range rows {
		key := liepinContactRowID(row)
		if _, exists := cache[key]; exists || key == "" || row.Text == "" {
			cache = nil
			break
		}
		cache[key] = row
	}
	r.contactRows = cache
	return conversations, nil
}

// readChangedContactRows 先轻读所有行；只有本轮仍能核对同一行时才接受补读，列表同时大变则交给一次完整读取。
func (r *Runtime) readChangedContactRows(ctx context.Context, browser model.Browser, request contract.ElementFindAllRequest) ([]contract.FindAllItem, bool, error) {
	light := request
	light.Fields = map[string]contract.SelectorSpec{"thread_meta": request.Fields["thread_meta"]}
	rows, err := readLiepinContactWindows(ctx, browser, light)
	if common.IsElementMissing(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	seen := make(map[string]bool, len(rows))
	var changed []int
	for i, row := range rows {
		id := liepinContactRowID(row)
		if id == "" || seen[id] || row.Text == "" {
			return nil, false, nil
		}
		seen[id] = true
		if old, ok := r.contactRows[id]; ok && sameLiepinContactRow(old, row) {
			old.Index, old.TotalMatches = row.Index, row.TotalMatches
			rows[i] = old
		} else {
			changed = append(changed, i)
		}
	}
	if len(changed) > 3 || len(changed)*2 > len(rows) {
		return nil, false, nil
	}
	for _, i := range changed {
		request.StartIndex, request.MaxItems = rows[i].Index, 1
		full, err := browser.FindAll(ctx, request)
		if err != nil && !common.IsElementMissing(err) {
			return nil, false, err
		}
		if len(full) != 1 || full[0].Index != rows[i].Index || full[0].TotalMatches != rows[i].TotalMatches || !sameLiepinContactRow(full[0], rows[i]) {
			return nil, false, nil
		}
		rows[i] = full[0]
	}
	return rows, true, nil
}

// readLiepinContactWindows 只在已渲染行超过首个窗口时补读；相邻窗口重叠一行以核对重排、缺行及重复身份。
func readLiepinContactWindows(ctx context.Context, browser model.Browser, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	rows, err := browser.FindAll(ctx, request)
	if err != nil || len(rows) == 0 || rows[0].TotalMatches <= request.MaxItems {
		return rows, err
	}
	const maxMatches = 5000 // Worker 的单次列表上限，也是重新核对整份联系人快照的最大行数。
	total := rows[0].TotalMatches
	if total > maxMatches {
		return nil, fmt.Errorf("联系人已渲染 %d 行，超过单次可核实的 %d 行，稍后再试", total, maxMatches)
	}
	seen := make(map[string]bool, total)
	checked := 0
	verified := contiguousContactWindow(rows, 0, request.MaxItems, total)
	for verified {
		// 同号或缺号时不能靠相邻窗口判定归属，改走完整快照。
		for _, row := range rows[checked:] {
			id := liepinContactRowID(row)
			if id == "" || seen[id] {
				verified = false
				break
			}
			seen[id] = true
		}
		if !verified || len(rows) == total {
			break
		}
		checked = len(rows)
		window := request
		window.StartIndex = len(rows) - 1
		window.MaxItems = min(max(2, request.MaxItems), total-window.StartIndex)
		page, err := browser.FindAll(ctx, window)
		if err != nil && !common.IsElementMissing(err) {
			return nil, err
		}
		verified = contiguousContactWindow(page, window.StartIndex, window.MaxItems, total) &&
			sameLiepinContactRow(rows[window.StartIndex], page[0])
		if verified {
			rows = append(rows, page[1:]...)
		}
	}
	if verified {
		return rows, nil
	}
	// 页面在补读期间有变化，丢弃前面各页，重新采集一次当前完整快照。
	request.MaxItems = maxMatches
	rows, err = browser.FindAll(ctx, request)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	total = rows[0].TotalMatches
	if total > maxMatches || !contiguousContactWindow(rows, 0, total, total) {
		return nil, fmt.Errorf("联系人列表仍在变化，未完整核实全部 %d 行，稍后重试", total)
	}
	return rows, nil
}

// contiguousContactWindow 核对该窗口每行在整份页面中的原始位置和总数，以免静默跳过一行。
func contiguousContactWindow(rows []contract.FindAllItem, start, count, total int) bool {
	if len(rows) != count {
		return false
	}
	for i, row := range rows {
		if row.Index != start+i || row.TotalMatches != total {
			return false
		}
	}
	return true
}

// liepinContactRowID 读取行内平台会话编号；缺少或无法解析时禁止复用旧联系人。
func liepinContactRowID(row contract.FindAllItem) string {
	meta, err := parseLiepinContactMeta(row.Fields["thread_meta"])
	if err != nil {
		return ""
	}
	return strings.TrimSpace(meta.ThreadID)
}

// sameLiepinContactRow 核对稳定会话编号和可见正文，允许同一行移动到列表其他位置。
func sameLiepinContactRow(a, b contract.FindAllItem) bool {
	id := liepinContactRowID(a)
	return id != "" && id == liepinContactRowID(b) && a.Text == b.Text
}
