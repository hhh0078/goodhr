// 本文件在独立PostgreSQL验证在线资料和原生资料动作中文展示，以及从同会话可信卡片派生的收到联系方式时间线分页。
package httpapi

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// TestResumeOfferTimelineDistinguishesAcceptance 验证同意附件与资料索要沿既有事件API分别展示，联系方式冲突标题也保留中文。
func TestResumeOfferTimelineDistinguishesAcceptance(t *testing.T) {
	f := newEditableFactFixture(t)
	const at = "2026-11-10T01:02:03Z"
	f.entity(t, "runtime_commands", "accept-offer", map[string]any{"conversation_id": "thread", "action": "request_info", "resume_offer_message_id": "offer", "status": "completed", "updated_at": at})
	f.entity(t, "sends", "accepted-file", map[string]any{"conversation_id": "thread", "message_key": "resume-offer:offer", "content": "接收候选人简历附件", "status": "sent", "updated_at": at})
	f.entity(t, "runtime_events", "accepted-event", map[string]any{"conversation_id": "thread", "title": "resume_offer_received", "status": "received", "occurred_at": at})
	f.entity(t, "runtime_events", "contact-conflict", map[string]any{"conversation_id": "thread", "title": "联系方式待核对", "status": "unknown", "occurred_at": at})
	tx, err := f.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var out candidateReplyHistory
	if err = readHistoryEvents(context.Background(), tx, f.tenant, f.user, "thread", "", &out); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"runtime_commands:accept-offer": "接收简历申请", "sends:accepted-file": "已接收附件", "runtime_events:accepted-event": "已接收附件", "runtime_events:contact-conflict": "联系方式待核对"}
	for _, event := range out.Events {
		if title, ok := want[event.ID]; ok {
			if event.Title != title {
				t.Fatal("同意动作或联系方式冲突标题错误", event)
			}
			delete(want, event.ID)
		}
	}
	if len(want) != 0 {
		t.Fatal("缺少已存接收事件", want)
	}
}

