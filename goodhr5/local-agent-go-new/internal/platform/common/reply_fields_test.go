// Package common 本文件验证实际平台配置在联系人与消息节点上分别读取必要字段，不触发无关姓名补读。
package common

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// conversationFieldBrowser 记录传给Worker的实际字段，并只返回请求中包含的合成值。
type conversationFieldBrowser struct {
	model.Browser
	request contract.ElementFindAllRequest
	values  map[string]string
}

// FindAll 模拟标准字段读取，不访问真实招聘页面；未请求的字段不会隐式返回。
func (b *conversationFieldBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.request = request
	fields := make(map[string]string)
	for key := range request.Fields {
		fields[key] = b.values[key]
	}
	return []contract.FindAllItem{{Index: 0, Fields: fields}}, nil
}

// TestActualPlatformConversationFieldScopes 验证四个平台现有混合配置无需迁移，消息不带联系人字段，联系人不带消息字段。
func TestActualPlatformConversationFieldScopes(t *testing.T) {
	for _, platform := range []string{"boss", "liepin", "hliepin", "zhaopin"} {
		t.Run(platform, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", platform, "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			var cfg model.Config
			if err = json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			browser := &conversationFieldBrowser{}
			if _, err = ReadConfiguredConversationMessages(context.Background(), browser, cfg); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"name", "avatar_url", "last_message", "unread_count", "thread_meta", "position_name"} {
				if _, exists := browser.request.Fields[key]; exists {
					t.Fatalf("聊天节点仍请求联系人字段 %s", key)
				}
			}
			for _, key := range []string{"message_text", "message_time", "resume_card", "body_class", "candidate_marker", "self_marker", "message_meta", "candidate_meta", "common_card"} {
				if configured, exists := cfg.ConversationFields[key]; exists && !reflect.DeepEqual(browser.request.Fields[key], configured) {
					t.Fatalf("消息字段 %s 的原选择器丢失", key)
				}
			}
			if !browser.request.FromEnd || browser.request.MaxItems != 5000 {
				t.Fatal("字段分组不能破坏最新五千条读取")
			}
			for _, read := range []func() error{
				func() error {
					_, err := FindConfiguredConversationItems(context.Background(), browser, cfg, "message.contact_item", true)
					return err
				},
				func() error {
					_, err := ScanUnreadConversations(context.Background(), browser, cfg, platform)
					return err
				},
			} {
				if err = read(); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"body_class", "message_text", "message_meta", "message_time", "resume_card", "candidate_meta", "common_card", "candidate_marker", "self_marker"} {
					if _, exists := browser.request.Fields[key]; exists {
						t.Fatalf("联系人节点仍请求消息字段 %s", key)
					}
				}
				for _, key := range []string{"name", "avatar_url", "last_message", "unread_count", "thread_meta", "position_name"} {
					if configured, exists := cfg.ConversationFields[key]; exists && !reflect.DeepEqual(browser.request.Fields[key], configured) {
						t.Fatalf("联系人字段 %s 丢失", key)
					}
				}
			}
			if _, exists := cfg.ConversationFields["name"]; !exists {
				t.Fatal("读取修改了共享配置")
			}
		})
	}
}

// TestLegacyMessageFallbackFieldsStillParse 验证旧方向、编号、正文与附件字段仍实际到达解析器。
func TestLegacyMessageFallbackFieldsStillParse(t *testing.T) {
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"message.item": autoReplyTestSelector("消息")}, ConversationFields: map[string]contract.SelectorSpec{}}
	values := map[string]string{"id": "legacy-id", "text": "附件资料", "sender_role": "candidate", "message_type": "card", "attachment_card": "附件卡片", "candidate_id": "platform-person", "item_class": "receive"}
	for key := range values {
		cfg.ConversationFields[key] = autoReplyTestSelector(key)
	}
	cfg.ConversationFields["name"] = autoReplyTestSelector("不应读取的联系人名")
	browser := &conversationFieldBrowser{values: values}
	items, err := ReadConfiguredConversationMessages(context.Background(), browser, cfg)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := parseConfiguredAutoReplyMessage(items[0], AutoReplyMessageRules{}, time.Now())
	if err != nil || message.Direction != "candidate" || message.PlatformMessageID != "legacy-id" || message.TextContent != "附件资料" || message.MessageType != "card" {
		t.Fatalf("旧消息字段后备被破坏：%+v %v", message, err)
	}
	var card struct {
		CandidateID string `json:"candidate_id"`
		Summary     string `json:"summary"`
	}
	if err = json.Unmarshal(message.CardContent, &card); err != nil || card.CandidateID != "platform-person" || card.Summary != "附件卡片" {
		t.Fatalf("旧卡片字段丢失：%+v %v", card, err)
	}
}
