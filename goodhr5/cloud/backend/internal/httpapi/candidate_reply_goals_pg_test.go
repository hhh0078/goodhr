// 本文件在隔离 PostgreSQL 验证动态目标来源与归档过滤，旧空字段不能导致目标列表丢失。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// TestCandidateGoalsKeepActiveSourcesAndHideArchived 验证 AI/岗位目标清楚区分、缺省归档字段兼容，并隔离其他会话。
func TestCandidateGoalsKeepActiveSourcesAndHideArchived(t *testing.T) {
	db := autoReplyStateTestDB(t)
	var tenant, user string
	if err := db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'goal-history@example.test') RETURNING id", tenant).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", tenant, user); err != nil {
		t.Fatal(err)
	}
	rows := []struct {
		id, source, thread string
		archived           any
		present            bool
	}{
		{"ai", "ai", "thread", nil, true}, {"hr", "position", "thread", "", true}, {"legacy", "", "thread", nil, false}, {"archived", "ai", "thread", "2026-11-09T00:00:00Z", true}, {"other", "ai", "other", nil, false},
	}
	for _, row := range rows {
		payload := map[string]any{"conversation_id": row.thread, "content": row.id, "status": "pending", "source_type": row.source, "updated_at": "2026-11-09T01:00:00Z"}
		if row.present {
			payload["archived_at"] = row.archived
		}
		raw, _ := json.Marshal(payload)
		if _, err := db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,'confirmations',$3,$4,1,1,'2026-11-09T01:00:00Z','{}')`, tenant, user, row.id, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	out := candidateReplyHistory{}
	if err = readHistoryGoals(context.Background(), tx, tenant, user, "thread", &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Goals) != 3 {
		t.Fatalf("活跃目标列表错误 %+v", out.Goals)
	}
	for _, goal := range out.Goals {
		if goal.ID == "ai" && goal.SourceType != "ai" || goal.ID != "ai" && goal.SourceType != "position" {
			t.Fatal("目标来源未兼容旧值", goal)
		}
	}
}
