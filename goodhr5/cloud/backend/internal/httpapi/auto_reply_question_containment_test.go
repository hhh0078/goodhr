// 本文件验证同源长题与子题去重的确定性边界，以及真实PostgreSQL并发登记和HR领取时的行锁安全。
package httpapi

import (
	"context"
	"testing"
	"time"
)

// TestUniqueContainedPendingQuestion 验证唯一包含才可复用，不相含、多条歧义和已填写或已处理的旧题保持独立。
func TestUniqueContainedPendingQuestion(t *testing.T) {
	pending := replyQuestion{ID: "one", Question: "具体做哪些工作？", Status: "pending", Version: 3}
	long := "工作地点在哪里、具体做哪些工作？"
	for _, tc := range []struct {
		name, question string
		candidates     []replyQuestion
		want           bool
	}{
		{name: "长题复用唯一子题", question: long, candidates: []replyQuestion{pending}, want: true},
		{name: "子题复用唯一长题", question: pending.Question, candidates: []replyQuestion{{ID: "one", Question: long, Status: "pending"}}, want: true},
		{name: "没有原题", question: long},
		{name: "空提议", candidates: []replyQuestion{pending}},
		{name: "空旧题", question: long, candidates: []replyQuestion{{ID: "one", Status: "pending"}}},
		{name: "独立子题", question: "工作地点在哪里？", candidates: []replyQuestion{pending}},
		{name: "多个相含题", question: long, candidates: []replyQuestion{pending, {ID: "two", Question: "工作地点在哪里", Status: "pending"}}},
		{name: "含一个处理中题也有歧义", question: long, candidates: []replyQuestion{pending, {ID: "two", Question: long, Status: "processing"}}},
		{name: "HR已填写", question: long, candidates: []replyQuestion{{ID: "one", Question: pending.Question, Status: "pending", Answer: "旧答复"}}},
		{name: "空格答复也不覆盖", question: long, candidates: []replyQuestion{{ID: "one", Question: pending.Question, Status: "pending", Answer: " "}}},
		{name: "正在处理", question: long, candidates: []replyQuestion{{ID: "one", Question: pending.Question, Status: "processing"}}},
		{name: "归档失败", question: long, candidates: []replyQuestion{{ID: "one", Question: pending.Question, Status: "failed"}}},
		{name: "已经归档", question: long, candidates: []replyQuestion{{ID: "one", Question: pending.Question, Status: "resolved"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, found := uniqueContainedPendingQuestion(tc.question, tc.candidates)
			if found != tc.want {
				t.Fatalf("包含复用不符：found=%v want=%v", found, tc.want)
			}
			if found && (q.ID != "one" || q.Question != tc.candidates[0].Question || q.Version != tc.candidates[0].Version) {
				t.Fatal("复用改变旧题目或版本", q)
			}
		})
	}
}

// seedContainmentSource 保存独立的真实候选来信，题目登记仍须经过生产来源核验SQL。
func seedContainmentSource(t *testing.T, f editableFactFixture, id, key, text string) {
	t.Helper()
	f.entity(t, "messages", id, map[string]string{"conversation_id": "thread", "direction": "candidate", "text_content": text, "platform_message_id": key})
}

// containmentSourceCount 统计当前岗位同一真实来源关联的问题数，不以题目字数推测去重结果。
func containmentSourceCount(t *testing.T, f editableFactFixture, messageID string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM auto_reply_cloud_question_sources s JOIN auto_reply_cloud_questions q ON q.id=s.question_id WHERE q.tenant_id=$1 AND q.position_id=$2 AND s.tenant_id=$1 AND s.user_id=$3 AND s.message_id=$4`, f.tenant, f.pos, f.user, messageID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestQuestionContainmentReusesSameSourceInEitherOrder 验证同轮分批及跨请求重复登记，无论长题或子题先到都保留同一旧题和版本。
func TestQuestionContainmentReusesSameSourceInEitherOrder(t *testing.T) {
	const short = "具体做哪些工作？"
	const long = "我想再确认一下，工作地点在哪里、具体做哪些工作？"
	for _, shortFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "先子题", false: "先长题"}[shortFirst], func(t *testing.T) {
			f := newEditableFactFixture(t)
			seedContainmentSource(t, f, "containment-source", "containment-key", long)
			first, second := long, short
			if shortFirst {
				first, second = short, long
			}
			original, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", first, "thread", "containment-key")
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{second, first, second} {
				repeated, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", text, "thread", "containment-key")
				if err != nil || repeated.ID != original.ID || repeated.Question != original.Question || repeated.Answer != original.Answer || repeated.Version != original.Version || !repeated.UpdatedAt.Equal(original.UpdatedAt) {
					t.Fatal("重复请求改写了旧待办", repeated, err)
				}
			}
			if count := containmentSourceCount(t, f, "containment-source"); count != 1 {
				t.Fatal("同源长题和子题仍重复建档", count)
			}
		})
	}
}

// TestQuestionContainmentKeepsIndependentAndAmbiguousTopics 验证同一原话的独立子题可分开登记，包含多题的长标题不能强选其中一条。
func TestQuestionContainmentKeepsIndependentAndAmbiguousTopics(t *testing.T) {
	f := newEditableFactFixture(t)
	const source = "工作地点在哪里？具体做哪些工作？"
	seedContainmentSource(t, f, "multi-source", "multi-key", source)
	ids := map[string]bool{}
	for _, text := range []string{"工作地点在哪里？", "具体做哪些工作？", source} {
		q, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", text, "thread", "multi-key")
		if err != nil || ids[q.ID] {
			t.Fatal("独立子题或歧义长题被强并", q, err)
		}
		ids[q.ID] = true
	}
	if count := containmentSourceCount(t, f, "multi-source"); count != 3 {
		t.Fatal("歧义待办未保留", count)
	}
}

// TestQuestionContainmentPreservesHRWork 验证HR已填写、正在整理、失败或完成时不通过包含规则挪用原题。
func TestQuestionContainmentPreservesHRWork(t *testing.T) {
	f := newEditableFactFixture(t)
	for i, state := range []struct{ status, answer string }{{"pending", "HR已填写"}, {"processing", ""}, {"failed", ""}, {"resolved", ""}} {
		short := "第" + string(rune('一'+i)) + "项工作怎么安排？"
		long := "还想核实一下，" + short
		key := "hr-state-" + state.status
		seedContainmentSource(t, f, key, key, long)
		original, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", short, "thread", key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec("UPDATE auto_reply_cloud_questions SET status=$2,answer=$3,version=version+1 WHERE id=$1", original.ID, state.status, state.answer); err != nil {
			t.Fatal(err)
		}
		created, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", long, "thread", key)
		if err != nil || created.ID == original.ID {
			t.Fatal("包含去重挪用了HR工作", state.status, created, err)
		}
		var status, answer string
		var version int
		if err = f.db.QueryRow("SELECT status,answer,version FROM auto_reply_cloud_questions WHERE id=$1", original.ID).Scan(&status, &answer, &version); err != nil || status != state.status || answer != state.answer || version != original.Version+1 {
			t.Fatal("原题处理状态或答复被改变", state.status, err)
		}
	}
}

// TestQuestionContainmentIsolatesSourceScopes 验证不同真实消息、成员、岗位和团队不能仅凭相含标题共享问题号。
func TestQuestionContainmentIsolatesSourceScopes(t *testing.T) {
	for _, scope := range []string{"message", "user", "position", "tenant"} {
		t.Run(scope, func(t *testing.T) {
			f := newEditableFactFixture(t)
			other := f
			one := func(query string, args ...any) string {
				t.Helper()
				var id string
				if err := f.db.QueryRow(query, args...).Scan(&id); err != nil {
					t.Fatal(err)
				}
				return id
			}
			if scope == "tenant" {
				other.tenant = one("INSERT INTO tenants DEFAULT VALUES RETURNING id")
			}
			if scope == "user" || scope == "tenant" {
				other.user = one("INSERT INTO users(tenant_id,email) VALUES($1,'containment-peer@example.test') RETURNING id", other.tenant)
				if _, err := f.db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", other.tenant, other.user); err != nil {
					t.Fatal(err)
				}
			}
			if scope == "position" || scope == "tenant" {
				other.pos = one("INSERT INTO positions(user_id,name) VALUES($1,'另一岗位') RETURNING id", other.user)
			}
			const short = "这个岗位在哪里办公？"
			const long = "我还想确认，这个岗位在哪里办公？"
			seedContainmentSource(t, f, "scoped-message", "scope-key", long)
			original, err := f.store.recordQuestion(context.Background(), f.tenant, f.user, f.pos, "", long, "thread", "scope-key")
			if err != nil {
				t.Fatal(err)
			}
			other.entity(t, "conversations", "other-thread", map[string]string{"position_id": other.pos})
			message := "scoped-message"
			if scope == "message" || scope == "position" {
				message = "other-message"
			}
			other.entity(t, "messages", message, map[string]string{"conversation_id": "other-thread", "direction": "candidate", "text_content": long, "platform_message_id": "other-key"})
			created, err := other.store.recordQuestion(context.Background(), other.tenant, other.user, other.pos, "", short, "other-thread", "other-key")
			if err != nil || created.ID == original.ID {
				t.Fatal("包含去重跨越来源范围", scope, created, err)
			}
			if scope == "message" || scope == "user" {
				// 同岗位的原有精确题义仍优先于当前来源的另一条相含题，保持旧跨来信共用规则。
				exact, err := other.store.recordQuestion(context.Background(), other.tenant, other.user, other.pos, "", long, "other-thread", "other-key")
				if err != nil || exact.ID != original.ID {
					t.Fatal("包含兜底抢走了原有精确题义", exact, err)
				}
			}
			if _, err = other.store.recordQuestion(context.Background(), other.tenant, other.user, other.pos, "", short, "thread", "scope-key"); (scope == "position" || scope == "tenant" || scope == "user") && err == nil {
				t.Fatal("来源权限检查被包含去重绕过", scope)
			}
		})
	}
}

// TestQuestionContainmentConcurrentRequestsSerialize 验证同一来信的并发长题和子题在数据库消息行排他锁下只生成一条待办。
func TestQuestionContainmentConcurrentRequestsSerialize(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const short = "具体做哪些工作？"
	const long = "工作地点在哪里、具体做哪些工作？"
	seedContainmentSource(t, f, "concurrent-source", "concurrent-key", long)
	block, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var blocker int
	if err = block.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	if _, err = block.ExecContext(ctx, "LOCK TABLE auto_reply_cloud_question_sources IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	firstResult, secondResult := make(chan replyQuestion, 1), make(chan replyQuestion, 1)
	go func() {
		q, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, "", short, "thread", "concurrent-key")
		firstResult <- q
		firstDone <- err
	}()
	firstPID := waitEditableFactBlocked(t, ctx, f.db, blocker, firstDone)
	go func() {
		q, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, "", long, "thread", "concurrent-key")
		secondResult <- q
		secondDone <- err
	}()
	// 第二条须等待第一条持有的消息行，而不是一同进入来源表后各自建题。
	waitEditableFactBlocked(t, ctx, f.db, firstPID, secondDone)
	if err = block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err = <-secondDone; err != nil {
		t.Fatal(err)
	}
	first, second := <-firstResult, <-secondResult
	if first.ID != second.ID || first.Question != short || second.Question != short || first.Version != second.Version || containmentSourceCount(t, f, "concurrent-source") != 1 {
		t.Fatal("并发同源登记仍重复或改写旧题", first, second)
	}
}

// TestQuestionContainmentConcurrentHRClaimWins 验证等待问题锁期间HR填写并领取后，不再复用该题或覆盖HR答复。
func TestQuestionContainmentConcurrentHRClaimWins(t *testing.T) {
	f := newEditableFactFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const long = "想核实一下，办公地址在哪里？"
	seedContainmentSource(t, f, "claim-source", "claim-key", long)
	old, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, "", "办公地址在哪里？", "thread", "claim-key")
	if err != nil {
		t.Fatal(err)
	}
	block, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var blocker int
	if err = block.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	if _, err = block.ExecContext(ctx, "UPDATE auto_reply_cloud_questions SET status='processing',answer='HR正在确认',version=version+1 WHERE id=$1", old.ID); err != nil {
		t.Fatal(err)
	}
	done, result := make(chan error, 1), make(chan replyQuestion, 1)
	go func() {
		q, err := f.store.recordQuestion(ctx, f.tenant, f.user, f.pos, "", long, "thread", "claim-key")
		result <- q
		done <- err
	}()
	waitEditableFactBlocked(t, ctx, f.db, blocker, done)
	if err = block.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if created := <-result; created.ID == old.ID {
		t.Fatal("等待锁后仍复用了已领取题目", created)
	}
	var status, answer string
	if err = f.db.QueryRow("SELECT status,answer FROM auto_reply_cloud_questions WHERE id=$1", old.ID).Scan(&status, &answer); err != nil || status != "processing" || answer != "HR正在确认" {
		t.Fatal("HR已领取答复被覆盖", err)
	}
}
