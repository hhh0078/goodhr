// Package common 本文件验证在线简历只允许平台显式片段变化，关闭后保留片段或回原页均须重新核身。
package common

import (
	"context"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// fragmentProfileBrowser 模拟在线面板改变URL片段，所有读写都只作用于测试内存。
type fragmentProfileBrowser struct {
	*onlineProfileTestBrowser
	page, openedPage, closedPage, readPage string
	ignoreEscape, keepOpen, wrongName      bool
	closeClicks                            int
}

// ListPages 返回当前真实模拟页，不把片段变化隐藏成原URL。
func (b *fragmentProfileBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: b.page, Current: true}}}, nil
}

// Click 仅区分本测试的在线入口及备用关闭，不提供重开聊天或导航能力。
func (b *fragmentProfileBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	if request.Selector.Description == "close" {
		b.closeClicks++
		b.open = b.keepOpen
		b.page = b.closedPage
	} else {
		b.open = true
		b.page = b.openedPage
	}
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 要求按键仍绑定实际打开后的页面，允许面板关闭时合法地改变片段。
func (b *fragmentProfileBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if request.Key != "Escape" || request.ExpectedPageURL != b.page || request.Target == nil {
		return contract.KeyboardPressResult{}, errors.New("关闭按键没有绑定实际在线页面")
	}
	b.keys++
	if !b.ignoreEscape {
		b.open = b.keepOpen
		b.page = b.closedPage
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// Read 可在正文读取后模拟切页或换人，验证关闭不能继续操作新页面。
func (b *fragmentProfileBrowser) Read(ctx context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	result, err := b.onlineProfileTestBrowser.Read(ctx, request)
	if request.Selector.Description == "body" {
		if b.readPage != "" {
			b.page = b.readPage
		}
		if b.wrongName {
			b.name = "另一位候选人"
		}
	}
	return result, err
}

// TestOnlineProfilePageAllowsOnlyConfiguredFragment 验证仅空片段和配置值能切换，编码、跨域、跨路径和查询变化均拒绝。
func TestOnlineProfilePageAllowsOnlyConfiguredFragment(t *testing.T) {
	base := "https://example.test/chat?job=1"
	for _, pair := range [][2]string{{base, base + "#preview"}, {base + "#preview", base}, {base + "#preview", base + "#preview"}, {base, base}} {
		if !onlineProfilePageAllowed(pair[0], pair[1], "preview") {
			t.Fatal("明确配置的两个片段状态应可切换")
		}
	}
	for _, changed := range []string{
		base + "#other", base + "#%70review", "https://other.test/chat?job=1#preview",
		"http://example.test/chat?job=1#preview", "https://example.test/other?job=1#preview",
		"https://example.test/chat?job=2#preview", "https://example.test/chat/?job=1#preview",
		"https://user@example.test/chat?job=1#preview", "https://example.test:443/chat?job=1#preview",
	} {
		if onlineProfilePageAllowed(base, changed, "preview") {
			t.Fatalf("不应允许未配置的页面变化：%s", changed)
		}
	}
	if onlineProfilePageAllowed(base+"#other", base+"#other", "preview") || onlineProfilePageAllowed(base, base+"#preview", "") || onlineProfilePageAllowed(base, base, "#preview") {
		t.Fatal("起始片段、未配置平台和无效配置都不能放宽")
	}
}

// TestOnlineProfileFragmentCloseRestoresBothLegalStates 验证Esc与备用按钮关闭后，保留preview或回原片段均正常核回原聊天。
func TestOnlineProfileFragmentCloseRestoresBothLegalStates(t *testing.T) {
	for _, test := range []struct {
		name, initial, closed string
		fallback              bool
	}{
		{"保留片段", "", "#preview", false}, {"恢复空片段", "", "", false},
		{"原页已有片段", "#preview", "#preview", false}, {"已有片段关闭后为空", "#preview", "", false},
		{"备用按钮保留片段", "", "#preview", true}, {"备用按钮恢复空片段", "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			original, cfg, snapshot := onlineProfileFixture("liepin")
			cfg.Behavior.AutoReplyOnlineProfileFragment = "preview"
			b := &fragmentProfileBrowser{onlineProfileTestBrowser: original, page: cfg.MessagesURL + test.initial, openedPage: cfg.MessagesURL + "#preview", closedPage: cfg.MessagesURL + test.closed, ignoreEscape: test.fallback}
			verified, restored := 0, 0
			verify := func(context.Context) error { verified++; return nil }
			restore := func(context.Context) error { restored++; return nil }
			profile, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, strings.TrimSpace)
			if err != nil || b.open || b.keys != 1 || restored != 1 || verified < 2 || profile.Mode != "dom" || profile.RawText == "" || len(profile.Screenshots) != 0 {
				t.Fatalf("合法片段未正确读取关闭恢复：err=%v open=%v keys=%d restore=%d verify=%d", err, b.open, b.keys, restored, verified)
			}
			wantButtons := 0
			if test.fallback {
				wantButtons = 1
			}
			if b.closeClicks != wantButtons {
				t.Fatal("备用关闭按钮只能在Esc未关闭时调用一次")
			}
		})
	}
}

// TestOnlineProfileFragmentRejectsChangedPageAndIdentity 验证越界URL、未配置平台、未关闭面板及恢复错人均不得登记成功。
func TestOnlineProfileFragmentRejectsChangedPageAndIdentity(t *testing.T) {
	for _, kind := range []string{"wrong-fragment", "wrong-path", "wrong-query", "no-config", "change-during-read", "panel-remains", "wrong-name", "wrong-restored-name", "wrong-restored-page"} {
		t.Run(kind, func(t *testing.T) {
			original, cfg, snapshot := onlineProfileFixture("liepin")
			cfg.Behavior.AutoReplyOnlineProfileFragment = "preview"
			b := &fragmentProfileBrowser{onlineProfileTestBrowser: original, page: cfg.MessagesURL, openedPage: cfg.MessagesURL + "#preview", closedPage: cfg.MessagesURL + "#preview"}
			switch kind {
			case "wrong-fragment":
				b.openedPage = cfg.MessagesURL + "#other"
			case "wrong-path":
				b.openedPage = "https://example.test/other#preview"
			case "wrong-query":
				b.openedPage = cfg.MessagesURL + "?other=1#preview"
			case "no-config":
				cfg.Behavior.AutoReplyOnlineProfileFragment = ""
			case "change-during-read":
				b.readPage = "https://other.test/chat#preview"
			case "panel-remains":
				b.keepOpen = true
				b.closedPage = cfg.MessagesURL
			case "wrong-name":
				b.wrongName = true
			}
			restored := 0
			verify := func(context.Context) error {
				if b.name != snapshot.CandidateName {
					return errors.New("候选人身份已变")
				}
				return nil
			}
			restore := func(context.Context) error {
				restored++
				if kind == "wrong-restored-name" {
					b.name = "其他聊天"
				}
				if kind == "wrong-restored-page" {
					b.page = "https://other.test/chat#preview"
				}
				return nil
			}
			_, err := ReadConfiguredAutoReplyOnlineProfile(context.Background(), b, cfg, snapshot, verify, restore, strings.TrimSpace)
			if err == nil {
				t.Fatal("页面或身份边界变化不能返回成功")
			}
			if strings.HasPrefix(kind, "wrong-") && kind != "wrong-name" && !strings.HasPrefix(kind, "wrong-restored") && (b.keys != 0 || original.bodyReads != 0 || restored != 0) {
				t.Fatal("打开后已经越界时不能继续读正文、关闭或恢复别的聊天")
			}
			if (kind == "no-config" || kind == "change-during-read" || kind == "wrong-name") && b.keys != 0 {
				t.Fatal("页面或在线姓名失去证明后不能追加按键")
			}
		})
	}
}
