// Package cloud 本文件用独立临时仓储验证原生交换号码、附件不同号码与冲突账本的兼容性，不访问招聘页面或真实数据库。
package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

const contactPhoneA = "13800138001"
const contactPhoneB = "13800138002"

// nativePhoneConflictFixture 建立同一候选人的actor、原生身份、收到的系统交换卡及候选人附件来源。
func nativePhoneConflictFixture(t *testing.T, scenario string) (*Client, AgentCredentials, AutoReplyConversation, AutoReplyCandidateInput) {
	t.Helper()
	store, err := storage.OpenAutoReplyScratch(&onlineHeaderAuthority{data: map[string]storage.AutoReplyStateChange{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := New("")
	c.EnableLocalAutoReply(store, t.TempDir())
	cred := AgentCredentials{Token: "fixture-token", MachineID: "fixture-machine"}
	ctx := context.Background()
	identity, err := c.SaveAutoReplyIdentity(ctx, cred, CandidatePlatformIdentity{PlatformID: "boss", PlatformAccountID: "account-one", PlatformCandidateID: "native-one", CandidateName: "测试候选人"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := c.SaveAutoReplyConversation(ctx, cred, AutoReplyConversation{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread-one", PlatformIdentityID: identity.ID, CandidateName: "测试候选人", PositionID: "position-one"})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, conversation)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.SetAutoReplyInfoRequestProgress(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoReceived, "native-phone"); err != nil {
		t.Fatal(err)
	}
	if err = c.SaveAutoReplyNativeContact(ctx, cred, conversation.ID, "phone", model.AutoReplyInfoRequestResult{Status: model.AutoReplyInfoReceived, Phone: contactPhoneA}); err != nil {
		t.Fatal(err)
	}
	phoneCard := AutoReplyMessage{PlatformMessageID: "native-phone", Direction: "system", MessageType: "card", TextContent: "测试候选人的手机号：" + contactPhoneA + "复制手机号"}
	card := map[string]string{"info_type": "phone", "info_status": "received", "phone": contactPhoneA, "owner_name": "测试候选人", "summary": phoneCard.TextContent}
	switch scenario {
	case "hr-card":
		phoneCard.Direction = "self"
	case "candidate-text":
		phoneCard.Direction, phoneCard.MessageType = "candidate", "text"
	case "system-text":
		phoneCard.MessageType = "text"
	case "wrong-owner":
		card["owner_name"] = "HR姓名"
	case "forged-summary":
		phoneCard.TextContent, card["summary"] = "仅普通文字没有真实交换资料", "仅普通文字没有真实交换资料"
	case "requested-only":
		card["info_status"] = "requested"
	case "masked-card":
		card["phone"] = "138****8001"
	case "no-native":
		card = map[string]string{}
	}
	phoneCard.CardContent, _ = json.Marshal(card)
	syncEntityMessages(t, c, cred, conversation, phoneCard, AutoReplyMessage{PlatformMessageID: "attachment-source", Direction: "candidate", MessageType: "resume", TextContent: "附件简历已发送"})
	if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		row, _, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", candidate.ID)
		if err != nil {
			return err
		}
		row.OnlineProfile = &AutoReplyOnlineProfileState{CandidateID: "canonical-one", ProfileKey: "unchanged-online-profile"}
		if err = tx.Put("candidates", candidate.ID, row); err != nil {
			return err
		}
		return tx.Put("cloud_links", "candidate:"+candidate.ID, localCloudLink{Value: "canonical-one"})
	}); err != nil {
		t.Fatal(err)
	}
	input := AutoReplyCandidateInput{LocalCandidateID: candidate.ID, PositionID: "position-one", PlatformID: "boss", PlatformAccountID: "account-one", PlatformCandidateID: "native-one", ResumeSourceMessageID: "attachment-source", StructuredCandidate: StructuredCandidate{CandidateName: "测试候选人", Phone: contactPhoneB, RawText: "本次附件正文仍保留号码 " + contactPhoneB}}
	if scenario == "same-format" {
		input.Phone = "+86 138-0013-8001"
	}
	if scenario == "missing-source" {
		input.ResumeSourceMessageID = "not-observed"
	}
	if scenario == "identity-differs" {
		if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
			current, _, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", identity.ID)
			if err != nil {
				return err
			}
			current.NormalizedPhone = "13800138003"
			return tx.Put("identities", identity.ID, current)
		}); err != nil {
			t.Fatal(err)
		}
	}
	return c, cred, conversation, input
}

// TestNativePhoneAttachmentMergeRequiresStrongEvidence 验证三处同号才能保护，HR卡、伪造文本、缺来源及旧无证据路径保持原合并规则。
func TestNativePhoneAttachmentMergeRequiresStrongEvidence(t *testing.T) {
	for _, scenario := range []string{"confirmed", "same-format", "hr-card", "candidate-text", "system-text", "wrong-owner", "forged-summary", "requested-only", "masked-card", "no-native", "missing-source", "identity-differs"} {
		t.Run(scenario, func(t *testing.T) {
			c, cred, conversation, input := nativePhoneConflictFixture(t, scenario)
			ctx := context.Background()
			if _, err := c.SaveAutoReplyCandidate(ctx, cred, input); err != nil {
				t.Fatal(err)
			}
			state, err := c.AutoReplyCandidateState(ctx, cred, AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformThreadID: "thread-one", PlatformCandidateID: "native-one"})
			if err != nil || state.Candidate == nil || state.Identity == nil {
				t.Fatal("合并后身份或候选人丢失", err)
			}
			want := contactPhoneB
			if scenario == "confirmed" || scenario == "same-format" {
				want = contactPhoneA
			}
			if state.Candidate.Phone != want || state.Identity.NormalizedPhone != want || !state.Candidate.ResumeStructured || state.Candidate.RawText != input.RawText || state.Candidate.ResumeSourceMessageID != input.ResumeSourceMessageID {
				t.Fatal("号码优先级、附件原文或来源不正确")
			}
			if state.Candidate.OnlineProfile == nil || state.Candidate.OnlineProfile.CandidateID != "canonical-one" || structuredLocalAutoReplyCandidate(*state.Candidate).Phone != want {
				t.Fatal("附件冲突改变canonical编号或下次发布的号码")
			}
			var audit json.RawMessage
			if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
				link, _, e := getAutoReplyEntity[localCloudLink](tx, "cloud_links", "candidate:"+input.LocalCandidateID)
				if e != nil || link.Value != "canonical-one" {
					return fmtConflictTestError(e)
				}
				message, found, e := storedContactConflictSource(tx, conversation, "attachment-source")
				if e != nil || !found {
					return fmtConflictTestError(e)
				}
				id := "contact-conflict:" + autoReplyDigest(conversation.ID+"|"+conversation.PositionID+"|phone|"+message.ID)
				audit, e = tx.ServerLedger("runtime_events", id, nil)
				return e
			}); err != nil {
				t.Fatal(err)
			}
			if (len(audit) > 0) != (scenario == "confirmed") {
				t.Fatal("格式等价或无强证据旧路径错误登记了冲突")
			}
			if strings.Contains(string(audit), contactPhoneA) || strings.Contains(string(audit), contactPhoneB) {
				t.Fatal("冲突审计泄漏了具体号码")
			}
			if scenario == "confirmed" {
				before := string(audit)
				if _, err = c.SaveAutoReplyCandidate(ctx, cred, input); err != nil {
					t.Fatal(err)
				}
				if err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
					message, _, e := storedContactConflictSource(tx, conversation, "attachment-source")
					if e != nil {
						return e
					}
					id := "contact-conflict:" + autoReplyDigest(conversation.ID+"|"+conversation.PositionID+"|phone|"+message.ID)
					audit, e = tx.ServerLedger("runtime_events", id, nil)
					return e
				}); err != nil || string(audit) != before {
					t.Fatal("相同附件反复生成冲突事件或改变首次发生时间", err)
				}
			}
		})
	}
}

