// Package liepin 文件作用：模拟重排、同名、预览时间变化和补读失败，验证未读缓存逐行核对且不会返回错人。
package liepin

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// contactRowsBrowser 按请求字段及窗口返回可见行，同时记录浏览器实际读取了多少详情。
type contactRowsBrowser struct {
	liepinReplyBrowserStub
	requests []contract.ElementFindAllRequest
	onDetail func()
	onRead   func(contract.ElementFindAllRequest, []contract.FindAllItem) []contract.FindAllItem
}

// FindAll 模拟详细字段按当前列表序号读取；允许在轻读和补读之间模拟平台重排。
func (b *contactRowsBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.requests = append(b.requests, req)
	if req.MaxItems == 1 && b.onDetail != nil {
		b.onDetail()
	}
	out := make([]contract.FindAllItem, 0)
	for i := req.StartIndex; i < min(len(b.items), req.StartIndex+req.MaxItems); i++ {
		raw := b.items[i]
		row := contract.FindAllItem{Index: i, TotalMatches: len(b.items), Text: raw.Text, Fields: map[string]string{}}
		for key := range req.Fields {
			row.Fields[key] = raw.Fields[key]
		}
		out = append(out, row)
	}
	if b.onRead != nil {
		out = b.onRead(req, out)
	}
	return out, nil
}

// contactRowsFixture 创建八个同名、同行正文但线程和头像各异的联系人，模拟同名造成的身份碰撞。
func contactRowsFixture() (*Runtime, *contactRowsBrowser, model.Config) {
	_, _, cfg := allContactFixture()
	cfg.ConversationFields = make(map[string]contract.SelectorSpec)
	for _, key := range []string{"thread_meta", "unread_count", "name", "last_message", "avatar_url", "position_name", "contact_time"} {
		cfg.ConversationFields[key] = testLiepinSelector(key)
	}
	b := &contactRowsBrowser{}
	for i := range 8 {
		b.items = append(b.items, contract.FindAllItem{Text: "同名候选人 旧消息", Fields: map[string]string{
			"name": "同名候选人", "last_message": "旧消息", "unread_count": "1",
			"thread_meta": fmt.Sprintf(`{"to_imid":"thread-%d"}`, i), "avatar_url": fmt.Sprintf("https://img.test/%d", i),
		}})
	}
	return NewRuntime(), b, cfg
}

// TestUnreadRowsReuseVerifiedIdentitiesAndReadChanges 验证普通列表的所有行都会核对且只为变更行补详情。
func TestUnreadRowsReuseVerifiedIdentitiesAndReadChanges(t *testing.T) {
	r, b, cfg := contactRowsFixture()
	ctx := context.Background()
	read := func() []model.Conversation {
		t.Helper()
		b.requests = nil
		items, err := r.readLiepinContacts(ctx, b, cfg)
		if err != nil || len(items) != len(b.items) {
			t.Fatalf("读取联系人丢行：%d %v", len(items), err)
		}
		return items
	}
	read()
	if len(b.requests) != 1 || len(b.requests[0].Fields) != 5 {
		t.Fatalf("首次读取了不需要的字段：%+v", b.requests)
	}
	if _, exists := b.requests[0].Fields["unread_count"]; exists {
		t.Fatal("全部联系人扫描不应单独读取红点")
	}
	b.items[0], b.items[7] = b.items[7], b.items[0]
	got := read()
	if len(b.requests) != 1 || len(b.requests[0].Fields) != 1 || got[0].PlatformThreadID != "thread-7" || got[0].Index != 0 || got[0].AvatarURL != "https://img.test/7" {
		t.Fatalf("同名同正文换位后串人或重复读详情：%+v, %d 个请求", got[0], len(b.requests))
	}
	for _, change := range []struct{ field, value string }{{"thread_meta", `{"to_imid":"replacement"}`}, {"text", "同名候选人 新消息"}} {
		b.items[4].Fields["avatar_url"] += "-updated"
		if change.field == "text" {
			b.items[4].Text = change.value
		} else {
			b.items[4].Fields[change.field] = change.value
		}
		got = read()
		if len(b.requests) != 2 || b.requests[1].StartIndex != 4 || b.requests[1].MaxItems != 1 || got[4].AvatarURL != b.items[4].Fields["avatar_url"] {
			t.Fatalf("%s 变更没有只补读第四行：%d 个请求 %+v", change.field, len(b.requests), got[4])
		}
	}
}

