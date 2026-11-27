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
	"time"
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

// TestAutoReplyLogsClear 验证清空只影响本人展示日志，余额拦截、认证、重放和新日志均保持正确。
func TestAutoReplyLogsClear(t *testing.T) {
	server, routes, token, db := runtimeAIHTTPFixture(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "0129_auto_reply_logs.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	runtimeAICommitForTest(t, routes, runtimeAITenantForTest, runtimeAIOwnerForTest, []AutoReplyStateChange{{Kind: "runtime_events", ID: "clear-fixture", Revision: 1, Sequence: 1, Payload: json.RawMessage(`{"title":"process_conversation","status":"failed"}`)}})
	var other string
	if err = db.QueryRow("INSERT INTO users(tenant_id,email) VALUES($1,'other-log@example.test') RETURNING id", runtimeAITenantForTest).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO auto_reply_state_owners(tenant_id,user_id) VALUES($1,$2)", runtimeAITenantForTest, other); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,'sends','clear-send','{"status":"sent"}',1,2,'','{}'),($1,$3,'runtime_events','clear-other','{}',1,1,'','{}')`, runtimeAITenantForTest, runtimeAIOwnerForTest, other); err != nil {
		t.Fatal(err)
	}
	if err = server.auth.store.SaveSession("clear-extension", Session{Email: runtimeAIEmailForTest, Kind: extensionSessionKind}, time.Hour); err != nil {
		t.Fatal(err)
	}
	// call 经真实HTTP路由发送清空请求，不直接调用删除实现。
	call := func(auth, suffix string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "/api/auto-reply/logs"+suffix, nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		return w
	}
	for auth, want := range map[string]int{"": 401, "clear-extension": 403} {
		if got := call(auth, ""); got.Code != want {
			t.Fatalf("认证拦截失败：%d %s", got.Code, got.Body.String())
		}
	}
	wallet := NewMemoryAIWalletStore()
	server.autoReply.aiWallet = wallet
	wallet.balances[runtimeAIEmailForTest] = 522
	if got := call(token, "?before_start=true"); got.Code != 402 {
		t.Fatalf("低余额清空未拦截：%d %s", got.Code, got.Body.String())
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", runtimeAIOwnerForTest).Scan(&count); err != nil || count != 1 {
		t.Fatal("低余额或无权限请求删除了日志", count, err)
	}
	wallet.balances[runtimeAIEmailForTest] = 1000
	got := call(token, "?before_start=true&user_id="+other)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"deleted_count":1`) {
		t.Fatalf("清空失败：%d %s", got.Code, got.Body.String())
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", other).Scan(&count); err != nil || count != 1 {
		t.Fatal("误删其他账号日志", count, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_cloud_entities WHERE user_id=$1", runtimeAIOwnerForTest).Scan(&count); err != nil || count != 2 {
		t.Fatal("清空影响权威事件或发送去重账本", count, err)
	}
	wallet.balances[runtimeAIEmailForTest] = 0
	if got = call(token, ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"deleted_count":0`) {
		t.Fatal("空日志或低余额不能手动清空", got.Code, got.Body.String())
	}
	if _, err = db.Exec(`UPDATE auto_reply_cloud_entities SET revision=2 WHERE user_id=$1 AND kind='runtime_events' AND id='clear-fixture'`, runtimeAIOwnerForTest); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", runtimeAIOwnerForTest).Scan(&count); err != nil || count != 0 {
		t.Fatal("清空后重放旧事件恢复了展示日志", count, err)
	}
	if _, err = db.Exec(`INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,parents) VALUES($1,$2,'runtime_events','clear-new','{}',1,3,'','{}')`, runtimeAITenantForTest, runtimeAIOwnerForTest); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow("SELECT count(*) FROM auto_reply_logs WHERE user_id=$1", runtimeAIOwnerForTest).Scan(&count); err != nil || count != 1 {
		t.Fatal("清空后的新日志无法显示", count, err)
	}
}
