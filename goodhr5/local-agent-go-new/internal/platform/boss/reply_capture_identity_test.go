// Package boss 本文件验证读历史期间切换聊天时丢弃整批资料，防止同名不同原生候选人的聊天和手机号串档。
package boss

import (
	"context"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossCaptureIdentityBrowser 在一次真实字段快照返回后模拟外部切会话，不改变读到的原消息正文和指纹。
type bossCaptureIdentityBrowser struct {
	*bossReplyTestBrowser
	change    string
	bodyReads int
}

// FindAll 只在读正文结束时切换可见身份；身份补核不能重新读取消息正文。
func (b *bossCaptureIdentityBrowser) FindAll(ctx context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	items, err := b.bossReplyTestBrowser.FindAll(ctx, request)
	if request.Selector.Description == "messages" && len(request.Fields) > 2 {
		b.bodyReads++
		if b.change == "name" {
			b.values["name"] = "其他候选人"
		}
		if b.change == "native_id" {
			for _, field := range []string{"candidate", "phone", "wechat", "thread"} {
				b.values[field] = strings.ReplaceAll(b.values[field], "754299372", "999999999")
			}
		}
	}
	return items, err
}

// TestBossHistoryRechecksIdentityBeforeReturning 验证普通Open及Read均在历史读取后核身，同名错编号与切人不得带出资料。
func TestBossHistoryRechecksIdentityBeforeReturning(t *testing.T) {
	for _, operation := range []string{"open", "read"} {
		for _, change := range []string{"none", "name", "native_id"} {
			t.Run(operation+"/"+change, func(t *testing.T) {
				base, cfg, snapshot := bossIdentityFixture()
				phone := bossNativePhoneFixture(snapshot.CandidateName, "13800138000")
				cfg.ConversationFields["message_meta"] = bossReplySelector("message_meta")
				base.messages = []contract.FindAllItem{bossTestMessage("04:59", "item-system", "较早系统提示"), bossTestMessage("05:00", "item-friend", "同意交换手机号"), phone}
				parsed, err := bossMessages(base.messages, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				known := []string{parsed[1].Key}
				for key, description := range map[string]string{"message.drawer": "drawer", "message.contact_item": "contacts", "message.contact_click_target": "open-contact", "message.drawer_scroll": "drawer-scroll"} {
					cfg.Selectors[key] = bossReplySelector(description)
				}
				cfg.ConversationFields["thread_id"] = contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "test"}}}, Description: "thread_id", ReadAttribute: "key"}
				cfg.ConversationFields["name"] = bossReplySelector("name")
				base.contacts = []contract.FindAllItem{{Text: snapshot.CandidateName, Fields: map[string]string{"thread_id": snapshot.PlatformThreadID, "name": snapshot.CandidateName}}}
				browser := &bossCaptureIdentityBrowser{bossReplyTestBrowser: base, change: change}
				var messages []model.ConversationMessage
				if operation == "open" {
					conversation := model.Conversation{Name: snapshot.CandidateName, PlatformThreadID: snapshot.PlatformThreadID, PlatformCandidateID: snapshot.PlatformCandidateID, CommunicationPosition: snapshot.CommunicationPosition}
					var opened model.AutoReplyConversationSnapshot
					opened, err = (&Runtime{}).OpenAutoReplyConversation(context.Background(), browser, cfg, conversation, known, 5000)
					messages = opened.Messages
				} else {
					messages, _, err = (&Runtime{}).ReadAutoReplyMessages(context.Background(), browser, cfg, snapshot, known, 5000)
				}
				if browser.bodyReads != 1 {
					t.Fatal("身份补核重新读取了正文或未真正完成历史读取")
				}
				if change != "none" {
					if err == nil || len(messages) != 0 {
						t.Fatal("读历史期间已切换身份，仍把聊天或联系方式返回给旧候选人")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if result, found := bossInfoMessageResult(messages, "phone", nil); !found || result.Phone != "13800138000" || result.EvidenceMessageID != parsed[2].Key {
					t.Fatal("身份未变却丢失原生手机号或改变消息键")
				}
			})
		}
	}
}
