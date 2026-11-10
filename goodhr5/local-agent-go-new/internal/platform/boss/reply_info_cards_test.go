// Package boss 本文件按实测原生卡片字段验证资料回执、候选归属和旧消息指纹，所有联系方式均为合成数据。
package boss

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossNativeGenericRequestReceipt 验证真实泛联系方式系统回执仅能归属于本次已执行动作的新消息，历史不能猜类型。
func TestBossNativeGenericRequestReceipt(t *testing.T) {
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	row := bossTestMessage("05:00", "item-system", "请求交换联系方式已发送")
	messages, err := bossMessages([]contract.FindAllItem{row, row}, now)
	if err != nil {
		t.Fatal(err)
	}
	first := messages[0]
	expected := common.StableAutoReplyMessageKey(fmt.Sprintf("boss-visible-v1|%s|system|system|%s|occurrence:1", first.SentAt.UTC().Format(time.RFC3339), row.Fields["message_text"]))
	if first.Direction != "system" || first.MessageType != "system" || first.Key != expected || first.Key == messages[1].Key {
		t.Fatal("泛系统回执改变了旧类型、指纹或同分钟重复次序")
	}
	for _, kind := range []string{"phone", "wechat"} {
		if _, found := bossInfoMessageResult(messages, kind, nil); found {
			t.Fatal("历史泛联系方式提示被猜为具体资料")
		}
		if _, found := bossInfoMessageResult(messages[:1], kind, map[string]bool{first.Key: true}); found {
			t.Fatal("点击前的旧提示被当作本次动作回执")
		}
		result, found := bossInfoMessageResult(messages, kind, map[string]bool{first.Key: true})
		if !found || result.Status != model.AutoReplyInfoRequested || result.EvidenceMessageID != messages[1].Key || result.Phone != "" || result.Wechat != "" || result.EvidenceMessage == nil || result.EvidenceMessage.Key != messages[1].Key {
			t.Fatal("本次明确动作的新系统提示没有成为准确回执")
		}
		var native bossInfoCard
		_ = json.Unmarshal(result.EvidenceMessage.CardContent, &native)
		if native.InfoType != "contact" || result.EvidenceMessage.Direction != "system" || result.EvidenceMessage.MessageType != "system" || result.EvidenceMessage.TextContent != row.Fields["message_text"] {
			t.Fatal("回传原消息时把泛联系方式提示永久改成了具体资料或修改了正文")
		}
	}
	if _, found := bossInfoMessageResult(messages, "resume", map[string]bool{}); found {
		t.Fatal("联系方式系统提示被当作简历回执")
	}
	for _, direction := range []string{"item-friend", "item-myself", ""} {
		fake := row
		fake.Fields = map[string]string{"message_time": "05:00", "message_text": row.Fields["message_text"], "body_class": direction}
		parsed, err := bossMessages([]contract.FindAllItem{fake}, now)
		if err != nil {
			t.Fatal(err)
		}
		var card bossInfoCard
		_ = json.Unmarshal(parsed[0].CardContent, &card)
		if card.InfoType != "" {
			t.Fatal("普通文字或缺失原生系统容器的消息冒充原生回执")
		}
	}
}

