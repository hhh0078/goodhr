// Package httpapi 本文件用隔离PostgreSQL验证独立日志千条上限、账号隔离、更新幂等和来源删除，保留发送权威账本。
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAutoReplyLogsHTTP 验证真实登录路由只返回本人日志，错误详情可读而凭据和额外字段不会透出。
func TestAutoReplyLogsHTTP(t *testing.T) {
	_, routes, token, db := runtimeAIHTTPFixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0129_auto_reply_logs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, []AutoReplyStateChange{{Kind: "runtime_events", ID: "fixture-failed", Revision: 1, Sequence: 1, UpdatedAt: "", Payload: json.RawMessage(`{"title":"process_conversation","status":"failed","error_detail":"BOSS会话编号无法核实 token=PRIVATE_CREDENTIAL","private_extra":"PRIVATE_EXTRA"}`)}})
	req := httptest.NewRequest("GET", "/api/auto-reply/logs?user_id=someone-else", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "BOSS会话编号无法核实") || strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatalf("详情或权限边界异常：%d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest("GET", "/api/auto-reply/logs", nil)
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("未登录可读取运行日志：%d", w.Code)
	}
}

// TestAutoReplyLogRetention 验证数据库实际触发器约束，而不是只检查接口返回条数。
func TestAutoReplyLogRetention(t *testing.T) {
	db := autoReplyStateTestDB(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0129_auto_reply_logs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	var tenant, owner, other string
	if err = db.QueryRow("INSERT INTO tenants DEFAULT VALUES RETURNING id").Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	for i, who := range []*string{&owner, &other} {
		if err = db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,$2) RETURNING id", tenant, fmt.Sprintf("log-%d@fixture.test", i)).Scan(who); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", tenant, *who); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) SELECT $1,$2,'runtime_events','event-'||g,jsonb_build_object('title','process_conversation','description','合成日志'),1,g,'','{}' FROM generate_series(1,1005) g`, tenant, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,'runtime_events','event-1','{}',1,1,'','{}'),($1,$3,'sends','receipt','{"status":"sent"}',1,1,'','{}')`, tenant, other, owner); err != nil {
		t.Fatal(err)
	}
	var count, authority int
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", owner).Scan(&count); err != nil || count != 1000 {
		t.Fatalf("上限错误：%d %v", count, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", other).Scan(&count); err != nil || count != 1 {
		t.Fatalf("其他账号受到清理：%d %v", count, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE user_id=$1", owner).Scan(&authority); err != nil || authority != 1006 {
		t.Fatal("展示清理影响了权威账本", authority, err)
	}
	if _, err = db.Exec(`UPDATE auto_reply_cloud_entities SET payload='{"description":"更新旧事件"}' WHERE user_id=$1 AND kind='runtime_events' AND id='event-1'`, owner); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1 AND event_id='event-1'", owner).Scan(&count); err != nil || count != 0 {
		t.Fatal("已淘汰事件被重放复活", err)
	}
	if _, err = db.Exec(`UPDATE auto_reply_cloud_entities SET payload='{"description":"实际错误原因"}' WHERE user_id=$1 AND kind='runtime_events' AND id='event-1005'`, owner); err != nil {
		t.Fatal(err)
	}
	var description string
	if err = db.QueryRow("SELECT payload->>'description' FROM auto_reply_logs WHERE user_id=$1 AND event_id='event-1005'", owner).Scan(&description); err != nil || description != "实际错误原因" {
		t.Fatal("更新没有投影具体原因", err)
	}
	if _, err = db.Exec(`DELETE FROM auto_reply_cloud_entities WHERE user_id=$1 AND kind='runtime_events' AND id='event-1005'`, owner); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", owner).Scan(&count); err != nil || count != 999 {
		t.Fatal("源事件删除后展示残留", count, err)
	}
}
