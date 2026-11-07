// Package liepin 本文件模拟未读标签常驻时的陈旧列表、入口角标变化和真实切换后的列表更新。
package liepin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// cachedUnreadBrowser 保留已有联系人列表，只有切换全部再返回未读时才展示准备好的新消息。
type cachedUnreadBrowser struct {
	liepinReplyBrowserStub
	badgeErr  error
	badge     string
	next      []contract.FindAllItem
	listReads int
}

// Read 提供可能停留在99+的可见入口角标。
func (b *cachedUnreadBrowser) Read(ctx context.Context, req contract.ElementReadRequest) (contract.ReadResult, error) {
	if req.Selector.Description == "测试未读数字" {
		return contract.ReadResult{Value: b.badge}, b.badgeErr
	}
	return b.liepinReplyBrowserStub.Read(ctx, req)
}

// FindAll 统计整张联系人列表读取次数，排除确认标签状态的轻量探测。
func (b *cachedUnreadBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "测试未读联系人" {
		b.listReads++
	}
	return b.liepinReplyBrowserStub.FindAll(ctx, req)
}

// Click 在实际的全部到未读切换时让新会话进入联系人列表。
func (b *cachedUnreadBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	if req.Selector.Description == "测试未读标签" && b.allSelected && b.next != nil {
		b.items, b.next = b.next, nil
	}
	return b.liepinReplyBrowserStub.Click(ctx, req)
}

// unreadRefreshFixture 创建仅包含可见数字、联系人和真实标签的猎聘配置。
func unreadRefreshFixture() (*Runtime, *cachedUnreadBrowser, model.Config) {
	cfg := model.Config{Name: "测试平台", Selectors: map[string]contract.SelectorSpec{}}
	for key, label := range map[string]string{"message.drawer": "测试联系人抽屉", "message.entry": "测试联系人入口", "message.entry_unread_count": "测试未读数字",
		"message.unread_tab": "测试未读标签", "message.unread_tab_selected": "测试未读标签已选中", "message.all_tab": "测试全部标签",
		"message.all_tab_selected": "测试全部标签已选中", "message.unread_item": "测试未读联系人"} {
		cfg.Selectors[key] = testLiepinSelector(label)
	}
	b := &cachedUnreadBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{drawerOpen: true, unreadSelected: true}, badge: "25"}
	for index := range 25 {
		b.items = append(b.items, contract.FindAllItem{Index: index, Fields: map[string]string{"name": "合成候选人", "thread_meta": fmt.Sprintf(`{"to_imid":"thread-%d"}`, index), "last_message": "旧摘要", "unread_count": "1"}})
	}
	return NewRuntime(), b, cfg
}

// TestLiepinUnreadRefreshStaleNonemptyList 验证真实的非空陈旧列表在角标变化或长时间不变时均能恢复。
func TestLiepinUnreadRefreshStaleNonemptyList(t *testing.T) {
	for _, tc := range []struct {
		name, initial, changed string
		interval               time.Duration
	}{
		{"数字增长", "25", "26", liepinUnreadChangeInterval},
		{"数字不变", "25", "25", liepinUnreadRefreshInterval},
		{"封顶角标", "99+", "99+", liepinUnreadRefreshInterval},
		{"角标隐藏", "", "", liepinUnreadRefreshInterval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, b, cfg := unreadRefreshFixture()
			b.badge = tc.initial
			if _, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil {
				t.Fatal(err)
			}
			if b.listReads != 1 {
				t.Fatal("首轮重复读取联系人", b.listReads)
			}
			b.clicks = nil
			b.badge = tc.changed
			b.next = []contract.FindAllItem{{Fields: map[string]string{"name": "新消息候选人", "thread_meta": `{"to_imid":"target-04"}`, "unread_count": "1"}}}
			if v, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil || len(v) != 25 || len(b.clicks) != 0 {
				t.Fatal("冷却期间发生多余切换", len(v), b.clicks, err)
			}
			r.unread.confirmedAt = time.Now().Add(-tc.interval - time.Second)
			v, err := r.ScanUnreadConversations(context.Background(), b, cfg)
			if err != nil || len(v) != 1 || v[0].PlatformThreadID != "target-04" || len(b.clicks) != 2 || b.clicks[0] != "测试全部标签" || b.clicks[1] != "测试未读标签" {
				t.Fatal("新消息没有通过真实标签切换进入扫描", v, b.clicks, err)
			}
			if _, err = r.ScanUnreadConversations(context.Background(), b, cfg); err != nil || len(b.clicks) != 2 {
				t.Fatal("刷新后每轮再次切换", b.clicks, err)
			}
		})
	}
}

// TestLiepinUnreadFreshAndEmptyViewsReadOnce 验证已更新、新选择和完全无未读时无需二次读取联系人。
func TestLiepinUnreadFreshAndEmptyViewsReadOnce(t *testing.T) {
	r, b, cfg := unreadRefreshFixture()
	b.unreadSelected = false
	b.allSelected = true
	if _, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil || b.listReads != 1 || len(b.clicks) != 1 {
		t.Fatal("刚选择未读被重复刷新", b.clicks, b.listReads, err)
	}
	b.clicks = nil
	r.unread.confirmedAt = time.Now().Add(-time.Minute)
	b.items[0].Fields["last_message"] = "已出现的新摘要"
	if v, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil || len(v) == 0 || v[0].Summary != "已出现的新摘要" || len(b.clicks) != 0 || b.listReads != 2 {
		t.Fatal("列表已自行更新却重读", b.clicks, b.listReads, err)
	}
	b.items = nil
	b.badge = ""
	for range 2 {
		r.unread.confirmedAt = time.Now().Add(-time.Minute)
		if v, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil || len(v) != 0 {
			t.Fatal(v, err)
		}
	}
	if len(b.clicks) != 0 || b.listReads != 4 {
		t.Fatal("空角标空列表产生多余操作", b.clicks, b.listReads)
	}
}

// TestLiepinHiddenEntryBadgeKeepsPolling 验证猎聘抽屉遮住悬浮入口后仍能通过可见列表更新，控制通道错误仍上报。
func TestLiepinHiddenEntryBadgeKeepsPolling(t *testing.T) {
	r, b, cfg := unreadRefreshFixture()
	b.badgeErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_VISIBLE"}}
	if _, err := r.ScanUnreadConversations(context.Background(), b, cfg); err != nil {
		t.Fatal(err)
	}
	r.unread.confirmedAt = time.Now().Add(-time.Minute)
	b.next = []contract.FindAllItem{{Fields: map[string]string{"name": "测试目标", "thread_meta": `{"to_imid":"new-target"}`, "unread_count": "1"}}}
	got, err := r.ScanUnreadConversations(context.Background(), b, cfg)
	if err != nil || len(got) != 1 || got[0].PlatformThreadID != "new-target" {
		t.Fatal("入口隐藏时未恢复扫描", got, err)
	}
	b.badgeErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE"}}
	if _, err := r.ScanUnreadConversations(context.Background(), b, cfg); !contract.IsSessionUnavailable(err) {
		t.Fatal("控制通道故障被忽略", err)
	}
}
