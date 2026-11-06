// Package liepin 本文件验证最新消息尾窗读取、系统提示跳过和读取期间新消息变化，不操作真实招聘页面。
package liepin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// latestMessageBrowser 记录尾窗请求并在字段读取后模拟新消息到达，返回字段严格取自实际请求。
type latestMessageBrowser struct {
	liepinReplyBrowserStub
	requests  []contract.ElementFindAllRequest
	afterRead func(*latestMessageBrowser, contract.ElementFindAllRequest)
}

// FindAll 复用已有窗口替身，保留正文和数量，只输出Worker实际被要求读取的字段。
func (b *latestMessageBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.requests = append(b.requests, request)
	items, err := b.liepinReplyBrowserStub.FindAll(ctx, request)
	for index := range items {
		fields := make(map[string]string, len(request.Fields))
		for key := range request.Fields {
			fields[key] = items[index].Fields[key]
		}
		items[index].Fields = fields
	}
	if b.afterRead != nil {
		b.afterRead(b, request)
	}
	return items, err
}

// latestMessageItems 创建候选人消息或系统提示，含真实DOM序号之外的稳定消息编号。
func latestMessageItems(count int, system bool) []contract.FindAllItem {
	items := make([]contract.FindAllItem, count)
	for index := range items {
		items[index] = historyAuditMessage(fmt.Sprintf("message-%d", index))
		items[index].Text = items[index].Fields["message_text"]
		if system {
			items[index].Fields["body_class"] = ""
		}
	}
	return items
}

// TestLiepinLatestReadsOnlyTailForLongHistory 验证89条和超过历史上限的会话仍只完整读取真正末条，并保留原正文和稳定编号。
func TestLiepinLatestReadsOnlyTailForLongHistory(t *testing.T) {
	for _, count := range []int{89, 5003} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			items := latestMessageItems(count, false)
			items[count-1].Fields["body_class"] = "im-ui-message-item-send"
			browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
			message, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig())
			if err != nil || message.Direction != "self" || message.PlatformMessageID != fmt.Sprintf("message-%d", count-1) || message.TextContent != items[count-1].Text {
				t.Fatalf("最新消息改变：%+v err=%v", message, err)
			}
			if len(browser.requests) != 2 || len(browser.requests[0].Fields) != 3 || len(browser.requests[1].Fields) != 1 {
				t.Fatalf("没有使用一条正文加轻量尾部复核：%+v", browser.requests)
			}
			for _, request := range browser.requests {
				if !request.FromEnd || request.MaxItems != 1 {
					t.Fatalf("最新消息仍在全量读取：%+v", request)
				}
			}
		})
	}
}

// TestLiepinLatestExpandsPastSystemTail 验证多条系统提示不遮蔽最后一条候选人消息，且尾窗按倍数扩展。
func TestLiepinLatestExpandsPastSystemTail(t *testing.T) {
	items := latestMessageItems(7, true)
	items[0].Fields["body_class"] = "im-ui-message-item-receive"
	browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
	message, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig())
	if err != nil || message.Direction != "candidate" || message.PlatformMessageID != "message-0" {
		t.Fatalf("系统提示遮蔽候选人消息：%+v %v", message, err)
	}
	sizes := []int{}
	for _, request := range browser.requests {
		sizes = append(sizes, request.MaxItems)
	}
	if !reflect.DeepEqual(sizes, []int{1, 2, 4, 8, 1}) {
		t.Fatalf("尾窗扩展不正确：%v", sizes)
	}
}

// TestLiepinLatestRejectsEmptyAndExcessiveSystemTail 验证空、纯系统提示和超过五千条系统尾部都有限退出，不返回窗口以外的旧消息。
func TestLiepinLatestRejectsEmptyAndExcessiveSystemTail(t *testing.T) {
	for _, count := range []int{0, 7, 5002} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			items := latestMessageItems(count, true)
			if count > 5000 {
				items[0].Fields["body_class"] = "im-ui-message-item-receive"
			}
			browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
			if _, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig()); err == nil {
				t.Fatal("未读取到候选人或HR消息却报告成功")
			}
			if len(browser.requests) > 14 {
				t.Fatalf("超出有界窗口：%d", len(browser.requests))
			}
			for _, request := range browser.requests {
				if !request.FromEnd || request.MaxItems > 5000 {
					t.Fatalf("扩大了历史读取上限：%+v", request)
				}
			}
		})
	}
}