// TestUnreadRowsRecoverWhenDetailMovedOrIdentityMissing 验证补读时换人、无稳定编号或重复编号都走完整读取。
func TestUnreadRowsRecoverWhenDetailMovedOrIdentityMissing(t *testing.T) {
	r, b, cfg := contactRowsFixture()
	ctx := context.Background()
	if _, err := r.readLiepinContacts(ctx, b, cfg); err != nil {
		t.Fatal(err)
	}
	b.items[2].Text = "位置变动 09:36 新消息"
	b.items[2].Fields["last_message"] = "位置变动的新消息"
	b.onDetail = func() {
		b.items[2], b.items[5] = b.items[5], b.items[2]
		b.onDetail = nil
	}
	b.requests = nil
	got, err := r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(b.requests) != 3 || b.requests[2].MaxItems == 1 || got[2].PlatformThreadID != "thread-5" || got[5].Summary != "位置变动的新消息" {
		t.Fatalf("补读中换人，未恢复为同一份全表：%d 个请求 %+v %v", len(b.requests), got, err)
	}
	b.items[1].Fields["thread_meta"] = ""
	got, err = r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(got) != 8 || got[1].PlatformThreadID != "" || got[1].Key == "" || r.contactRows != nil {
		t.Fatalf("无编号联系人被遗漏或错误复用：%+v %v", got, err)
	}
	b.items[1].Fields["thread_meta"] = b.items[4].Fields["thread_meta"]
	got, err = r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(got) != 8 || r.contactRows != nil {
		t.Fatalf("重复编号不应进入缓存：%d %v", len(got), err)
	}
}

// failedUnreadRowsBrowser 在补读时模拟 Worker 故障，检查不会用未完整核对的数据替换上轮结果。
type failedUnreadRowsBrowser struct{ *contactRowsBrowser }

// FindAll 在单行补读时报错，其余批量读取复用正常页面替身。
func (b failedUnreadRowsBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.MaxItems == 1 {
		return nil, errors.New("读取详情时断开")
	}
	return b.contactRowsBrowser.FindAll(ctx, req)
}

// TestUnreadRowsDiscardFailedGeneration 验证补读失败不会推进任何一行的快照。
func TestUnreadRowsDiscardFailedGeneration(t *testing.T) {
	r, b, cfg := contactRowsFixture()
	ctx := context.Background()
	if _, err := r.readLiepinContacts(ctx, b, cfg); err != nil {
		t.Fatal(err)
	}
	b.items[2].Text = "09:38 新摘要"
	b.items[2].Fields["last_message"] = "新摘要"
	if _, err := r.readLiepinContacts(ctx, failedUnreadRowsBrowser{b}, cfg); err == nil || r.contactRows["thread-2"].Fields["last_message"] != "旧消息" {
		t.Fatal("补读失败后错误推进缓存")
	}
	b.requests = nil
	got, err := r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(b.requests) != 2 || got[2].Summary != "新摘要" {
		t.Fatalf("重试未重新补齐：%+v %v", got, err)
	}
}

// largeUnreadRowsFixture 创建多于首个一百行窗口的模拟联系人，供检查首屏以后的真实变化。
func largeUnreadRowsFixture(total int) (*Runtime, *contactRowsBrowser, model.Config) {
	r, b, cfg := contactRowsFixture()
	for i := len(b.items); i < total; i++ {
		b.items = append(b.items, contract.FindAllItem{Text: "相同的可见摘要", Fields: map[string]string{
			"name": "同名候选人", "last_message": "旧消息", "unread_count": "1", "thread_meta": fmt.Sprintf(`{"to_imid":"thread-%d"}`, i),
		}})
	}
	cfg.MaxItems = 100
	return r, b, cfg
}

