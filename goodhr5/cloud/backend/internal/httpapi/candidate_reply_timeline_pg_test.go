// 本文件使用隔离 PostgreSQL 验证候选人操作时间线的归属、计时依据与分类分页。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestCandidateReplyTimelineBoundDurations 以相同会话名和多页核验数据测试只读取真实绑定、只计算实际执行的起止。
func TestCandidateReplyTimelineBoundDurations(t *testing.T) {
	db := autoReplyStateTestDB(t)
	var tenant, owner string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'timeline@example.test') RETURNING id", tenant).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", tenant, owner); err != nil {
		t.Fatal(err)
	}
	const thread = "thread"
	const began = "2026-11-09T05:00:01.300Z"
	const ended = "2026-11-09T05:00:02.550Z"
	insert := func(kind, id string, payload map[string]any) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO auto_reply_cloud_entities (tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,$3,$4,$5,1,1,$6,'{}')`, tenant, owner, kind, id, string(raw), ended); err != nil {
			t.Fatal(err)
		}
	}
	record := func(conv, action, status string) map[string]any {
		return map[string]any{"conversation_id": conv, "action": action, "status": status, "started_at": began, "updated_at": ended}
	}
	insert("runtime_commands", "download", record(thread, "collect_resume", "completed"))
	insert("runtime_commands", "lost", record(thread, "send", "unknown"))
	insert("runtime_commands", "wrong", record("different", "send", "completed"))
	insert("runtime_commands", "unbound", record("", "scan", "completed"))
	insert("runtime_events", "measured", map[string]any{"conversation_id": thread, "title": "resume_ai", "occurred_at": ended, "duration_ms": 711})
	insert("runs", "round", map[string]any{"conversation_id": thread, "status": "completed", "started_at": began, "completed_at": ended})
	insert("tools", "tool", map[string]any{"ai_run_id": "round", "tool_name": "finish_review", "started_at": began, "completed_at": ended})
	insert("tools", "legacy", map[string]any{"ai_run_id": "round", "tool_name": "finish_review", "started_at": "0001-01-01T00:00:00Z"})
	insert("recommendations", "job", map[string]any{"conversation_id": thread, "input_hash": "hash", "started_at": began, "completed_at": ended})
	insert("reports", "hash", map[string]any{"report": map[string]any{"generated_at": ended}})
	insert("reports", "foreign-hash", map[string]any{"report": map[string]any{"generated_at": ended}})
	for i := 0; i < 53; i++ {
		insert("runtime_commands", fmt.Sprintf("probe-%02d", i), record(thread, "verify", "completed"))
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	read := func(mode, cursor string) (candidateReplyHistory, error) {
		var out candidateReplyHistory
		err := readHistoryEvents(context.Background(), tx, tenant, owner, thread, cursor, &out, mode)
		return out, err
	}
	primary, err := read("primary", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(primary.Events) != 8 {
		t.Fatalf("应收到8条本会话业务记录，实际%d", len(primary.Events))
	}
	want := map[string]int64{"runtime_commands:download": 1250, "runs:round": 1250, "tools:tool": 1250, "recommendations:job": 1250, "runtime_events:measured": 711}
	for _, e := range primary.Events {
		if strings.Contains(e.OccurredAt, "0001-") || e.Group == "verification" {
			t.Fatalf("默认包含无效时间或核验：%+v", e)
		}
		if ms, ok := want[e.ID]; ok {
			if e.DurationMS == nil || *e.DurationMS != ms {
				t.Fatalf("真实耗时错误：%+v", e)
			}
			delete(want, e.ID)
		} else if e.DurationMS != nil {
			t.Fatalf("未知步骤耗时不应推算：%+v", e)
		}
		if e.ID == "runtime_commands:download" && (e.Title != "下载简历附件" || e.Group != "action") {
			t.Fatal(e)
		}
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	page, err := read("verification", "")
	if err != nil || len(page.Events) != 50 || page.NextEventCursor == "" {
		t.Fatal(len(page.Events), err)
	}
	if _, err = read("primary", page.NextEventCursor); err == nil {
		t.Fatal("分类间游标错误通过")
	}
	next, err := read("verification", page.NextEventCursor)
	if err != nil || len(next.Events) != 3 {
		t.Fatal(len(next.Events), err)
	}
	seen := map[string]bool{}
	for _, e := range append(page.Events, next.Events...) {
		if e.Title != "核对当前会话身份" || e.Group != "verification" || seen[e.ID] {
			t.Fatal(e)
		}
		seen[e.ID] = true
	}
	all, err := read("all", "")
	if err != nil || len(all.Events) != 50 || all.NextEventCursor == "" {
		t.Fatal(len(all.Events), err)
	}
}
