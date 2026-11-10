// Package boss 本文件验证可见按钮编号交叉核验、仅原页送达检查及全部联系人含置顶行的恢复规则。
package boss

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossIdentityFixture 用三类原生按钮的公开ka构造目标会话，不读取任何隐藏平台状态。
func bossIdentityFixture() (*bossReplyTestBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	b, cfg := bossMessagesFixture(nil)
	b.values = map[string]string{"name": "目标候选人", "candidate": "get_resume_754299372", "phone": "get_phone_754299372", "wechat": "get_wechat_754299372", "thread": "754299372-0", "position": "测试岗位"}
	for key, description := range map[string]string{"message.current_name": "name", "message.current_candidate_id": "candidate", "message.current_phone_candidate_id": "phone", "message.current_wechat_candidate_id": "wechat", "message.current_thread_id": "thread", "message.current_position": "position"} {
		cfg.Selectors[key] = bossReplySelector(description)
	}
	return b, cfg, model.AutoReplyConversationSnapshot{CandidateName: "目标候选人", PlatformCandidateID: "754299372", PlatformThreadID: "754299372-0", CommunicationPosition: "测试岗位"}
}

// TestBossIdentityRejectsSameNameAndMixedNativeIDs 验证同名错编号、三按钮混号、线程错号和缺配置都不能操作。
func TestBossIdentityRejectsSameNameAndMixedNativeIDs(t *testing.T) {
	for _, field := range []string{"candidate", "phone", "wechat", "thread", "missing-config"} {
		b, cfg, snapshot := bossIdentityFixture()
		if field == "missing-config" {
			delete(cfg.Selectors, "message.current_phone_candidate_id")
		} else {
			b.values[field] = strings.ReplaceAll(b.values[field], "754299372", "999999999")
		}
		if err := (&Runtime{}).VerifyAutoReplyConversationIdentity(context.Background(), b, cfg, snapshot); err == nil || b.clicks != 0 {
			t.Fatalf("%s未拒绝不一致身份或发生了点击：%v", field, err)
		}
	}
	b, cfg, snapshot := bossIdentityFixture()
	if err := (&Runtime{}).VerifyAutoReplyConversationIdentity(context.Background(), b, cfg, snapshot); err != nil {
		t.Fatal("三个真实编号一致却不能核验", err)
	}
}

// TestBossDeliveryCannotReopenClosedConversation 验证发送后的聊天关闭时只保留未确认，不重新打开同名会话。
func TestBossDeliveryCannotReopenClosedConversation(t *testing.T) {
	b, cfg, snapshot := bossIdentityFixture()
	delete(b.values, "name")
	snapshot.DeliveryPageURL = cfg.MessagesURL
	err := ensureBossAutoReplyConversation(context.Background(), b, cfg, snapshot)
	if !errors.Is(err, model.ErrAutoReplyConversationNotOpen) || b.clicks != 0 {
		t.Fatalf("关闭后的送达检查重新操作了页面：%v clicks=%d", err, b.clicks)
	}
}

// bossContactFixture 创建带原生key和一个无key置顶行的全部联系人快照。
func bossContactFixture(total int) (*Runtime, *bossReplyTestBrowser, model.Config) {
	b, cfg := bossMessagesFixture(nil)
	cfg.MaxItems = 100
	cfg.Selectors["message.contact_item"] = bossReplySelector("contacts")
	for _, key := range []string{"thread_id", "name", "avatar_url", "last_message", "contact_time"} {
		cfg.ConversationFields[key] = bossReplySelector(key)
	}
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("候选人%d", i)
		b.contacts = append(b.contacts, contract.FindAllItem{Text: name + " 原消息", Fields: map[string]string{"thread_id": fmt.Sprintf("%d-0", 1000+i), "name": name, "avatar_url": fmt.Sprintf("https://img.test/%d", i), "last_message": "原消息"}})
	}
	b.contacts = append(b.contacts, contract.FindAllItem{Text: "置顶候选人 原消息", Fields: map[string]string{"name": "置顶候选人", "avatar_url": "https://img.test/pinned", "last_message": "原消息"}})
	return NewRuntime(), b, cfg
}

// TestBossContactsKeepPinnedRowsAndStableReorder 验证无key置顶行仍被保留，原生会话重排不会换用旧序号详情。
func TestBossContactsKeepPinnedRowsAndStableReorder(t *testing.T) {
	r, b, cfg := bossContactFixture(8)
	first, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(first) != 9 || !strings.HasPrefix(first[8].PlatformThreadID, "local:") || first[0].PlatformCandidateID != "1000" {
		t.Fatalf("置顶行丢失或原生候选编号错误：%+v %v", first, err)
	}
	b.contacts[0], b.contacts[7] = b.contacts[7], b.contacts[0]
	b.requests = nil
	second, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || second[0].Name != "候选人7" || second[0].PlatformThreadID != "1007-0" || second[0].Index != 0 || second[8].PlatformThreadID != first[8].PlatformThreadID || len(b.requests) != 2 {
		t.Fatalf("重排后串人、置顶键改变或重复读取详情：%+v reads=%d err=%v", second, len(b.requests), err)
	}
}

// TestBossContactsReadAfterFirstHundred 验证首百行之外的新来信也会完整读到，完整轻读后只给无key置顶行补头像。
func TestBossContactsReadAfterFirstHundred(t *testing.T) {
	r, b, cfg := bossContactFixture(110)
	first, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(first) != 111 || len(b.requests) != 3 {
		t.Fatalf("首百行后联系人没有补齐：%d %d %v", len(first), len(b.requests), err)
	}
	b.contacts[104].Text = "候选人104 新来信"
	b.contacts[104].Fields["last_message"] = "新来信"
	b.requests = nil
	second, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(second) != 111 || second[104].Summary != "候选人104 新来信" || len(b.requests) != 3 {
		t.Fatalf("首百行后新来信被忽略或全表重复补详情：len=%d reads=%d err=%v", len(second), len(b.requests), err)
	}
}

// TestBossContactsManyUnkeyedUseOneFreshSnapshot 验证多个无编号置顶行只成批补头像，仍保留原生行缓存并捕获新来信。
func TestBossContactsManyUnkeyedUseOneFreshSnapshot(t *testing.T) {
	r, b, cfg := bossContactFixture(8)
	for i := 0; i < 10; i++ {
		b.contacts = append(b.contacts, contract.FindAllItem{Text: fmt.Sprintf("置顶%d 原消息", i), Fields: map[string]string{"name": fmt.Sprintf("置顶%d", i), "avatar_url": fmt.Sprintf("https://img.test/pin/%d", i), "last_message": "原消息"}})
	}
	if _, err := r.readBossContacts(context.Background(), b, cfg); err != nil {
		t.Fatal(err)
	}
	b.requests = nil
	b.contacts[0].Text = "候选人0 新来信"
	b.contacts[0].Fields["last_message"] = "新来信"
	rows, err := r.readBossContacts(context.Background(), b, cfg)
	if err != nil || len(rows) != 19 || rows[0].Summary != "候选人0 新来信" || len(b.requests) != 2 || len(r.contactRows) != 8 {
		t.Fatalf("多置顶重复读取或漏来信：rows=%d reads=%d err=%v", len(rows), len(b.requests), err)
	}
}
