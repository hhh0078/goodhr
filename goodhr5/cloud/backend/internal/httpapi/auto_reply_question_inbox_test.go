// 本文件以独立 PostgreSQL 验证待确认数量、本人岗位范围、来源归属和翻页一致性。
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestQuestionInboxScopesAndPagination 验证关闭岗位、同题多来源、跨成员及跨团队都保持正确数量和原话范围。
func TestQuestionInboxScopesAndPagination(t *testing.T) {
	db := autoReplyStateTestDB(t)
	if _, err := db.Exec("ALTER TABLE positions ADD COLUMN platform_id text NOT NULL DEFAULT 'liepin'"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	one := func(query string, args ...any) string {
		t.Helper()
		var id string
		if err := db.QueryRow(query, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	tenant := one("INSERT INTO tenants DEFAULT VALUES RETURNING id")
	foreignTenant := one("INSERT INTO tenants DEFAULT VALUES RETURNING id")
	owner := one("INSERT INTO users(tenant_id,email) VALUES($1,'inbox-owner@example.test') RETURNING id", tenant)
	member := one("INSERT INTO users(tenant_id,email) VALUES($1,'inbox-member@example.test') RETURNING id", tenant)
	foreign := one("INSERT INTO users(tenant_id,email) VALUES($1,'inbox-foreign@example.test') RETURNING id", foreignTenant)
	position := one("INSERT INTO positions(user_id,name) VALUES($1,'开放岗位') RETURNING id", owner)
	closed := one("INSERT INTO positions(user_id,name,is_open) VALUES($1,'关闭岗位',false) RETURNING id", owner)
	other := one("INSERT INTO positions(user_id,name) VALUES($1,'其他成员岗位') RETURNING id", member)
	foreignPosition := one("INSERT INTO positions(user_id,name) VALUES($1,'其他团队岗位') RETURNING id", foreign)
	question := func(ten, pos, status, title string) string {
		t.Helper()
		return one(`INSERT INTO auto_reply_cloud_questions(tenant_id,position_id,dedupe_key,question,status,updated_at) VALUES($1,$2,$3,$4,$5,'2026-11-09T01:00:00Z') RETURNING id`, ten, pos, replyImportHash(title+status), title, status)
	}
	for i := 0; i < 23; i++ {
		question(tenant, position, "pending", fmt.Sprintf("待确认%d", i))
	}
	closedQuestion := question(tenant, closed, "failed", "归档失败可以重试")
	processing := question(tenant, position, "processing", "正在整理")
	if _, err := db.Exec("UPDATE auto_reply_cloud_questions SET updated_at=now(),answer='已经填写的答复' WHERE id=$1", processing); err != nil {
		t.Fatal(err)
	}
	question(tenant, position, "resolved", "已解决")
	question(tenant, position, "pending", "")
	question(tenant, other, "pending", "同团队他人隐私")
	question(foreignTenant, foreignPosition, "pending", "其他团队隐私")
	if _, err := db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", tenant, owner); err != nil {
		t.Fatal(err)
	}
	insertEntity := func(kind, id string, data map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(data)
		if _, err := db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,$3,$4,$5,1,1,'2026-11-09T01:00:00Z','{}')`, tenant, owner, kind, id, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	insertEntity("conversations", "source-thread", map[string]any{"candidate_name": "来源候选人", "position_id": closed})
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("message-%d", i)
		insertEntity("messages", id, map[string]any{"conversation_id": "source-thread", "text_content": fmt.Sprintf("真实原话%d", i)})
		if _, err := db.Exec("INSERT INTO auto_reply_cloud_question_sources(question_id,tenant_id,user_id,message_id) VALUES($1,$2,$3,$4)", closedQuestion, tenant, owner, id); err != nil {
			t.Fatal(err)
		}
	}
	s := NewPostgresAutoReplyStore(db)
	summary, err := s.questionInboxSummary(ctx, tenant, "inbox-owner@example.test")
	if err != nil || summary.PendingCount != 24 || len(summary.Positions) != 2 {
		t.Fatalf("汇总错误：%+v %v", summary, err)
	}
	for _, p := range summary.Positions {
		if p.PositionID == closed && (p.IsOpen || p.PendingCount != 1) {
			t.Fatalf("关闭岗位丢失：%+v", p)
		}
	}
	page, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", closed, "pending", "", 10)
	if err != nil || len(page.Questions) != 1 || len(page.Questions[0].SourceMessages) != 2 || page.PendingCount != 24 {
		t.Fatalf("同题来源或计数错误：%+v %v", page, err)
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", cursor, 10)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, q := range page.Questions {
			if seen[q.ID] || q.PositionID != position || len(q.SourceMessages) > 0 {
				t.Fatalf("重行或串原话：%+v", q)
			}
			seen[q.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
		if pages > 3 {
			t.Fatal("分页没有结束")
		}
	}
	if len(seen) != 23 || pages != 3 {
		t.Fatalf("分页漏行：%d条/%d页", len(seen), pages)
	}
	// 尚未看到的行移到第一页时，末行锚点不变也不能继续旧游标而漏掉该行。
	before, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	later, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", before.NextCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE auto_reply_cloud_questions SET updated_at='2026-11-09T02:00:00Z',version=version+1 WHERE id=$1", later.Questions[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", before.NextCursor, 10); err == nil {
		t.Fatal("未读行前移时旧游标会漏行")
	}
	first, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", closed, "pending", first.NextCursor, 10); err == nil {
		t.Fatal("其他岗位游标被接受")
	}
	if _, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "all", first.NextCursor, 10); err == nil {
		t.Fatal("其他筛选游标被接受")
	}
	for _, pos := range []string{other, foreignPosition} {
		page, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", pos, "all", "", 10)
		if err != nil || len(page.Questions) != 0 {
			t.Fatalf("其他成员或团队资料泄漏：%+v %v", page, err)
		}
	}
	all, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "all", "", 50)
	if err != nil || len(all.Questions) != 25 {
		t.Fatalf("全部问题缺少整理中或历史记录：%+v %v", all, err)
	}
	anchor := first.Questions[len(first.Questions)-1]
	if _, err = db.Exec("UPDATE auto_reply_cloud_questions SET status='resolved',updated_at=now() WHERE id=$1", anchor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", first.NextCursor, 10); err == nil {
		t.Fatal("已变更游标应要求重新载入，不能默默漏行")
	}
	// 模拟归档进程中断：原答复保留，超时任务重新出现在默认待办，且可用原版本重领。
	if _, err = db.Exec("UPDATE auto_reply_cloud_questions SET updated_at=now()-interval '3 minutes' WHERE id=$1", processing); err != nil {
		t.Fatal(err)
	}
	summary, err = s.questionInboxSummary(ctx, tenant, "inbox-owner@example.test")
	if err != nil || summary.PendingCount != 24 {
		t.Fatalf("超时归档丢失待办：%+v %v", summary, err)
	}
	recovered, err := s.questionInboxPage(ctx, tenant, "inbox-owner@example.test", position, "pending", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range recovered.Questions {
		if q.ID != processing {
			continue
		}
		found = true
		if q.Status != "failed" || q.Answer != "已经填写的答复" || q.Error == "" {
			t.Fatalf("未提供可恢复答复：%+v", q)
		}
		claimed, retry, e := s.beginQuestionAnswer(ctx, tenant, position, q.ID, q.Answer, q.Version)
		if e != nil || !retry || claimed.Status != "processing" {
			t.Fatalf("中断任务不能重试：%+v %v", claimed, e)
		}
	}
	if !found {
		t.Fatal("默认待办隐藏超时归档")
	}
}

// TestQuestionInboxCursorRejectsInvalidInput 验证无效、换岗位和换筛选的游标均不会进入数据库查询。
func TestQuestionInboxCursorRejectsInvalidInput(t *testing.T) {
	position := "11111111-1111-1111-1111-111111111111"
	good := replyInboxCursor{Position: position, View: "pending", ID: "22222222-2222-2222-2222-222222222222", UpdatedAt: time.Now().UTC(), Revision: strings.Repeat("a", 32)}
	encode := func(v replyInboxCursor) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	if _, err := decodeReplyInboxCursor(encode(good), position, "pending"); err != nil {
		t.Fatal(err)
	}
	badID := good
	badID.ID = "not-a-uuid"
	for _, raw := range []string{"?", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), encode(badID)} {
		if _, err := decodeReplyInboxCursor(raw, position, "pending"); err == nil {
			t.Fatal("接受无效游标")
		}
	}
}