// TestUnreadRowsCoverBeyondFirstWindow 验证补读有交叠，且第一百零五行的新消息也能按原位置补读唯一详情。
func TestUnreadRowsCoverBeyondFirstWindow(t *testing.T) {
	r, b, cfg := largeUnreadRowsFixture(110)
	ctx := context.Background()
	got, err := r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(got) != 110 || len(b.requests) != 2 || b.requests[1].StartIndex != 99 || b.requests[1].MaxItems != 11 {
		t.Fatalf("没有一次补齐第一个窗口之后的联系人：len=%d requests=%d err=%v", len(got), len(b.requests), err)
	}
	b.items[104].Text = "09:39 新摘要"
	b.items[104].Fields["last_message"] = "新摘要"
	b.requests = nil
	got, err = r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(got) != 110 || len(b.requests) != 3 || b.requests[2].StartIndex != 104 || got[104].Summary != "新摘要" {
		t.Fatalf("第二个窗口的新消息没有补读：len=%d requests=%d err=%v", len(got), len(b.requests), err)
	}
}

// TestUnreadRowsWindowReorderRetriesAndGapsFail 验证跨窗口重号时丢弃混合结果，重新核对仍缺行则保留上次成功快照。
func TestUnreadRowsWindowReorderRetriesAndGapsFail(t *testing.T) {
	r, b, cfg := largeUnreadRowsFixture(110)
	ctx := context.Background()
	b.onRead = func(req contract.ElementFindAllRequest, page []contract.FindAllItem) []contract.FindAllItem {
		if req.StartIndex == 0 && req.MaxItems == 100 {
			// 首窗读完后，模拟平台交换第一行和第一百零五行，交叠行本身没有变化。
			b.items[0], b.items[104] = b.items[104], b.items[0]
		}
		return page
	}
	got, err := r.readLiepinContacts(ctx, b, cfg)
	if err != nil || len(got) != 110 || got[0].PlatformThreadID != "thread-104" || len(b.requests) != 3 || b.requests[2].MaxItems != 5000 {
		t.Fatalf("跨窗口换人后没有重新从头核对：%d 个请求 %v", len(b.requests), err)
	}
	b.requests = nil
	b.items[0].Text = "09:40 新摘要"
	b.items[0].Fields["last_message"] = "新摘要"
	b.onRead = func(req contract.ElementFindAllRequest, page []contract.FindAllItem) []contract.FindAllItem {
		if req.StartIndex == 99 || req.MaxItems == 5000 {
			return page[1:]
		}
		return page
	}
	_, err = r.readLiepinContacts(ctx, b, cfg)
	if err == nil || len(b.requests) != 3 || r.contactRows["thread-104"].Fields["last_message"] != "旧消息" {
		t.Fatalf("分页持续缺行却推进了缓存：%d 个请求 %v", len(b.requests), err)
	}
}

// TestUnreadRowsRejectAboveWorkerLimit 验证超出 Worker 上限时不把前一百个联系人当作完整结果。
func TestUnreadRowsRejectAboveWorkerLimit(t *testing.T) {
	r, b, cfg := largeUnreadRowsFixture(110)
	b.onRead = func(_ contract.ElementFindAllRequest, page []contract.FindAllItem) []contract.FindAllItem {
		for i := range page {
			page[i].TotalMatches = 5001
		}
		return page
	}
	if _, err := r.readLiepinContacts(context.Background(), b, cfg); err == nil || len(b.requests) != 1 || r.contactRows != nil {
		t.Fatal("超出可核实上限后静默接收了部分列表", err)
	}
}
