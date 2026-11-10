// Package cloud 本文件验证文字意图的配置版本绑定、空话术禁发和旧记录兼容，不执行招聘页面动作。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/storage"
)

// textPolicyTestSnapshot 创建带一条明确话术的完整合成岗位快照，便于区分缺库与明确停用。
func textPolicyTestSnapshot() AutoReplyPositionSnapshot {
	return AutoReplyPositionSnapshot{Position: AutoReplyPosition{ID: "position", PlatformID: "liepin"}, Config: PositionAutoReplyConfig{PositionID: "position", Enabled: true, Version: 1, PhrasesVersion: "library", Phrases: []AutoReplyPhrase{{ID: "receive_resume", Content: "简历收到了。"}}}, CompanyProfile: CompanyProfile{ID: "company"}, Subscription: AutoReplySubscription{Active: true, AllowAutoReply: true}}
}

// TestTextPolicyFreezesIntentAndRejectsWrongPosition 验证版本和话术写入既有发送JSON，错误岗位上下文不能借用会话。
func TestTextPolicyFreezesIntentAndRejectsWrongPosition(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	conv := entityTestConversation(t, c, cred)
	snapshot := textPolicyTestSnapshot()
	snapshot.Position.ID, snapshot.Config.PositionID = conv.PositionID, conv.PositionID
	snapshot.Position.PlatformID = conv.PlatformID
	ctx := WithAutoReplyTextPolicy(context.Background(), snapshot, "receive_resume")
	if _, ok, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "简历收到了。", 0); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		row, found, err := tx.Get("sends", autoReplySendID(conv.ID, "m1", 0))
		if err != nil || !found {
			t.Fatal("没有既有发送意图", found, err)
		}
		var intent struct {
			ContextRevision string `json:"context_revision"`
			PhraseID        string `json:"phrase_id"`
		}
		if json.Unmarshal(row.Data, &intent) != nil || intent.ContextRevision != AutoReplyGoalContextRevision(snapshot) || intent.PhraseID != "receive_resume" {
			t.Fatal("文字配置没有按可选字段冻结")
		}
		return ValidateAutoReplyQueuedText(tx, autoReplySendID(conv.ID, "m1", 0), conv.ID, "简历收到了。", snapshot)
	}); err != nil {
		t.Fatal(err)
	}
	snapshot.Position.ID = "other-position"
	if _, ok, err := c.ClaimAutoReplySend(WithAutoReplyTextPolicy(context.Background(), snapshot, ""), cred, conv.ID, "m2", "普通回复。", 0); !errors.Is(err, ErrAutoReplyTextPolicyChanged) || ok {
		t.Fatal("其他岗位的上下文借用了当前会话", ok, err)
	}
}

// TestTextPolicyRespectsExplicitBlankDeletedAndLegacyLibrary 区分管理员空话术、删除标记、完整空库与旧服务没有提供话术库。
func TestTextPolicyRespectsExplicitBlankDeletedAndLegacyLibrary(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  PositionAutoReplyConfig
		id   string
		want bool
	}{
		{"正常话术", PositionAutoReplyConfig{Phrases: []AutoReplyPhrase{{ID: "receive_resume", Content: "已收到"}}}, "receive_resume", true},
		{"明确清空", PositionAutoReplyConfig{Phrases: []AutoReplyPhrase{{ID: "receive_resume", Content: " \n "}}}, "receive_resume", false},
		{"删除仍有旧内容", PositionAutoReplyConfig{Phrases: []AutoReplyPhrase{{ID: "receive_resume", Content: "旧内容", Deleted: true}}}, "receive_resume", false},
		{"完整空库", PositionAutoReplyConfig{Phrases: []AutoReplyPhrase{}}, "receive_resume", false},
		{"版本化缺失", PositionAutoReplyConfig{PhrasesVersion: "present"}, "receive_resume", false},
		{"旧签收兼容", PositionAutoReplyConfig{}, "receive_resume", true},
		{"旧简历索要兼容", PositionAutoReplyConfig{}, "request_resume", true},
		{"旧库不猜新类别", PositionAutoReplyConfig{}, "request_wechat", false},
		{"普通答案不要求话术", PositionAutoReplyConfig{Phrases: []AutoReplyPhrase{}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoReplyTextPhraseEnabled(tc.cfg, tc.id); got != tc.want {
				t.Fatal("话术停发或旧库兼容不符", got, tc.want)
			}
		})
	}
}

// TestTextPolicyRejectsBlankWithCurrentRevision 验证即使草稿版本正好等于当前配置，明确空话术仍不能被登记的旧正文绕过。
func TestTextPolicyRejectsBlankWithCurrentRevision(t *testing.T) {
	c, cred, _ := entityTestClient(t)
	conv := entityTestConversation(t, c, cred)
	snapshot := textPolicyTestSnapshot()
	snapshot.Position.PlatformID = conv.PlatformID
	snapshot.Config.Phrases[0].Content = ""
	ctx := WithAutoReplyTextPolicy(context.Background(), snapshot, "receive_resume")
	if _, ok, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "旧的签收内容", 0); err != nil || !ok {
		t.Fatal(ok, err)
	}
	err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return ValidateAutoReplyQueuedText(tx, autoReplySendID(conv.ID, "m1", 0), conv.ID, "旧的签收内容", snapshot)
	})
	if !errors.Is(err, ErrAutoReplyTextPolicyChanged) {
		t.Fatal("相同配置版本绕过了明确空话术", err)
	}
}