// TestCandidateNativeTimelineDerivesScopedEvidence 验证只有已收到且完整的同成员同会话卡片生成稳定事件，技术收据不误翻普通聊天正文。
func TestCandidateNativeTimelineDerivesScopedEvidence(t *testing.T) {
	f := newEditableFactFixture(t)
	const at = "2026-11-10T01:02:03Z"
	card := func(id, thread, kind, status, value string) {
		t.Helper()
		f.entity(t, "messages", id, map[string]any{"conversation_id": thread, "direction": "candidate", "message_type": "card", "platform_sent_at": at, "card_content": map[string]string{"info_type": kind, "info_status": status, kind: value}})
	}
	card("received-phone", "thread", "phone", "received", "13900001111")
	card("received-wechat", "thread", "wechat", "received", "candidate_wechat")
	card("masked-phone", "thread", "phone", "received", "139****1111")
	card("requested-phone", "thread", "phone", "requested", "13900001111")
	card("resume-card", "thread", "resume", "received", "附件")
	card("other-conversation", "another-thread", "phone", "received", "18800002222")
	var peer string
	if err := f.db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'native-history-peer@example.test') RETURNING id", f.tenant).Scan(&peer); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", f.tenant, peer); err != nil {
		t.Fatal(err)
	}
	peerFixture := f
	peerFixture.user = peer
	peerFixture.entity(t, "messages", "peer-card", map[string]any{"conversation_id": "thread", "direction": "candidate", "message_type": "card", "platform_sent_at": at, "card_content": map[string]string{"info_type": "phone", "info_status": "received", "phone": "15500002222"}})
	for _, action := range []string{"read_online_profile", "request_info"} {
		f.entity(t, "runtime_commands", action, map[string]any{"conversation_id": "thread", "action": action, "status": "completed", "updated_at": at})
	}
	for _, stage := range []string{"online_profile", "request_info_resume", "request_info_phone", "request_info_wechat"} {
		f.entity(t, "runtime_events", stage, map[string]any{"conversation_id": "thread", "title": stage, "status": "received", "occurred_at": at})
	}
	for _, kind := range []string{"resume", "phone", "wechat"} {
		f.entity(t, "sends", "technical-"+kind, map[string]any{"conversation_id": "thread", "message_key": "info-request:" + kind, "content": "平台资料索要：" + kind, "status": "unknown", "updated_at": at})
	}
	f.entity(t, "sends", "ordinary-content", map[string]any{"conversation_id": "thread", "message_key": "ordinary", "content": "平台资料索要：phone", "status": "sent", "updated_at": at})
	f.entity(t, "sends", "wrong-type", map[string]any{"conversation_id": "thread", "message_key": "info-request:wechat", "content": "平台资料索要：phone", "status": "sent", "updated_at": at})
	tx, err := f.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	read := func(cursor string) candidateReplyHistory {
		t.Helper()
		var out candidateReplyHistory
		if err := readHistoryEvents(context.Background(), tx, f.tenant, f.user, "thread", cursor, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := read("")
	if len(first.Events) != 13 {
		t.Fatal("事件数量错误，可能遗漏真实证据或凭空新增", len(first.Events), first.Events)
	}
	want := map[string]string{"runtime_commands:read_online_profile": "读取在线简历", "runtime_commands:request_info": "索要候选人资料", "runtime_events:online_profile": "整理在线简历", "runtime_events:request_info_resume": "索要简历", "runtime_events:request_info_phone": "索要手机号", "runtime_events:request_info_wechat": "索要微信", "contact-received:phone:received-phone": "已收到手机号", "contact-received:wechat:received-wechat": "已收到微信"}
	for _, event := range first.Events {
		if title, exists := want[event.ID]; exists {
			if event.Title != title {
				t.Fatal("新增动作或阶段未使用中文", event)
			}
			delete(want, event.ID)
		}
		if event.StatusLabel == "" || event.StatusLabel == event.Status {
			t.Fatal("技术状态未中文展示", event)
		}
		if strings.HasPrefix(event.ID, "contact-received:") && (event.DurationMS != nil || event.OccurredAt != at || event.Group != "action" || event.Status != "received") {
			t.Fatal("收到事件伪造时间或耗时", event)
		}
		if strings.HasPrefix(event.ID, "sends:technical-") && (strings.Contains(event.Description, "平台资料索要：") || event.StatusLabel != "结果待核实") {
			t.Fatal("技术请求被当成实际聊天正文或状态误译", event)
		}
		if (event.ID == "sends:ordinary-content" || event.ID == "sends:wrong-type") && event.Description != "平台资料索要：phone" {
			t.Fatal("普通正文被泛化替换", event)
		}
	}
	if len(want) != 0 {
		t.Fatal("缺少新增动作或收到事件", want)
	}
	if repeated := read(""); len(repeated.Events) != len(first.Events) {
		t.Fatal("重复查看产生了新事件")
	} else {
		for i := range repeated.Events {
			if repeated.Events[i].ID != first.Events[i].ID {
				t.Fatal("派生事件编号不稳定")
			}
		}
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		card(fmt.Sprintf("page-card-%02d", i), "thread", "wechat", "received", "candidate_wechat")
	}
	tx, err = f.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	page := read("")
	if len(page.Events) != 50 || page.NextEventCursor == "" {
		t.Fatal("新增收到事件没有参与标准分页", len(page.Events), page.NextEventCursor)
	}
	next := read(page.NextEventCursor)
	if len(next.Events) != 16 || next.NextEventCursor != "" {
		t.Fatal("收到事件跨页漏行", len(next.Events), next.NextEventCursor)
	}
	seen := map[string]bool{}
	for _, e := range append(page.Events, next.Events...) {
		if seen[e.ID] {
			t.Fatal("派生事件跨页重复", e.ID)
		}
		seen[e.ID] = true
	}
}