// fmtConflictTestError 为夹具缺失提供固定错误，避免错误路径将号码或原文写入测试输出。
func fmtConflictTestError(cause error) error {
	if cause != nil {
		return cause
	}
	return fmt.Errorf("候选人canonical关联或来源消息不正确")
}

// TestContactComparableValuesRejectMasked 验证格式归一不会把掩码或空值变成已核实手机号。
func TestContactComparableValuesRejectMasked(t *testing.T) {
	if !AutoReplyContactValuesEqual("phone", contactPhoneA, "+86 138-0013-8001") || AutoReplyContactValuesEqual("phone", "138****8001", contactPhoneA) || AutoReplyContactValuesEqual("phone", "", "") {
		t.Fatal("完整号码与格式或掩码的区分不正确")
	}
}

// TestIdentityEmptySnapshotPreservesOnlySameBoundPhone 验证普通身份刷新只在同一平台、账号、原生候选人与actor保留既有号码，不能借其它身份补号码。
func TestIdentityEmptySnapshotPreservesOnlySameBoundPhone(t *testing.T) {
	c, cred, _, _ := nativePhoneConflictFixture(t, "confirmed")
	ctx := context.Background()
	lookup := AutoReplyCandidateLookup{PlatformID: "boss", PlatformAccountID: "account-one", PlatformCandidateID: "native-one", PlatformThreadID: "thread-one"}
	state, err := c.AutoReplyCandidateState(ctx, cred, lookup)
	if err != nil || state.Identity == nil || state.Candidate == nil {
		t.Fatal("测试身份未建立", err)
	}
	before := *state.Identity
	input := CandidatePlatformIdentity{CandidateID: state.Candidate.ID, PlatformID: before.PlatformID, PlatformAccountID: before.PlatformAccountID, PlatformCandidateID: before.PlatformCandidateID, CandidateName: "测试候选人", NormalizedPhone: " \t "}
	for repeat := 0; repeat < 2; repeat++ {
		saved, err := c.SaveAutoReplyIdentity(ctx, cred, input)
		if err != nil || saved.ID != before.ID || saved.NormalizedPhone != contactPhoneA || !saved.FirstSeenAt.Equal(before.FirstSeenAt) {
			t.Fatal("空快照清掉了同一已绑定身份号码或首次记录时间", err)
		}
	}
	for _, change := range []string{"platform", "account", "native"} {
		other := input
		switch change {
		case "platform":
			other.PlatformID = "other-platform"
		case "account":
			other.PlatformAccountID = "other-account"
		case "native":
			other.PlatformCandidateID = "other-native"
		}
		other.NormalizedPhone = ""
		saved, err := c.SaveAutoReplyIdentity(ctx, cred, other)
		if err != nil || saved.NormalizedPhone != "" {
			t.Fatal("不同原生身份借用了已有号码", err)
		}
	}
}
