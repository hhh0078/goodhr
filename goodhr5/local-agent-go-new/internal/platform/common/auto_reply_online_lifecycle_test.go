// Package common 本文件验证在线简历已有弹层、点击后验证失败及取消时的归属保护与恢复，不访问真实页面。
package common

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// onlineLifecycleBrowser 模拟独立聊天身份与弹层身份，防止用当前聊天姓名假定旧canvas已经属于该人。
type onlineLifecycleBrowser struct {
	*onlineProfileTestBrowser
	page, panelName                    string
	chatRight, ignoreEscape, failClose bool
	entryError                         error
	entryClicks, closeClicks, restores int
	afterEntry                         func(*onlineLifecycleBrowser)
	cancelCapture                      context.CancelFunc
}

// ListPages 返回实际模拟页，任何越界都不能被当作原资料页已经关闭。
func (b *onlineLifecycleBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.page, Current: true}}}, nil
}

// Read 读取当前弹层真实名称，取消后的采集不能继续取得正文或来源。
func (b *onlineLifecycleBrowser) Read(ctx context.Context, req contract.ElementReadRequest) (contract.ReadResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.ReadResult{}, err
	}
	if req.Selector.Description == "name" {
		return contract.ReadResult{Value: b.panelName}, nil
	}
	return b.onlineProfileTestBrowser.Read(ctx, req)
}

// Click 已有遮层时禁止点击后方入口；备用关闭按实际状态决定是否成功，不用返回值替代页面证据。
func (b *onlineLifecycleBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.ClickResult{}, err
	}
	if req.Selector.Description == "close" {
		b.closeClicks++
		if !b.failClose {
			b.open = false
		}
		return contract.ClickResult{Clicked: true}, nil
	}
	b.entryClicks++
	if b.open {
		return contract.ClickResult{}, errors.New("已有资料遮住了后方入口")
	}
	b.open = true
	b.panelName = b.name
	if b.afterEntry != nil {
		b.afterEntry(b)
	}
	return contract.ClickResult{Clicked: true}, b.entryError
}

// PressKey 只允许对仍在原页的资料弹层按Esc，未生效时留给现有关闭按钮备用。
func (b *onlineLifecycleBrowser) PressKey(ctx context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if err := ctx.Err(); err != nil {
		return contract.KeyboardPressResult{}, err
	}
	if req.Key != "Escape" || req.Target == nil || req.ExpectedPageURL != b.page {
		return contract.KeyboardPressResult{}, errors.New("关闭缺少原页约束")
	}
	b.keys++
	if !b.ignoreEscape {
		b.open = false
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// LongScreenshot 复用合成PNG能力，可在截图结束时模拟上层取消以验证独立清理预算。
func (b *onlineLifecycleBrowser) LongScreenshot(ctx context.Context, req contract.LongScreenshotRequest) (contract.LongScreenshotResult, error) {
	result, err := b.onlineProfileTestBrowser.LongScreenshot(ctx, req)
	if b.cancelCapture != nil {
		b.cancelCapture()
	}
	return result, err
}

// lifecycleCallbacks 只在真实当前聊天仍属于目标时允许核身及恢复，失败时不替测试改回候选人。
func (b *onlineLifecycleBrowser) lifecycleCallbacks() (func(context.Context) error, func(context.Context) error) {
	verify := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !b.chatRight {
			return errors.New("当前聊天已换人")
		}
		return nil
	}
	restore := func(ctx context.Context) error { b.restores++; return verify(ctx) }
	return verify, restore
}

// TestOnlineProfilePreexistingPanelLifecycle 验证可信DOM同人弹层可复用；canvas旧弹层无论显示何人都先安全重置，异人DOM与错人聊天不动。
func TestOnlineProfilePreexistingPanelLifecycle(t *testing.T) {
	for _, scenario := range []string{"dom-same", "dom-other", "canvas-same", "canvas-unknown-other", "changed-chat"} {
		t.Run(scenario, func(t *testing.T) {
			platform := "liepin"
			if strings.HasPrefix(scenario, "canvas") {
				platform = "boss"
			}
			base, cfg, snapshot := onlineProfileFixture(platform)
			base.open = true
			b := &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, panelName: snapshot.CandidateName, chatRight: true}
			if scenario == "dom-other" || scenario == "canvas-unknown-other" {
				b.panelName = "另一人在线资料"
			}
			if scenario == "changed-chat" {
				b.chatRight = false
			}
			verify, restore := b.lifecycleCallbacks()
			profile, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, nil)
			if scenario == "dom-other" || scenario == "changed-chat" {
				if err == nil || b.entryClicks != 0 || b.keys != 0 || b.restores != 0 || !b.open {
					t.Fatalf("不属于当前人的弹层被操作：err=%v entry=%d keys=%d restore=%d", err, b.entryClicks, b.keys, b.restores)
				}
				return
			}
			wantEntry, wantKeys, wantRestore := 0, 1, 1
			if platform == "boss" {
				wantEntry, wantKeys, wantRestore = 1, 2, 2
			}
			if err != nil || b.open || b.entryClicks != wantEntry || b.keys != wantKeys || b.restores != wantRestore || profile.Mode == "" {
				t.Fatalf("已有弹层仍盲点或没有恢复：err=%v entry=%d keys=%d restore=%d", err, b.entryClicks, b.keys, b.restores)
			}
		})
	}
}

