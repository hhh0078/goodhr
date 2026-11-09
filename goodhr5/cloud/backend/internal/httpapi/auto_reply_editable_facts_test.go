// 本文件在隔离PostgreSQL验证HR补充真实落库、人工编辑优先、旧答复失效及一次性迁移。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type editableFactFixture struct {
	db                         *sql.DB
	store                      *PostgresAutoReplyStore
	tenant, user, pos, company string
	question                   replyQuestion
}

// newEditableFactFixture 建立真实公司、岗位和候选来信关联，所有写入都在独立测试模式内。
func newEditableFactFixture(t *testing.T, stopBefore ...string) editableFactFixture {
	t.Helper()
	f := editableFactFixture{db: autoReplyStateTestDB(t, stopBefore...)}
	f.store = NewPostgresAutoReplyStore(f.db)
	if err := f.db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'fact-hr@example.test') RETURNING id", f.tenant).Scan(&f.user); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("INSERT INTO positions(user_id,name) VALUES($1,'开发工程师') RETURNING id", f.user).Scan(&f.pos); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("INSERT INTO tenant_company_profiles(tenant_id,name,extra_info) VALUES($1,'验收公司','原有公司介绍') RETURNING id", f.tenant).Scan(&f.company); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO position_auto_reply_configs(position_id,tenant_id,company_profile_id,position_description,enabled) VALUES($1,$2,$3,'原有岗位职责',true)", f.pos, f.tenant, f.company); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
	f.entity(t, "conversations", "thread", map[string]string{"position_id": f.pos, "platform_id": "liepin", "platform_account_id": "account", "platform_thread_id": "platform-thread", "candidate_name": "测试候选人"})
	f.entity(t, "messages", "m1", map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "k1"})
	q, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", "办公地址在哪里", "thread", "k1")
	if err != nil {
		t.Fatal(err)
	}
	f.question = q
	return f
}

