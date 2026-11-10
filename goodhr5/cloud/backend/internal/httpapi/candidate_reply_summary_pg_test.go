// 本文件用独立PostgreSQL验证在线临时简历无cloud_links时仍有历史，并隔离成员、会话、来源摘要和当前联系方式。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// TestCandidateOnlineHistoryIncludesTemporaryProfile 验证真实在线建档仅有精确canonical缓存时也能展示会话、摘要、AI消息和三类进度。
func TestCandidateOnlineHistoryIncludesTemporaryProfile(t *testing.T) {
	f, candidateStore, in := onlineProfileFixture(t)
	ctx := context.Background()
	result, err := candidateStore.saveOnlineProfile(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	cacheOnlineFixture(t, f, in, result)
	if _, err = f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(jsonb_set(payload,'{online_profile,match_status}',to_jsonb('temporary'::text)),'{online_profile,profile,mode}',to_jsonb('ai'::text)) WHERE tenant_id=$1 AND user_id=$2 AND kind='candidates' AND id='online-local'`, f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
	info := `{"resume":{"status":"requested","updated_at":"2026-11-10T01:03:00Z"},"phone":{"status":"deferred","updated_at":"2026-11-10T01:04:00Z"}}`
	if _, err = f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=jsonb_set(payload,'{info_requests}',$3::jsonb) WHERE tenant_id=$1 AND user_id=$2 AND kind='conversations' AND id='online-thread'`, f.tenant, f.user, info); err != nil {
		t.Fatal(err)
	}
	f.entity(t, "messages", "online-answer", map[string]any{"conversation_id": "online-thread", "direction": "self", "message_type": "text", "platform_message_id": "answer-key", "text_content": "平台资料索要：phone"})
	f.entity(t, "sends", "online-answer-send", map[string]any{"conversation_id": "online-thread", "status": "sent", "outbound_message_key": "answer-key", "message_key": "normal-answer", "content": "平台资料索要：phone"})
	var links int
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE kind='cloud_links' AND tenant_id=$1", f.tenant).Scan(&links); err != nil || links != 0 {
		t.Fatal("测试应覆盖尚无cloud_links的真实在线临时档", links, err)
	}
	scope := CandidateLibraryScope{TenantID: f.tenant, UserEmail: in.Scope.UserEmail}
	selected := f.user + ":online-thread"
	query := url.Values{"thread_id": {selected}}
	history, err := f.store.candidateHistory(ctx, scope, result.CandidateID, query)
	if err != nil || len(history.Threads) != 1 || history.ThreadID != selected || len(history.Messages) != 2 || history.OnlineProfile == nil || !history.OnlineProfile.IsTemporary || history.OnlineProfile.Mode != "ai" || history.OnlineProfile.CandidateName != in.Request.Profile.CandidateName || history.OnlineProfile.EarliestSchoolYear == nil || *history.OnlineProfile.EarliestSchoolYear != *in.Request.Profile.EarliestSchoolYear || len(history.InfoRequests) != 2 || history.Threads[0].OnlineProfile == nil {
		t.Fatal("在线临时档历史或摘要缺失", history, err)
	}
	seenAI := false
	for _, m := range history.Messages {
		if m.ID == "online-answer" {
			seenAI = m.SenderType == "ai" && m.Text == "平台资料索要：phone"
		}
	}
	if !seenAI {
		t.Fatal("真实AI消息归因丢失或普通聊天正文被技术翻译")
	}
	bytes, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"source_key", "profile_key", "actor_hash", "machine_id", "raw_text", "screenshots", "synthetic-machine", "synthetic-source"} {
		if strings.Contains(string(bytes), secret) {
			t.Fatal("历史DTO暴露内部来源或图像", secret)
		}
	}
	if _, err = f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=payload||'{"Phone":"13900001111","Wechat":"candidate_wechat"}'::jsonb WHERE tenant_id=$1 AND user_id=$2 AND kind='candidates' AND id='online-local'`, f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
	history, err = f.store.candidateHistory(ctx, scope, result.CandidateID, query)
	if err != nil || history.Contacts.Phone != "13900001111" || history.Contacts.Wechat != "candidate_wechat" || history.OnlineProfile == nil || history.OnlineProfile.IsTemporary {
		t.Fatal("当前联系方式未展示或临时状态未更新", history, err)
	}

	// 同成员的另一个会话共用候选人时，只展示自己的进度，不冒充也读过当前在线来源。
	f.entity(t, "conversations", "second-thread", map[string]any{"candidate_id": "online-local", "position_id": f.pos, "platform_id": "boss", "platform_account_id": in.Request.PlatformAccountID, "info_requests": map[string]any{"wechat": map[string]string{"status": "unavailable"}}})
	f.entity(t, "messages", "second-message", map[string]any{"conversation_id": "second-thread", "direction": "candidate", "text_content": "另一个会话自己的原话"})
	other, err := f.store.candidateHistory(ctx, scope, result.CandidateID, url.Values{"thread_id": {f.user + ":second-thread"}})
	if err != nil || other.OnlineProfile != nil || len(other.InfoRequests) != 1 || other.InfoRequests[0].Kind != "wechat" || len(other.Messages) != 1 || other.Messages[0].ID != "second-message" {
		t.Fatal("不同会话共享了在线快照或聊天进度", other, err)
	}

	// 另一个成员即使复用相同本机候选人号、线程号与canonical，也不能被普通成员读取。
	var peer string
	if err = f.db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'history-peer@example.test') RETURNING id", f.tenant).Scan(&peer); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", f.tenant, peer); err != nil {
		t.Fatal(err)
	}
	peerFixture := f
	peerFixture.user = peer
	peerFixture.entity(t, "candidates", "online-local", map[string]any{"Phone": "15500002222", "online_profile": map[string]any{"candidate_id": result.CandidateID, "profile_key": result.ProfileKey}})
	peerFixture.entity(t, "conversations", "online-thread", map[string]any{"candidate_id": "online-local", "position_id": f.pos, "platform_id": "boss", "platform_account_id": in.Request.PlatformAccountID})
	peerFixture.entity(t, "messages", "peer-secret", map[string]any{"conversation_id": "online-thread", "direction": "candidate", "text_content": "其他成员私有内容"})
	history, err = f.store.candidateHistory(ctx, scope, result.CandidateID, query)
	if err != nil || len(history.Threads) != 2 || history.Contacts.Phone != "13900001111" || history.Contacts.Phone == "15500002222" {
		t.Fatal("成员范围被在线精确关联扩大", history, err)
	}
	if _, err = f.store.candidateHistory(ctx, scope, result.CandidateID, url.Values{"thread_id": {peer + ":online-thread"}}); !errors.Is(err, ErrNotFound) {
		t.Fatal("其他成员会话编号可被直接选择", err)
	}
	peerScope := scope
	peerScope.UserEmail = "history-peer@example.test"
	peerHistory, err := f.store.candidateHistory(ctx, peerScope, result.CandidateID, url.Values{"thread_id": {peer + ":online-thread"}})
	if err != nil || peerHistory.OnlineProfile != nil || peerHistory.Contacts.Phone != "15500002222" || len(peerHistory.Messages) != 1 || peerHistory.Messages[0].ID != "peer-secret" {
		t.Fatal("同名会话借用了原成员的在线证明", peerHistory, err)
	}
	foreignScope := scope
	if err = f.db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&foreignScope.TenantID); err != nil {
		t.Fatal(err)
	}
	foreign, err := f.store.candidateHistory(ctx, foreignScope, result.CandidateID, nil)
	if err != nil || len(foreign.Threads) != 0 || foreign.OnlineProfile != nil || foreign.Contacts.Phone != "" {
		t.Fatal("跨团队读取到历史或联系方式", foreign, err)
	}
}

// TestCandidateReplyHistoryNoAssociationHasEmptySummary 验证没有自动回复关联的简历保留稳定空摘要，不猜联系人或进度。
func TestCandidateReplyHistoryNoAssociationHasEmptySummary(t *testing.T) {
	f := newEditableFactFixture(t)
	var candidate string
	if err := f.db.QueryRow("INSERT INTO candidate_profiles(tenant_id,candidate_name,phone) VALUES($1,'独立手工资料','18800001111') RETURNING id", f.tenant).Scan(&candidate); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.candidateHistory(context.Background(), CandidateLibraryScope{TenantID: f.tenant, UserEmail: "fact-hr@example.test"}, candidate, nil)
	if err != nil || len(got.Threads) != 0 || len(got.InfoRequests) != 0 || got.OnlineProfile != nil || got.Contacts.Phone != "" || got.Contacts.Wechat != "" {
		t.Fatal("没有已授权会话却拼出了自动回复摘要", got, err)
	}
}