// TestOnlineProfileClickFailureOwnsCleanup 验证点击已打开但校验失败仍收尾，换页换人后不关闭其新弹层。
func TestOnlineProfileClickFailureOwnsCleanup(t *testing.T) {
	for _, scenario := range []string{"verify-failure", "cancel-after-click", "changed-page", "changed-person"} {
		t.Run(scenario, func(t *testing.T) {
			base, cfg, snapshot := onlineProfileFixture("boss")
			cause := errors.New("点击后可见验证失败")
			b := &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, chatRight: true, entryError: cause}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "cancel-after-click":
				b.afterEntry = func(*onlineLifecycleBrowser) { cancel() }
				b.entryError = context.Canceled
			case "changed-page":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.page = "https://other.test/chat" }
			case "changed-person":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.chatRight = false; b.panelName = "另一人资料" }
			}
			verify, restore := b.lifecycleCallbacks()
			_, err := ReadConfiguredAutoReplyOnlineProfile(ctx, b, cfg, snapshot, verify, restore, nil)
			if err == nil || b.entryClicks != 1 || b.screenshots != 0 {
				t.Fatalf("点击验证错误丢失或继续读资料：%v", err)
			}
			if strings.HasPrefix(scenario, "changed-") {
				if b.keys != 0 || b.closeClicks != 0 || b.restores != 0 || !b.open {
					t.Fatal("换页或换人后仍关闭了别人的弹层")
				}
			} else if b.open || b.keys != 1 || b.restores != 1 {
				t.Fatalf("点击已触发的弹层残留：open=%v keys=%d restore=%d err=%v", b.open, b.keys, b.restores, err)
			}
			if scenario == "verify-failure" && !errors.Is(err, cause) {
				t.Fatal("清理丢失了原点击失败原因")
			}
		})
	}
}

// TestOnlineProfileCloseFailureCanRecover 验证Esc和备用按钮都未关闭时不重开入口，下一轮先处理旧层再恢复正常读取。
func TestOnlineProfileCloseFailureCanRecover(t *testing.T) {
	base, cfg, snapshot := onlineProfileFixture("boss")
	base.open = true
	b := &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, panelName: snapshot.CandidateName, chatRight: true, ignoreEscape: true, failClose: true}
	verify, restore := b.lifecycleCallbacks()
	if _, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, nil); err == nil || !b.open || b.entryClicks != 0 || b.keys != 1 || b.closeClicks != 1 || b.restores != 0 {
		t.Fatalf("关闭失败后仍重开入口或恢复聊天：%v", err)
	}
	b.ignoreEscape, b.failClose = false, false
	if _, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, nil); err != nil || b.open || b.entryClicks != 1 || b.keys != 3 || b.restores != 2 {
		t.Fatalf("下一轮没有先重置旧弹层后恢复：%v", err)
	}
}

// TestOnlineProfileRestoreFailureDoesNotReopenBehindPanel 验证旧canvas已关闭但聊天恢复失败时立即返回，下一次正常恢复前不点击入口。
func TestOnlineProfileRestoreFailureDoesNotReopenBehindPanel(t *testing.T) {
	base, cfg, snapshot := onlineProfileFixture("boss")
	base.open = true
	b := &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, panelName: snapshot.CandidateName, chatRight: true}
	chatRestored := false
	verify := func(context.Context) error {
		if b.keys > 0 && !chatRestored {
			return model.ErrAutoReplyConversationNotOpen
		}
		return nil
	}
	failed := errors.New("原聊天暂未恢复")
	restore := func(context.Context) error {
		b.restores++
		if b.restores == 1 {
			return failed
		}
		chatRestored = true
		return nil
	}
	if _, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, nil); !errors.Is(err, failed) || b.open || b.entryClicks != 0 || b.restores != 1 {
		t.Fatalf("恢复失败后盲点入口或错误丢失：%v", err)
	}
	if _, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, nil); err != nil || b.open || b.entryClicks != 1 || b.restores != 3 {
		t.Fatalf("后续没有先恢复聊天再打开资料：%v", err)
	}
}

// TestOnlineProfileCancelledLifecycle 验证开始前取消不碰页面，采集中取消仍以独立上下文关闭本轮目标弹层。
func TestOnlineProfileCancelledLifecycle(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before", false: "after-capture"}[before], func(t *testing.T) {
			base, cfg, snapshot := onlineProfileFixture("boss")
			b := &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, chatRight: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if before {
				cancel()
			} else {
				b.cancelCapture = cancel
			}
			verify, restore := b.lifecycleCallbacks()
			_, err := ReadConfiguredAutoReplyOnlineProfile(ctx, b, cfg, snapshot, verify, restore, nil)
			if err == nil || b.open {
				t.Fatalf("取消后仍成功或留有弹层：%v", err)
			}
			if before && (b.entryClicks != 0 || b.keys != 0 || b.restores != 0) {
				t.Fatal("开始前取消仍操作页面")
			}
			if !before && (b.entryClicks != 1 || b.keys != 1 || b.restores != 1) {
				t.Fatal("采集中取消没有清理自己的弹层")
			}
		})
	}
}
