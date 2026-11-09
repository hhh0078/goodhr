// 本文件在隔离 PostgreSQL 中核验服务器权威状态、真实历史分页、原话去重、答复版本与简历删除。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// autoReplyStateTestDB 在单个独立测试模式应用云端自动回复的实际迁移。
func autoReplyStateTestDB(t *testing.T, stopBefore ...string) *sql.DB {
	t.Helper()
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec("ALTER TABLE users ADD COLUMN status text NOT NULL DEFAULT 'active'; ALTER TABLE positions ADD COLUMN user_id uuid REFERENCES users(id)"); err != nil {
		t.Fatal(err)
	}
	applyAutoReplyTestMigrations(t, db)
	for _, name := range []string{"0112_auto_reply_cloud_state.sql", "0113_auto_reply_import_receipts.sql", "0114_auto_reply_answer_receipts.sql", "0115_auto_reply_attachment_cleanup.sql", "0116_auto_reply_question_privacy.sql", "0117_auto_reply_authoritative_boundaries.sql", "0118_auto_reply_sealed_audits.sql", "0119_auto_reply_editable_facts.sql"} {
		if len(stopBefore) > 0 && stopBefore[0] == name {
			break
		}
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// TestAutoReplyCloudStateFlow 使用真实外键和事务，覆盖来源关联、修改归档、跨成员访问与删除代际。
func TestAutoReplyCloudStateFlow(t *testing.T) {
	db := autoReplyStateTestDB(t)
	var err error
	var tenant, uid, pos, company, candidate string
	one := func(out *string, q string, args ...any) {
		t.Helper()
		if err := db.QueryRow(q, args...).Scan(out); err != nil {
			t.Fatal(err)
		}
	}
	one(&tenant, "INSERT INTO tenants DEFAULT VALUES RETURNING id")
	one(&uid, "INSERT INTO users(tenant_id,email) VALUES($1,'hr@example.test') RETURNING id", tenant)
	one(&pos, "INSERT INTO positions(user_id,name) VALUES($1,'招聘专员') RETURNING id", uid)
	one(&company, "INSERT INTO tenant_company_profiles(tenant_id,name) VALUES($1,'甲公司') RETURNING id", tenant)
	one(&candidate, "INSERT INTO candidate_profiles(tenant_id,candidate_name) VALUES($1,'测试候选人') RETURNING id", tenant)
	if _, err = db.Exec("INSERT INTO position_auto_reply_configs(position_id,tenant_id,company_profile_id) VALUES($1,$2,$3)", pos, tenant, company); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := NewPostgresAutoReplyStore(db)
	base := t.TempDir()
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", base)
	bytes := "真实测试PDF字节"
	digest := replyImportHash(bytes)
	relative := replyImportHash("auto_reply:v1:"+replyImportHash(uid)) + "/attachments/" + digest + ".pdf"
	file := filepath.Join(base, replyImportHash(tenant+"\x00"+uid), filepath.FromSlash(relative))
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, []byte(bytes), 0600); err != nil {
		t.Fatal(err)
	}
	makeChange := func(kind, id string, seq int, fields map[string]any) AutoReplyStateChange {
		t.Helper()
		p, e := json.Marshal(fields)
		if e != nil {
			t.Fatal(e)
		}
		return AutoReplyStateChange{Kind: kind, ID: id, Sequence: int64(seq), Revision: 1, Payload: p, UpdatedAt: "2026-11-09T01:00:00Z", MessageOrder: float64(seq)}
	}
	changes := []AutoReplyStateChange{
		makeChange("candidates", "local-1", 1, map[string]any{"Name": "测试候选人"}),
		makeChange("cloud_links", "candidate:local-1", 1, map[string]any{"value": candidate}),
		makeChange("conversations", "thread-1", 1, map[string]any{"candidate_id": "local-1", "position_id": pos, "platform_id": "liepin", "candidate_name": "测试候选人", "platform_account_id": "hr-channel", "platform_thread_id": "plat-thread"}),
		makeChange("runs", "run", 1, map[string]any{"conversation_id": "thread-1", "started_at": "2026-11-09T01:00:00Z", "completed_at": "2026-11-09T01:00:01Z", "status": "done"}),
		makeChange("tools", "tool", 1, map[string]any{"ai_run_id": "run", "tool_name": "核对资料", "status": "done"}),
		makeChange("sends", "out", 1, map[string]any{"conversation_id": "thread-1", "status": "sent", "outbound_message_key": "k54", "content": "已收到"}),
		makeChange("attachments", "att", 1, map[string]any{"conversation_id": "thread-1", "relative_path": relative, "sha256": digest, "size_bytes": len(bytes)}),
		makeChange("confirmations", "goal", 1, map[string]any{"conversation_id": "thread-1", "source_ref": "k1", "evidence_text": "排班", "content": "工作时间", "status": "pending"}),
	}
	for i := 1; i <= 54; i++ {
		msg, side := "排班怎么安排？每周休几天？", "candidate"
		if i == 54 {
			msg, side = "已收到", "self"
		}
		changes = append(changes, makeChange("messages", fmt.Sprintf("m%d", i), i, map[string]any{"conversation_id": "thread-1", "direction": side, "text_content": msg, "platform_message_id": fmt.Sprintf("k%d", i)}))
	}
	v, err := s.CommitAutoReplyState(ctx, tenant, uid, 0, changes)
	if err != nil || v != 1 {
		t.Fatalf("首次提交: %v %d", err, v)
	}
	if _, err = s.CommitAutoReplyState(ctx, tenant, uid, 0, changes); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("过期版本被接受", err)
	}
	state, err := s.LoadAutoReplyState(ctx, tenant, uid)
	if err != nil || len(state.Entities) != len(changes) {
		t.Fatal("PG恢复不完整", err, len(state.Entities))
	}
	state, err = s.LoadAutoReplyState(ctx, tenant, uid, v)
	if err != nil || state.Entities != nil {
		t.Fatal("已知版本应跳过正文", err)
	}
	scope := CandidateLibraryScope{TenantID: tenant, UserEmail: "hr@example.test"}
	h, err := s.candidateHistory(ctx, scope, candidate, nil)
	if err != nil || len(h.Threads) != 1 || len(h.Messages) != 50 || h.Messages[49].SenderType != "ai" || h.NextMessageCursor != "m5" || len(h.Goals) != 1 || len(h.Goals[0].EvidenceIDs) != 1 || len(h.Events) != 4 {
		t.Fatalf("历史、来源或分页异常 %#v %v", h, err)
	}
	h, err = s.candidateHistory(ctx, scope, candidate, url.Values{"message_cursor": {h.NextMessageCursor}})
	if err != nil || len(h.Messages) != 4 || h.Messages[0].ID != "m1" || h.NextMessageCursor != "" {
		t.Fatal("第二页重复或遗漏", err)
	}
	scope.UserEmail = "other@example.test"
	h, err = s.candidateHistory(ctx, scope, candidate, nil)
	if err != nil || len(h.Threads) > 0 {
		t.Fatal("跨成员泄漏", err)
	}
	q, err := s.recordQuestion(ctx, tenant, uid, pos, "", "排班怎么安排", "thread-1", "k1")
	if err != nil {
		t.Fatal(err)
	}
	q2, err := s.recordQuestion(ctx, tenant, uid, pos, q.ID, "排班怎么安排", "thread-1", "k2")
	if err != nil || q.ID != q2.ID {
		t.Fatal(err)
	}
	_, err = s.recordQuestion(ctx, tenant, uid, pos, q.ID, "排班怎么安排", "thread-1", "k2")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.recordQuestion(ctx, tenant, uid, pos, "", "每周休几天", "thread-1", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.recordQuestion(ctx, tenant, uid, pos, "", "无关问题", "thread-1", "k1"); err == nil {
		t.Fatal("虚构原话被登记")
	}
	qs, err := s.readQuestions(ctx, tenant, pos, false)
	if err != nil || len(qs) != 2 {
		t.Fatal("同一来源多题未保留", err)
	}
	for _, item := range qs {
		if item.ID == q.ID && len(item.SourceMessages) != 2 {
			t.Fatal("合并来源遗漏或重复")
		}
	}
	q, claimed, err := s.beginQuestionAnswer(ctx, tenant, pos, q.ID, "公司统一双休", q.Version)
	if err != nil || !claimed {
		t.Fatal(err)
	}
	old := q.Version
	q, err = s.finishQuestionAnswer(ctx, tenant, pos, q.ID, q.Version, company, "company", "公司统一双休", "")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Rollback()
	if _, err = probe.Exec("DELETE FROM auto_reply_cloud_question_sources WHERE question_id=$1 AND message_id='m2'", q.ID); err != nil {
		t.Fatal(err)
	}
	var sharedQuestion, sharedAnswer string
	if err = probe.QueryRow("SELECT question,answer FROM auto_reply_cloud_questions WHERE id=$1", q.ID).Scan(&sharedQuestion, &sharedAnswer); err != nil || sharedQuestion == "" || sharedAnswer != "公司统一双休" {
		t.Fatal("剩余来源仍引用的问题和HR答复不应清理", err)
	}
	if err = probe.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE position_auto_reply_configs SET enabled=true WHERE position_id=$1", pos); err != nil {
		t.Fatal(err)
	}
	delivery, err := s.pendingAnswers(ctx, tenant, uid, "machine1", "liepin", "hr-channel")
	if err != nil || len(delivery) != 1 || delivery[0].SourceKey != "k2" || delivery[0].Answer != "公司统一双休" {
		t.Fatal("同会话两条原话应仅有一次补答", delivery, err)
	}
	first := delivery[0]
	for i := 0; i < 2; i++ {
		yes, e := s.claimAnswer(ctx, tenant, uid, "machine1", first.ID, first.ConversationID)
		if e != nil || !yes {
			t.Fatal("同设备恢复补答", e)
		}
	}
	if yes, e := s.claimAnswer(ctx, tenant, uid, "machine2", first.ID, first.ConversationID); e != nil || yes {
		t.Fatal("其他设备抢到租约", e)
	}
	if e := s.finishAnswer(ctx, tenant, uid, "machine1", first.ID, first.ConversationID, "sent", "k54"); e != nil {
		t.Fatal(e)
	}
	if e := s.finishAnswer(ctx, tenant, uid, "machine1", first.ID, first.ConversationID, "sent", "k54"); e != nil {
		t.Fatal(e)
	}
	if e := s.finishAnswer(ctx, tenant, uid, "machine1", first.ID, first.ConversationID, "manual", ""); !errors.Is(e, errReplyStateConflict) {
		t.Fatal("不同终态覆盖已发送收据", e)
	}
	if again, e := s.pendingAnswers(ctx, tenant, uid, "machine1", "liepin", "hr-channel"); e != nil || len(again) != 0 {
		t.Fatal("已发送补答重复进入队列", e)
	}

	// 只有未登记过的真实提问来源重开后，才允许再次补充；已完成条目不能由旧页面改写。
	q, err = s.recordQuestion(ctx, tenant, uid, pos, q.ID, "排班怎么安排", "thread-1", "k3")
	if err != nil || q.Status != "pending" {
		t.Fatal("新问题来源没有重开原题", err)
	}
	q, _, err = s.beginQuestionAnswer(ctx, tenant, pos, q.ID, "本岗周日休息", q.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.finishQuestionAnswer(ctx, tenant, pos, q.ID, old, company, "both", "旧答案", ""); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("过期答复没有被拒", err)
	}
	q, err = s.finishQuestionAnswer(ctx, tenant, pos, q.ID, q.Version, company, "position", "本岗周日休息", "")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetPositionAutoReplyConfig(ctx, tenant, pos)
	if err != nil || !strings.Contains(cfg.PositionDescription, "本岗周日休息") || strings.Contains(cfg.PositionDescription, "公司统一双休") {
		t.Fatal("岗位没有保存可编辑的新事实", err)
	}
	co, err := s.GetCompanyProfile(ctx, tenant, company)
	if err != nil || co.ExtraInfo != "" {
		t.Fatal("改归属残留旧公司事实", err)
	}
	archiveDigest := normalizeAutoReplyDedupeKey("archive")
	imported, err := s.ImportAutoReplyState(ctx, tenant, uid, archiveDigest, changes, 1)
	if err != nil || imported.Accepted != 0 || imported.Reconciled != len(changes) || imported.Suppressed != 0 {
		t.Fatalf("兼容已有状态失败: %+v %v", imported, err)
	}
	replay, err := s.ImportAutoReplyState(ctx, tenant, uid, archiveDigest, changes, 1)
	if err != nil || replay != imported {
		t.Fatal("回执或计数不幂等", err)
	}

	if _, err = db.Exec("DELETE FROM candidate_profiles WHERE id=$1", candidate); err != nil {
		t.Fatal(err)
	}
	state, err = s.LoadAutoReplyState(ctx, tenant, uid)
	if err != nil || state.Version != 2 || len(state.Entities) != 1 || state.Entities[0].Kind != "purge_files" {
		t.Fatalf("删除不完整:%+v %v", state, err)
	}
	if _, err = s.CommitAutoReplyState(ctx, tenant, uid, state.Version, changes[:1]); !errors.Is(err, errCandidateDeleted) {
		t.Fatal("旧任务复活", err)
	}
	unowned := makeChange("conversations", "bad", 8, map[string]any{"position_id": "不存在的前团队岗位"})
	if _, e := s.ImportAutoReplyState(ctx, tenant, uid, normalizeAutoReplyDedupeKey("unowned"), []AutoReplyStateChange{unowned}, 0); !errors.Is(e, ErrAutoReplyForbidden) {
		t.Fatal("未知归属的老数据不应进新团队", e)
	}

	afterDelete, err := s.ImportAutoReplyState(ctx, tenant, uid, normalizeAutoReplyDedupeKey("later"), changes, 1)
	if err != nil || afterDelete.Accepted != 0 || afterDelete.Suppressed != len(changes) {
		t.Fatalf("旧设备迁移复活已删来源: %+v %v", afterDelete, err)
	}
	newRows := []AutoReplyStateChange{makeChange("identities", "old-device-id", 1, map[string]any{"platform_id": "old", "platform_candidate_id": "erased-source", "candidate_id": "old-local"}), makeChange("conversations", "old-thread", 1, map[string]any{"platform_identity_id": "old-device-id", "candidate_id": "old-local"}), makeChange("candidates", "old-local", 1, map[string]any{}), makeChange("messages", "old-message", 1, map[string]any{"conversation_id": "old-thread"}), makeChange("messages", "unrelated", 2, map[string]any{"conversation_id": "untouched-thread"})}
	if _, err = db.Exec("INSERT INTO candidate_deletion_keys(tenant_id,kind,identity_hash) VALUES($1,'source',$2)", tenant, purgeSourceHash("old", "erased-source")); err != nil {
		t.Fatal(err)
	}
	mixed, err := s.ImportAutoReplyState(ctx, tenant, uid, normalizeAutoReplyDedupeKey("mixed"), newRows, 0)
	if err != nil || mixed.Accepted != 1 || mixed.Suppressed != 4 {
		t.Fatalf("未识别旧端来源与后代或误删其它记录: %+v %v", mixed, err)
	}

	qs, err = s.readQuestions(ctx, tenant, pos, false)
	if err != nil || len(qs) != 0 {
		t.Fatal("候选来源删除不完整", err, qs)
	}
	var original, answer, fact string
	if err = db.QueryRow("SELECT question,answer,fact FROM auto_reply_cloud_questions WHERE id=$1", q.ID).Scan(&original, &answer, &fact); err != nil || original != "" || answer != "" || fact != "本岗周日休息" {
		t.Fatal("最后来源删除后应只留下招聘事实", err, original, answer, fact)
	}
}