// TestBossNativeWechatRequestReceipt 验证实测微信系统文案明确归类，保留原消息键，普通文字或近似文案不得冒充。
func TestBossNativeWechatRequestReceipt(t *testing.T) {
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	row := bossTestMessage("05:00", "item-system", "请求交换微信已发送")
	messages, err := bossMessages([]contract.FindAllItem{row}, now)
	if err != nil {
		t.Fatal(err)
	}
	message := messages[0]
	expected := common.StableAutoReplyMessageKey(fmt.Sprintf("boss-visible-v1|%s|system|system|%s|occurrence:1", message.SentAt.UTC().Format(time.RFC3339), row.Fields["message_text"]))
	result, found := bossInfoMessageResult(messages, "wechat", nil)
	if !found || result.Status != model.AutoReplyInfoRequested || result.EvidenceMessageID != expected || result.EvidenceMessage == nil || result.EvidenceMessage.Direction != "system" || result.EvidenceMessage.MessageType != "system" || result.EvidenceMessage.TextContent != row.Fields["message_text"] {
		t.Fatal("实测微信请求没有形成原样的系统回执")
	}
	if _, found := bossInfoMessageResult(messages, "phone", map[string]bool{}); found {
		t.Fatal("明确微信请求被误认成手机号请求")
	}
	if _, found := bossInfoMessageResult(messages, "wechat", map[string]bool{message.Key: true}); found {
		t.Fatal("旧微信请求被当作本次新回执")
	}
	for _, test := range []struct{ direction, text string }{
		{"item-friend", row.Fields["message_text"]}, {"item-myself", row.Fields["message_text"]}, {"", row.Fields["message_text"]},
		{"item-system", "请求交换微信已发送成功"}, {"item-system", "对方已同意交换微信"}, {"item-system", "对方拒绝交换微信"},
	} {
		parsed, err := bossMessages([]contract.FindAllItem{bossTestMessage("05:00", test.direction, test.text)}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, found := bossInfoMessageResult(parsed, "wechat", map[string]bool{}); found {
			t.Fatal("普通文字或未经实测的文案被当作微信回执")
		}
	}
}

// bossNativePhoneFixture 仅复刻实测系统contact-copy卡的可见字段，使用虚拟号码而非真实候选资料。
func bossNativePhoneFixture(owner, phone string) contract.FindAllItem {
	text := owner + "的手机号：\n" + phone + "\n复制手机号"
	row := bossTestMessage("05:00", "item-system clearfix", text)
	row.Fields["common_card"] = text
	row.Fields["message_meta"] = "message-dialog-icon message-dialog-icon-contact"
	return row
}

// TestBossNativeReceivedPhoneBinding 验证真实原生卡、完整值与当前核身姓名同时成立才收入候选档案，旧正文及消息键保持不变。
func TestBossNativeReceivedPhoneBinding(t *testing.T) {
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	row := bossNativePhoneFixture("测试候选人", "13800138000")
	messages, err := bossMessages([]contract.FindAllItem{row}, now)
	if err != nil {
		t.Fatal(err)
	}
	var unbound bossInfoCard
	if err = json.Unmarshal(messages[0].CardContent, &unbound); err != nil || unbound.InfoType != "contact" || unbound.OwnerName != "测试候选人" {
		t.Fatal("原生卡在核身前没有保留待核对的归属")
	}
	if _, found := bossInfoMessageResult(messages, "phone", nil); found {
		t.Fatal("未绑定的原生联系方式已当作候选人手机号")
	}
	legacy := bossNativePhoneFixture("测试候选人", "13800138000")
	delete(legacy.Fields, "message_meta")
	old, err := bossMessages([]contract.FindAllItem{legacy}, now)
	if err != nil || old[0].Key != messages[0].Key {
		t.Fatal("新增typed信息改变旧卡片指纹")
	}
	if err = bindBossNativeInfoCards(messages, "测试候选人"); err != nil {
		t.Fatal(err)
	}
	result, found := bossInfoMessageResult(messages, "phone", nil)
	if !found || result.Status != model.AutoReplyInfoReceived || result.Phone != "13800138000" || result.Wechat != "" || result.EvidenceMessageID != messages[0].Key {
		t.Fatal("候选原生手机号卡没有成为有证据的received结果")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded model.AutoReplyInfoRequestResult
	if err = json.Unmarshal(raw, &decoded); err != nil || decoded.EvidenceMessage == nil || decoded.EvidenceMessage.Key != result.EvidenceMessageID || decoded.EvidenceMessage.Direction != "system" || decoded.EvidenceMessage.TextContent != row.Fields["message_text"] {
		t.Fatal("真实回执原消息在标准JSON传输中丢失或变形")
	}
	if messages[0].Direction != "system" || messages[0].MessageType != "card" || messages[0].TextContent != row.Fields["message_text"] || messages[0].Key != old[0].Key {
		t.Fatal("资料绑定修改了聊天方向、类型、正文或指纹")
	}
	if _, found := bossInfoMessageResult(messages, "wechat", nil); found {
		t.Fatal("手机号卡串成微信结果")
	}
	boundary := messages[0].SentAt.Add(time.Minute)
	if len(bossInfoMessagesAfter(messages, &boundary)) != 0 {
		t.Fatal("删除前的资料卡越过了新沟通边界")
	}
	if err = bindBossNativeInfoCards(messages, "另一候选人"); err != nil {
		t.Fatal(err)
	}
	if _, found := bossInfoMessageResult(messages, "phone", nil); found {
		t.Fatal("绑定后的卡片换人仍保留候选手机号")
	}
}

// TestBossNativeReceivedPhoneRejectsUnprovenCards 验证普通文字、HR归属、掩码、相似图标及未实测微信或拒绝文案不能成为已获得资料。
func TestBossNativeReceivedPhoneRejectsUnprovenCards(t *testing.T) {
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	cases := []struct {
		name, candidate string
		change          func(*contract.FindAllItem)
	}{
		{"HR的手机号", "当前候选人", nil},
		{"缺少当前姓名", "", nil},
		{"普通候选文字", "测试候选人", func(row *contract.FindAllItem) { row.Fields["body_class"] = "item-friend" }},
		{"普通HR文字", "测试候选人", func(row *contract.FindAllItem) { row.Fields["body_class"] = "item-myself" }},
		{"缺少系统容器", "测试候选人", func(row *contract.FindAllItem) { row.Fields["body_class"] = "" }},
		{"缺少原生图标", "测试候选人", func(row *contract.FindAllItem) { delete(row.Fields, "message_meta") }},
		{"相似图标", "测试候选人", func(row *contract.FindAllItem) { row.Fields["message_meta"] = "fake-message-dialog-icon-contact" }},
		{"掩码值", "测试候选人", func(row *contract.FindAllItem) {
			row.Fields["common_card"] = "测试候选人的手机号：138****8000\n复制手机号"
		}},
		{"正文拼凑", "测试候选人", func(row *contract.FindAllItem) { row.Fields["common_card"] += "\n其他号码13800138001" }},
		{"未实测微信卡", "测试候选人", func(row *contract.FindAllItem) {
			row.Fields["common_card"] = "测试候选人的微信号：example_123\n复制微信号"
		}},
		{"未实测拒绝", "测试候选人", func(row *contract.FindAllItem) { row.Fields["common_card"] = "对方拒绝交换手机号" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			row := bossNativePhoneFixture("测试候选人", "13800138000")
			if test.change != nil {
				test.change(&row)
			}
			messages, err := bossMessages([]contract.FindAllItem{row}, now)
			if err != nil {
				t.Fatal(err)
			}
			if err = bindBossNativeInfoCards(messages, test.candidate); err != nil {
				t.Fatal(err)
			}
			if _, found := bossInfoMessageResult(messages, "phone", nil); found {
				t.Fatal("无可靠候选归属或原生证据的卡片成为received")
			}
			var card bossInfoCard
			_ = json.Unmarshal(messages[0].CardContent, &card)
			if card.Phone != "" || card.Wechat != "" {
				t.Fatal("不可信联系方式仍留在可消费的typed字段")
			}
		})
	}
}

// TestBossNativeInfoFieldPipeline 验证实际BOSS配置读取既有message_meta字段并经消息解析和姓名绑定得到真实手机号回执。
func TestBossNativeInfoFieldPipeline(t *testing.T) {
	raw, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var real model.Config
	if err = json.Unmarshal(raw, &real); err != nil {
		t.Fatal(err)
	}
	meta, found := real.ConversationFields["message_meta"]
	if !found || meta.ReadAttribute != "class" || len(meta.Target.Selectors) < 1 {
		t.Fatal("真实配置没有提供原生图标类名证明")
	}
	selector := meta.Target.Selectors[0].Value
	for _, part := range []string{".item-system", ".contact-copy", ".message-card-wrap", ".message-card-buttons", ".card-btn", ".message-dialog-icon-contact"} {
		if !strings.Contains(selector, part) {
			t.Fatal("原生图标读取缺少已核实的结构限制")
		}
	}
	rows := []contract.FindAllItem{bossTestMessage("昨天 20:00", "item-system", "旧消息"), bossTestMessage("05:00", "item-friend", "可以交换手机号"), bossNativePhoneFixture("测试候选人", "13800138000")}
	b, cfg := bossMessagesFixture(rows)
	cfg.ConversationFields["message_meta"] = meta
	messages, err := readBossInfoMessages(context.Background(), b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.requests) == 0 || b.requests[0].Fields["message_meta"].ReadAttribute != "class" {
		t.Fatal("消息读取链路丢弃了原生图标字段")
	}
	if err = bindBossNativeInfoCards(messages, "测试候选人"); err != nil {
		t.Fatal(err)
	}
	if result, found := bossInfoMessageResult(messages, "phone", nil); !found || result.Status != model.AutoReplyInfoReceived || result.Phone != "13800138000" {
		t.Fatal("FindAll字段到typed手机号回执链路不完整")
	}
}
