// Package boss 本文件验证实测微信系统卡的归属、完整值和原消息指纹，测试仅使用合成联系方式。
package boss

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossNativeWechatFixture 按已核验的系统contact-copy卡复刻可见字段，不读取任何隐藏状态。
func bossNativeWechatFixture(owner, value string) contract.FindAllItem {
	text := owner + "的微信号：\n" + value + "\n复制微信号"
	row := bossTestMessage("09:21", "item-system clearfix", text)
	row.Fields["common_card"] = text
	row.Fields["message_meta"] = "message-dialog-icon message-dialog-icon-weixin"
	return row
}

// TestBossNativeReceivedWechatBinding 验证微信卡绑定当前核身姓名后才可消费，数字微信不能串成手机号。
func TestBossNativeReceivedWechatBinding(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	for _, value := range []string{"demo_wechat", "13800138000", "demo-wx_123"} {
		row := bossNativeWechatFixture("测试候选人", value)
		messages, err := bossMessages([]contract.FindAllItem{row}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, found := bossInfoMessageResult(messages, "wechat", nil); found {
			t.Fatal("未核身微信卡已可消费")
		}
		legacy := bossNativeWechatFixture("测试候选人", value)
		delete(legacy.Fields, "message_meta")
		old, err := bossMessages([]contract.FindAllItem{legacy}, now)
		if err != nil || old[0].Key != messages[0].Key {
			t.Fatal("新增微信类型改变了原消息键")
		}
		if err = bindBossNativeInfoCards(messages, "测试候选人"); err != nil {
			t.Fatal(err)
		}
		result, found := bossInfoMessageResult(messages, "wechat", nil)
		if !found || result.Status != model.AutoReplyInfoReceived || result.Wechat != value || result.Phone != "" || result.EvidenceMessage == nil || result.EvidenceMessage.Direction != "system" || result.EvidenceMessageID != old[0].Key {
			t.Fatal("微信卡未形成归属准确的完整原消息回执")
		}
		if _, found = bossInfoMessageResult(messages, "phone", nil); found {
			t.Fatal("微信卡被当成手机号")
		}
		if err = bindBossNativeInfoCards(messages, "另一候选人"); err != nil {
			t.Fatal(err)
		}
		if _, found = bossInfoMessageResult(messages, "wechat", nil); found {
			t.Fatal("换人后微信卡仍可消费")
		}
	}
}

// TestBossNativeReceivedWechatRejectsUnprovenCards 验证普通文字、其他归属、掩码和近似原生结构均不能收入候选微信。
func TestBossNativeReceivedWechatRejectsUnprovenCards(t *testing.T) {
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct{ candidate, bodyClass, meta, value, suffix string }{
		{"", "item-system", "message-dialog-icon-weixin", "demo_wechat", ""},
		{"其他人", "item-system", "message-dialog-icon-weixin", "demo_wechat", ""},
		{"测试候选人", "item-friend", "message-dialog-icon-weixin", "demo_wechat", ""},
		{"测试候选人", "item-myself", "message-dialog-icon-weixin", "demo_wechat", ""},
		{"测试候选人", "item-system-like", "message-dialog-icon-weixin", "demo_wechat", ""},
		{"测试候选人", "item-system", "message-dialog-icon-weixin-like", "demo_wechat", ""},
		{"测试候选人", "item-system", "message-dialog-icon-contact", "demo_wechat", ""},
		{"测试候选人", "item-system", "", "demo_wechat", ""},
		{"测试候选人", "item-system", "message-dialog-icon-weixin", "demo***chat", ""},
		{"测试候选人", "item-system", "message-dialog-icon-weixin", "short", ""},
		{"测试候选人", "item-system", "message-dialog-icon-weixin", "abcdefghijklmnopqrstu", ""},
		{"测试候选人", "item-system", "message-dialog-icon-weixin", "demo_wechat", "额外内容"},
	} {
		row := bossNativeWechatFixture("测试候选人", test.value)
		row.Fields["body_class"], row.Fields["message_meta"] = test.bodyClass, test.meta
		row.Fields["common_card"] += test.suffix
		messages, err := bossMessages([]contract.FindAllItem{row}, now)
		if err != nil {
			t.Fatal(err)
		}
		if err = bindBossNativeInfoCards(messages, test.candidate); err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"wechat", "phone"} {
			if _, found := bossInfoMessageResult(messages, kind, nil); found {
				t.Fatal("未经证明的卡片产生联系方式", test)
			}
		}
	}
}

// TestBossNativeWechatFieldPipeline 验证真实配置的系统卡、精确复制按钮和微信图标限制一路传递到资料解析。
func TestBossNativeWechatFieldPipeline(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var real model.Config
	if err = json.Unmarshal(raw, &real); err != nil {
		t.Fatal(err)
	}
	meta := real.ConversationFields["message_meta"]
	selector := ""
	for _, target := range meta.Target.Selectors {
		if strings.Contains(target.Value, "message-dialog-icon-weixin") {
			selector = target.Value
		}
	}
	for _, part := range []string{".item-system .contact-copy", ".message-card-wrap", ".message-card-buttons > .card-btn:text-is(\"复制微信号\")", ".message-dialog-icon-weixin"} {
		if !strings.Contains(selector, part) {
			t.Fatal("微信原生结构读取限制缺失", part)
		}
	}
	rows := []contract.FindAllItem{bossTestMessage("昨天 20:00", "item-system", "旧消息"), bossTestMessage("09:21", "item-friend", "可以交换微信"), bossNativeWechatFixture("测试候选人", "demo_wechat")}
	b, cfg := bossMessagesFixture(rows)
	cfg.ConversationFields["message_meta"] = meta
	messages, err := readBossInfoMessages(context.Background(), b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.requests) == 0 || b.requests[0].Fields["message_meta"].ReadAttribute != "class" {
		t.Fatal("消息读取未保留图标类名")
	}
	if err = bindBossNativeInfoCards(messages, "测试候选人"); err != nil {
		t.Fatal(err)
	}
	if result, found := bossInfoMessageResult(messages, "wechat", nil); !found || result.Wechat != "demo_wechat" || result.Status != model.AutoReplyInfoReceived {
		t.Fatal("原生字段至微信回执链路未完成")
	}
}
