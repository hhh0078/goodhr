// 本文件验证自动回复通知按会话去重，以及岗位条件编号和配置并发保护。
package httpapi

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// applyAutoReplyTestMigrations 在本机隔离测试schema安装自动回复配置与摘要表。
func applyAutoReplyTestMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range []string{"0002_add_system_configs.sql", "0104_auto_reply_configuration.sql", "0105_auto_reply_result_summaries.sql", "0110_auto_reply_phrase_library.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAutoReplyNoticeSeparatesConversations 验证两位候选人的相同消息不会互相吞掉提醒，重试同一提醒仍使用同一键。
func TestAutoReplyNoticeSeparatesConversations(t *testing.T) {
	first := autoReplyNoticeRequest{ID: "conversation-a-notice", BasedOnMessageKey: "same-minute-message", ReasonKey: "needs-help", Reason: "需要人工协助"}
	second := first
	second.ID = "conversation-b-notice"
	if autoReplyNoticeDedupeKey("position", first) == autoReplyNoticeDedupeKey("position", second) {
		t.Fatal("不同会话的提醒被合并")
	}
	if autoReplyNoticeDedupeKey("position", first) == autoReplyNoticeDedupeKey("another-position", first) {
		t.Fatal("不同岗位的提醒被合并")
	}
	first.ID = ""
	if err := validateAutoReplyNotice(first); err == nil {
		t.Fatal("没有会话标识的提醒不应被接受")
	}
	first.ConversationID = "legacy-conversation"
	if err := validateAutoReplyNotice(first); err != nil {
		t.Fatal(err)
	}
}

// TestAutoReplyPostgresStableConditions 验证编辑和交换条件正文保留身份，旧版本和外部条件编号不能覆盖当前岗位。
func TestAutoReplyPostgresStableConditions(t *testing.T) {
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id UUID REFERENCES users(id);`); err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	var tenantID, positionID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`WITH owner AS (INSERT INTO users(email,tenant_id) VALUES('condition-owner@example.com',$1) RETURNING id) INSERT INTO positions(name,user_id) SELECT '测试岗位',id FROM owner RETURNING id::text`, tenantID).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresAutoReplyStore(db)
	ctx := context.Background()
	config, err := store.SavePositionAutoReplyConfig(ctx, "condition-owner@example.com", PositionAutoReplyConfig{TenantID: tenantID, PositionID: positionID, Conditions: []PositionReplyCondition{{Type: "required", Content: "偶尔出差", Enabled: true}, {Type: "confirm", Content: "到岗时间", Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	stale := config
	if _, err = store.SavePositionAutoReplyConfig(ctx, "condition-owner@example.com", config, 0); err == nil {
		t.Fatal("两个初始页面不能用显式版本0覆盖已保存配置")
	}
	stale.Conditions = append([]PositionReplyCondition(nil), config.Conditions...)
	id := config.Conditions[0].ID
	config.Conditions[0].Content, config.Conditions[1].Content = config.Conditions[1].Content, config.Conditions[0].Content
	config, err = store.SavePositionAutoReplyConfig(ctx, "condition-owner@example.com", config)
	if err != nil || config.Conditions[0].ID != id || config.Version != stale.Version+1 {
		t.Fatalf("条件身份未保留：%+v %v", config, err)
	}
	if !config.Conditions[0].CreatedAt.Equal(stale.Conditions[0].CreatedAt) || config.Conditions[0].CreatedByUserID != stale.Conditions[0].CreatedByUserID {
		t.Fatal("编辑条件不应重置创建记录")
	}
	if _, err = store.SavePositionAutoReplyConfig(ctx, "condition-owner@example.com", stale); err == nil {
		t.Fatal("过期配置覆盖了新配置")
	}
	config.Conditions[0].ID = "00000000-0000-0000-0000-000000000001"
	if _, err = store.SavePositionAutoReplyConfig(ctx, "condition-owner@example.com", config); err == nil {
		t.Fatal("接受了不属于当前岗位的条件编号")
	}
	current, err := store.GetPositionAutoReplyConfig(ctx, tenantID, positionID)
	if err != nil || current.Conditions[0].ID != id {
		t.Fatalf("失败保存没有整体回滚：%+v %v", current, err)
	}
}

// TestAutoReplyNoticeRecovery 验证并发重试不会把发送中当成功，失败和崩溃遗留租约可恢复，已发送不会重发。
func TestAutoReplyNoticeRecovery(t *testing.T) {
	db := candidatePostgresTestDB(t)
	applyAutoReplyTestMigrations(t, db)
	var tenantID, positionID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO positions(name) VALUES('通知合成岗位') RETURNING id::text`).Scan(&positionID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresAutoReplyStore(db)
	claim := func() (string, string) {
		t.Helper()
		id, status, err := store.claimAutoReplyNotice(context.Background(), tenantID, positionID, "synthetic@example.invalid", "same-notice")
		if err != nil {
			t.Fatal(err)
		}
		return id, status
	}
	first, _ := claim()
	if first == "" {
		t.Fatal("新通知没有领取")
	}
	if id, status := claim(); id != "" || status != "pending" {
		t.Fatalf("并发重试错误：%s %s", id, status)
	}
	if _, err := db.Exec(`UPDATE auto_reply_notice_summaries SET status='failed' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if id, _ := claim(); id != first {
		t.Fatal("失败通知没有复用原记录恢复")
	}
	if _, err := db.Exec(`UPDATE auto_reply_notice_summaries SET updated_at=now()-interval '6 minutes' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if id, _ := claim(); id != first {
		t.Fatal("崩溃遗留通知没有恢复")
	}
	if _, err := db.Exec(`UPDATE auto_reply_notice_summaries SET status='sent' WHERE id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if id, status := claim(); id != "" || status != "sent" {
		t.Fatalf("已发送通知被重新领取：%s %s", id, status)
	}
}
