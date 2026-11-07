// Package cloud 本文件验证旧简历及通知待办升级后的严格云端请求、即时重试与幂等回执。
package cloud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestAutoReplyLegacyWireRetry 用临时旧待办重现真实的未知字段错误，验证升级后立即按相同编号重放。
func TestAutoReplyLegacyWireRetry(t *testing.T) {
	ctx := context.Background()
	c, cred, store := entityTestClient(t)
	const legacyError = "这次参数没读明白，请检查后再试"
	const messageKey, reasonKey = "private-message-key", "private-reason-key"
	notice := AutoReplyNotification{ConversationID: "private-thread", PositionID: "position", BasedOnMessageKey: messageKey,
		ReasonKey: reasonKey, CandidateName: "合成候选人", Gender: "男", PlatformID: "platform", Reason: "private-reason",
		LatestMessage: "private-chat", ErrorMessage: "private-error", Status: "failed", CreatedAt: time.Now()}
	noticeID := autoReplyNoticeID(notice)
	candidate := AutoReplyCandidateInput{LocalCandidateID: "local", ResumeSourceMessageID: "private-source-message", PositionID: "position",
		PlatformID: "platform", PlatformAccountID: "account", PlatformCandidateID: "person", Gender: "男", BirthYMPrecision: "month",
		StructuredCandidate: StructuredCandidate{CandidateName: "合成候选人", Phone: "13800000001", BirthYM: "1990-10", WorkExperiences: []CandidateWorkExperience{{CompanyName: "合成公司"}},
			RawText: "private-resume", ColleagueCommunications: []CandidateCommunication{{Content: "private-chat"}}},
		AvatarURL: "private-avatar", BasicInfo: "private-basic"}
	candidateID := "candidate:local:position"
	candidateWire := map[string]any{"position_id": "position", "platform_id": "platform", "platform_account_id": "account", "platform_candidate_id": "person",
		"candidate_name": "合成候选人", "phone": "13800000001", "gender": "男", "birth_ym": "1990-10", "birth_ym_precision": "month",
		"work_experiences": []any{map[string]any{"company_name": "合成公司", "position_name": "", "content": "", "start_ym": "", "end_ym": ""}}}
	noticeWire := map[string]any{"id": noticeID, "position_id": "position", "platform_id": "platform", "candidate_name": "合成候选人", "gender": "男",
		"based_on_message_key": autoReplyDigest(messageKey), "reason_key": autoReplyDigest(reasonKey), "reason": "需要人工协助，请在本地运行记录查看详情。"}
	var sentCandidates, sentNotices atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/me" {
			fmt.Fprint(w, `{"user":{"id":"fixture-owner"}}`)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		switch r.URL.Path {
		case autoReplyAgentBasePath + "/candidates":
			if !reflect.DeepEqual(body, candidateWire) {
				t.Errorf("实际发送的结构化字段与云端契约不一致：%v", body)
				w.WriteHeader(400)
				return
			}
			sentCandidates.Add(1)
			fmt.Fprint(w, `{"ok":true,"candidate_id":"cloud-candidate"}`)
		case autoReplyAgentBasePath + "/notifications":
			if !reflect.DeepEqual(body, noticeWire) {
				t.Errorf("实际发送的通知字段与云端契约不一致：%v", body)
				w.WriteHeader(400)
				return
			}
			sentNotices.Add(1)
			fmt.Fprint(w, `{"ok":true,"email_sent":true}`)
		default:
			t.Errorf("意外路径：%s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c.baseURL = server.URL
	if _, err := c.ValidateSession(ctx, cred.Token); err != nil {
		t.Fatal(err)
	}
	// 直连兼容入口也应走同一发送边界，避免泄露私有字段或触发严格解码失败。
	direct := New(server.URL)
	if _, err := direct.SaveAutoReplyCandidate(ctx, cred, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := direct.NotifyAutoReplyManualHandoff(ctx, cred, notice); err != nil {
		t.Fatal(err)
	}
	rawCandidate, err := json.Marshal(autoReplyCandidateUpload{LocalCandidateID: "local", Input: candidate, Revision: 3})
	if err != nil {
		t.Fatal(err)
	}
	rawNotice, err := json.Marshal(notice)
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		for _, item := range []autoReplyOutboxItem{
			{ID: candidateID, Kind: "candidate", Status: "failed", Payload: rawCandidate, Attempts: 21, NextAttemptAt: future, LastError: legacyError},
			{ID: noticeID, Kind: "notice", Status: "failed", Payload: rawNotice, Attempts: 1, NextAttemptAt: future, LastError: legacyError},
			{ID: "network", Kind: "notice", Status: "failed", Payload: rawNotice, NextAttemptAt: future, LastError: "网络断开"},
			{ID: "completed", Kind: "notice", Status: "completed", Payload: rawNotice, NextAttemptAt: future, LastError: legacyError},
			{ID: "leased", Kind: "candidate", Status: "running", Payload: rawCandidate, LeaseUntil: future},
		} {
			if err := tx.Put("outbox", item.ID, item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 仅回退临时测试库的这条迁移账本，以实际 Migrate 重放旧记录的升级路径。
	db, err := sql.Open("sqlite", filepath.Join(c.localAutoDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`DELETE FROM schema_migrations WHERE version='009_auto_reply_outbox_wire_retry.sql'`); err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = c.RetryAutoReplyOutbox(ctx, cred); err != nil {
			t.Fatal(err)
		}
	}
	if sentCandidates.Load() != 2 || sentNotices.Load() != 2 {
		t.Fatalf("每类应各有一次直连、一次升级重放：%d/%d", sentCandidates.Load(), sentNotices.Load())
	}
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		link, _, err := getAutoReplyEntity[localCloudLink](tx, "cloud_links", "candidate:local")
		if err != nil || link.Value != "cloud-candidate" {
			return fmt.Errorf("云端回执未保存：%v", err)
		}
		for id, expected := range map[string]int{candidateID: 22, noticeID: 2} {
			item, _, err := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
			if err != nil || item.Status != "completed" || item.Attempts != expected {
				return fmt.Errorf("旧待办未保留尝试记录并完成：%s %v", id, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