// TestLiepinLatestRechecksNewMessageDuringRead 验证字段读取后新增、同数量替换和扩窗时新增消息均返回重新核实的真正末条。
func TestLiepinLatestRechecksNewMessageDuringRead(t *testing.T) {
	for _, kind := range []string{"append", "replace", "edit", "expand"} {
		t.Run(kind, func(t *testing.T) {
			items := latestMessageItems(2, false)
			if kind == "expand" {
				items[1].Fields["body_class"] = ""
			}
			browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
			browser.afterRead = func(b *latestMessageBrowser, request contract.ElementFindAllRequest) {
				if len(b.requests) != 1 {
					return
				}
				updated := append([]contract.FindAllItem(nil), items...)
				newMessage := historyAuditMessage("newest")
				if kind == "edit" {
					newMessage.Fields["message_meta"] = items[len(items)-1].Fields["message_meta"]
					newMessage.Text = "newest"
				}
				if kind == "replace" || kind == "edit" {
					updated[len(updated)-1] = newMessage
				} else {
					updated = append(updated, newMessage)
				}
				b.historyBatches = [][]contract.FindAllItem{updated}
			}
			message, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig())
			if err != nil || message.TextContent != "newest" || (kind != "edit" && message.PlatformMessageID != "newest") {
				t.Fatalf("返回了读取期间已经过期的消息：%+v %v", message, err)
			}
		})
	}
}

// TestLiepinLatestFailsClosedWhenChangingOrCancelled 验证持续来消息和取消均不会返回未复核的旧消息。
func TestLiepinLatestFailsClosedWhenChangingOrCancelled(t *testing.T) {
	browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{latestMessageItems(1, false)}}}
	browser.afterRead = func(b *latestMessageBrowser, request contract.ElementFindAllRequest) {
		if _, full := request.Fields["body_class"]; full {
			b.historyBatches[0] = append(b.historyBatches[0], historyAuditMessage(fmt.Sprintf("new-%d", len(b.requests))))
		}
	}
	if _, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, historyAuditConfig()); err == nil || !strings.Contains(err.Error(), "还在变化") || len(browser.requests) != 6 {
		t.Fatalf("持续变化没有有限停止：requests=%d err=%v", len(browser.requests), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readLiepinLatestAutoReplyMessage(ctx, browser, historyAuditConfig()); !errors.Is(err, context.Canceled) || len(browser.requests) != 6 {
		t.Fatalf("取消后仍读取消息：%v", err)
	}
}

// TestLiepinLatestDuplicateFallbackMatchesHistory 验证无平台编号的同分钟重复消息按完整分钟组编号，追加新消息不复用旧键。
func TestLiepinLatestDuplicateFallbackMatchesHistory(t *testing.T) {
	cfg := historyAuditConfig()
	cfg.ConversationFields["message_time"] = testLiepinSelector("消息时间")
	items := latestMessageItems(4, false)
	for index := range items {
		items[index].Fields["message_meta"] = ""
		items[index].Fields["message_time"] = "今天 10:01"
		items[index].Fields["message_text"], items[index].Text = "重复问题", "重复问题"
	}
	items[0].Fields["message_time"] = "今天 10:00"
	items[3].Fields["body_class"] = ""
	browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
	message, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, cfg)
	full, parseErr := liepinMessages(items)
	if err != nil || parseErr != nil || message.Key != full[2].Key || message.Key == full[1].Key {
		t.Fatalf("尾窗重复编号与完整历史不一致：latest=%s full=%+v err=%v/%v", message.Key, full, err, parseErr)
	}
	oldKey := message.Key
	items = append(items, items[2])
	browser.historyBatches[0] = items
	message, err = readLiepinLatestAutoReplyMessage(context.Background(), browser, cfg)
	full, parseErr = liepinMessages(items)
	if err != nil || parseErr != nil || message.Key != full[4].Key || message.Key == oldKey || full[2].Key != oldKey {
		t.Fatalf("追加同分钟消息后编号漂移：latest=%s old=%s err=%v/%v", message.Key, oldKey, err, parseErr)
	}
}

// TestLiepinLatestRejectsTruncatedDuplicateMinute 验证五千条窗口仍覆盖不全同一分钟时不猜测重复消息编号。
func TestLiepinLatestRejectsTruncatedDuplicateMinute(t *testing.T) {
	cfg := historyAuditConfig()
	cfg.ConversationFields["message_time"] = testLiepinSelector("消息时间")
	items := latestMessageItems(5001, false)
	for index := range items {
		items[index].Fields["message_meta"] = ""
		items[index].Fields["message_time"] = "今天 10:01"
	}
	browser := &latestMessageBrowser{liepinReplyBrowserStub: liepinReplyBrowserStub{historyBatches: [][]contract.FindAllItem{items}}}
	if _, err := readLiepinLatestAutoReplyMessage(context.Background(), browser, cfg); err == nil || !strings.Contains(err.Error(), "重复编号") {
		t.Fatalf("不完整分钟被猜成稳定编号：%v", err)
	}
}