// entity 保存带真实外键的最小来源实体，避免测试绕过问题与来信的归属检查。
func (f editableFactFixture) entity(t *testing.T, kind, id string, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,$3,$4,$5::jsonb,1,1,'2026-11-09T01:00:00Z','{}')`, f.tenant, f.user, kind, id, string(raw))
	if err != nil {
		t.Fatal(err)
	}
}

// archive 模拟模型已返回受证据支持的事实，再走实际领取、快照与原子归档流程。
func (f *editableFactFixture) archive(t *testing.T, target, fact string) {
	t.Helper()
	ctx := context.Background()
	q, claimed, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, fact, f.question.Version)
	if err != nil || !claimed {
		t.Fatal("领取失败", err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	q, err = f.store.finishQuestionAnswer(ctx, f.tenant, f.pos, q.ID, q.Version, f.company, target, fact, "", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	f.question = q
}

// TestEditableFactsManualChangeWins 验证修改或删除正文后旧答案不重新进入模型、待发队列或共享资料。
func TestEditableFactsManualChangeWins(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	old, _ := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	f.archive(t, "company", "公司办公地址为成都A座。")
	co, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || !strings.Contains(co.ExtraInfo, "原有公司介绍") || !strings.Contains(co.ExtraInfo, "公司办公地址为成都A座。") {
		t.Fatal("事实没有进入真实公司字段", err)
	}
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", old); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("旧公司草稿覆盖了归档结果", err)
	}
	pending, err := f.store.pendingAnswers(ctx, f.tenant, f.user, "device", "liepin", "account")
	if err != nil || len(pending) != 1 {
		t.Fatal("原补答缺失", err)
	}
	co.ExtraInfo = "原有公司介绍\n办公地址暂时需要核实"
	saved, err := f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", co)
	if err != nil {
		t.Fatal(err)
	}
	_, claimed, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, f.question.Answer, f.question.Version)
	if err != nil || claimed {
		t.Fatal("重复答复不应重归档", err)
	}
	co, err = f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || co.ExtraInfo != saved.ExtraInfo {
		t.Fatal("旧事实复活", err)
	}
	index, err := f.store.readQuestions(ctx, f.tenant, f.pos, true)
	if err != nil || len(index) != 1 || index[0].Answer != "" {
		t.Fatal("模型索引仍暴露历史答复", err)
	}
	pending, err = f.store.pendingAnswers(ctx, f.tenant, f.user, "device", "liepin", "account")
	if err != nil || len(pending) != 0 {
		t.Fatal("人工已改资料仍派发旧补答", err)
	}
	f.entity(t, "messages", "m2", map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "k2"})
	q, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, f.question.ID, "办公地址在哪里", "thread", "k2")
	if err != nil || q.ID != f.question.ID || q.Status != "pending" || q.Version != f.question.Version+1 {
		t.Fatal("新的未知来信没有重开原题", q, err)
	}
	again, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, f.question.ID, "办公地址在哪里", "thread", "k2")
	if err != nil || again.Version != q.Version {
		t.Fatal("同来源重交重复重开", err)
	}
	f.question = q
	f.archive(t, "company", "公司办公地址为成都B座。")
	var history int
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_question_answer_history WHERE question_id=$1", q.ID).Scan(&history); err != nil || history != 2 {
		t.Fatal("历史答复未保留", history, err)
	}
	if _, err = f.db.Exec("DELETE FROM auto_reply_cloud_question_sources WHERE question_id=$1", q.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_question_answer_history WHERE question_id=$1 AND answer<>''", q.ID).Scan(&history); err != nil || history != 0 {
		t.Fatal("删除最后候选来源后答复原文仍遗留", history, err)
	}
}

// TestEditableFactsRejectOldModelSnapshot 验证模型分析期间发生的人工编辑会使旧结果失效，不能丢掉新正文。
func TestEditableFactsRejectOldModelSnapshot(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	q, _, err := f.store.beginQuestionAnswer(ctx, f.tenant, f.pos, f.question.ID, "公司双休", f.question.Version)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.store.questionFactContext(ctx, f.tenant, f.pos)
	if err != nil {
		t.Fatal(err)
	}
	co, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil {
		t.Fatal(err)
	}
	co.ExtraInfo = "人工刚改的新正文"
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", co); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.finishQuestionAnswer(ctx, f.tenant, f.pos, q.ID, q.Version, f.company, "company", "公司双休", "", snapshot); !errors.Is(err, errReplyStateConflict) {
		t.Fatal("旧模型快照没有被拒绝", err)
	}
	co, err = f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || co.ExtraInfo != "人工刚改的新正文" {
		t.Fatal("人工新正文被覆盖", err)
	}
}

// TestEditableFactsManualNegationIsNotManagedFragment 验证人工否定或改写的句内子串不会被当成旧归档片段替换或继续发送。
func TestEditableFactsManualNegationIsNotManagedFragment(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	f.archive(t, "company", "双休")
	co, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil {
		t.Fatal(err)
	}
	co.ExtraInfo = "原有公司介绍\n目前不保证双休"
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", co); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.pendingAnswers(ctx, f.tenant, f.user, "device", "liepin", "account")
	if err != nil || len(rows) != 0 {
		t.Fatal("人工否定仍领取了旧双休答复", rows, err)
	}
	f.entity(t, "messages", "m2", map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "k2"})
	f.question, err = f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, f.question.ID, "办公地址在哪里", "thread", "k2")
	if err != nil || f.question.Status != "pending" {
		t.Fatal("新来源未重开待补充问题", err)
	}
	f.archive(t, "company", "每周日休息")
	co, err = f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || !strings.Contains(co.ExtraInfo, "目前不保证双休") || !strings.Contains(co.ExtraInfo, "每周日休息") || strings.Contains(co.ExtraInfo, "不保证每周日休息") {
		t.Fatal("精准合并破坏了人工句子", co.ExtraInfo, err)
	}
}

// TestEditableFactsLegacyMigration 验证旧事实真实迁入可编辑资料，人工修改后不再从旧记录自动追加。
func TestEditableFactsLegacyMigration(t *testing.T) {
	f := newEditableFactFixture(t, "0119_auto_reply_editable_facts.sql")
	ctx := context.Background()
	_, err := f.db.Exec("UPDATE auto_reply_cloud_questions SET status='resolved',version=2,answer='原HR双休答复',fact='公司双休',publish_target='both',company_profile_id=$2 WHERE id=$1", f.question.ID, f.company)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0119_auto_reply_editable_facts.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	co, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || strings.Count(co.ExtraInfo, "公司双休") != 1 {
		t.Fatal("旧公司补充未迁入", err)
	}
	cfg, err := f.store.GetPositionAutoReplyConfig(ctx, f.tenant, f.pos)
	if err != nil || strings.Count(cfg.PositionDescription, "公司双休") != 1 {
		t.Fatal("旧岗位补充未迁入", err)
	}
	co.ExtraInfo = "人工更新后的公司政策"
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", co); err != nil {
		t.Fatal(err)
	}
	co, err = f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || co.ExtraInfo != "人工更新后的公司政策" {
		t.Fatal("已迁移旧事实复活", err)
	}
}

// TestEditableFactsCandidatePurgePreservesSharedHistory 验证真实永久删除先保留共享原话，最后来源删除时清空历版答复原文。
func TestEditableFactsCandidatePurgePreservesSharedHistory(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx := context.Background()
	for _, name := range []string{"0103_position_screening_records.sql", "0090_recruitment_workflows.sql", "0091_recruitment_mail.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(string(raw)); err != nil {
			t.Fatal(name, err)
		}
	}
	var first, second string
	var firstVersion, secondVersion int64
	for index, item := range []struct {
		id      *string
		version *int64
	}{{&first, &firstVersion}, {&second, &secondVersion}} {
		if err := f.db.QueryRow("INSERT INTO candidate_profiles(tenant_id,candidate_name,source_platform_id,source_platform_candidate_id) VALUES($1,'测试候选人','liepin',$2) RETURNING id,row_version", f.tenant, "source-"+string(rune('a'+index))).Scan(item.id, item.version); err != nil {
			t.Fatal(err)
		}
		local := "local-" + *item.id
		f.entity(t, "candidates", local, map[string]string{"name": "测试候选人"})
		f.entity(t, "cloud_links", "candidate:"+local, map[string]string{"value": *item.id})
		if _, err := f.db.Exec("UPDATE auto_reply_cloud_entities SET parents=ARRAY[$4] WHERE tenant_id=$1 AND user_id=$2 AND kind='cloud_links' AND id=$3", f.tenant, f.user, "candidate:"+local, "candidates:"+local); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET payload=payload||jsonb_build_object('candidate_id',$3::text),parents=ARRAY['candidates:'||$3::text] WHERE tenant_id=$1 AND user_id=$2 AND kind='conversations' AND id='thread'`, f.tenant, f.user, "local-"+first); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET parents=ARRAY['conversations:thread'] WHERE tenant_id=$1 AND user_id=$2 AND kind='messages' AND id='m1'`, f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
	f.entity(t, "conversations", "thread-peer", map[string]string{"candidate_id": "local-" + second, "position_id": f.pos, "platform_id": "liepin", "platform_account_id": "account", "platform_thread_id": "platform-peer", "candidate_name": "测试候选人"})
	f.entity(t, "messages", "m-peer", map[string]string{"conversation_id": "thread-peer", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "key-peer"})
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET parents=CASE WHEN kind='conversations' THEN ARRAY['candidates:'||$3] ELSE ARRAY['conversations:thread-peer'] END WHERE tenant_id=$1 AND user_id=$2 AND id IN('thread-peer','m-peer')`, f.tenant, f.user, "local-"+second); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, f.question.ID, "办公地址在哪里", "thread-peer", "key-peer"); err != nil {
		t.Fatal(err)
	}
	f.archive(t, "company", "公司办公地址为成都A座。")
	// 第二版也需由新的真实来源重开；设置父关联，让最后候选人删除继续覆盖整条来源链。
	f.entity(t, "messages", "m-peer-latest", map[string]string{"conversation_id": "thread-peer", "direction": "candidate", "text_content": "办公地址在哪里？", "platform_message_id": "key-peer-latest"})
	if _, err := f.db.Exec(`UPDATE auto_reply_cloud_entities SET parents=ARRAY['conversations:thread-peer'] WHERE tenant_id=$1 AND user_id=$2 AND kind='messages' AND id='m-peer-latest'`, f.tenant, f.user); err != nil {
		t.Fatal(err)
	}
	var err error
	f.question, err = f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, f.question.ID, "办公地址在哪里", "thread-peer", "key-peer-latest")
	if err != nil || f.question.Status != "pending" {
		t.Fatal("新候选来源未重开问题", err)
	}
	f.archive(t, "company", "公司办公地址为成都B座。")
	co, err := f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil {
		t.Fatal(err)
	}
	co.ExtraInfo = "人工后续确认：成都C座。"
	if _, err = f.store.SaveCompanyProfile(ctx, f.tenant, "fact-hr@example.test", co); err != nil {
		t.Fatal(err)
	}
	scope := CandidateLibraryScope{TenantID: f.tenant, UserEmail: "fact-hr@example.test", IsAdmin: true}
	candidates := NewPostgresCandidateStore(f.db)
	if err = candidates.purgeCandidate(ctx, scope, first, firstVersion); err != nil {
		t.Fatal("删除第一位候选人", err)
	}
	var count int
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_question_answer_history WHERE question_id=$1 AND answer<>''", f.question.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("还有其他来源时提前清空历史答复", count, err)
	}
	if err = candidates.purgeCandidate(ctx, scope, second, secondVersion); err != nil {
		t.Fatal("删除最后候选人", err)
	}
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_question_answer_history WHERE question_id=$1 AND answer<>''", f.question.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("最后候选人删除后原文仍留存", count, err)
	}
	if err = f.db.QueryRow("SELECT count(*) FROM auto_reply_question_answer_history WHERE question_id=$1 AND fact<>''", f.question.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("可公开事实审计被误删", count, err)
	}
	co, err = f.store.GetCompanyProfile(ctx, f.tenant, f.company)
	if err != nil || co.ExtraInfo != "人工后续确认：成都C座。" {
		t.Fatal("候选人删除覆盖了人工公司资料", co.ExtraInfo, err)
	}
}
