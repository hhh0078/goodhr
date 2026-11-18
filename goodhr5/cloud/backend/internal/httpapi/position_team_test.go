// 本文件验证团队岗位列表、管理员代改、成员复制归属和越权保护，仅使用隔离内存及本机测试数据库。
package httpapi

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPositionTeamPermissions 验证团队管理员可代改、普通成员只能改本人，跨团队与失效权限均不能代改。
func TestPositionTeamPermissions(t *testing.T) {
	checkPositionTeamPermissions(t, mustNewServer(t))
}

// TestPositionTeamPostgresPermissions 在显式指定的本机测试库验证真实团队查询和代改保存，禁止连接业务库。
func TestPositionTeamPostgresPermissions(t *testing.T) {
	dsn := os.Getenv("GOODHR_TEAM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未指定本机团队测试数据库")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("团队岗位集成测试仅允许回环地址的_test库")
	}
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(directory) }()
	db, err := (Config{PostgresDSN: dsn}).PostgresDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := mustNewServer(t)
	server.auth.tenantStore = NewPostgresTenantStore(db)
	server.positions.store = NewPostgresPositionStore(db)
	checkPositionTeamPermissions(t, server)
}

// checkPositionTeamPermissions 对内存和PostgreSQL使用同一套请求验证权限及历史保持。
func checkPositionTeamPermissions(t *testing.T, server *Server) {
	t.Helper()
	routes := server.Routes()
	suffix := time.Now().Format("150405.000000000") + "@example.com"
	owner, admin, member, colleague, outsider := "team-owner-"+suffix, "team-admin-"+suffix, "team-member-"+suffix, "team-colleague-"+suffix, "team-outsider-"+suffix
	tokens := map[string]string{}
	for _, email := range []string{owner, admin, member, colleague, outsider} {
		tokens[email] = loginForTest(t, routes, email)
	}
	tenants := server.auth.tenantStore
	tenant, err := tenants.GetOrCreateTenant(owner)
	if err != nil {
		t.Fatal(err)
	}
	for email, role := range map[string]string{admin: "admin", member: "user", colleague: "user"} {
		invitation, _, err := tenants.InviteMember(tenant.ID, email, role, owner)
		if err != nil {
			t.Fatal(err)
		}
		if err = tenants.AcceptInvitation(invitation.ID, email); err != nil {
			t.Fatal(err)
		}
	}
	// 待接受的邀请不等于团队成员，不能看到或修改对方岗位。
	if _, _, err = tenants.InviteMember(tenant.ID, outsider, "user", owner); err != nil {
		t.Fatal(err)
	}
	store := server.positions.store
	position, err := store.SavePosition(Position{UserEmail: member, Name: "Java工程师", PlatformID: "boss", AIConfig: map[string]any{"position_requirement": "三年Java经验"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.IncrementPositionCounts(position.ID, 9, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err = store.FinishPositionRun(position.ID, "stopped", 3); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SavePosition(Position{UserEmail: outsider, Name: "外部岗位"}); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{owner, admin, member, colleague} {
		status, data := candidateTestRequest(t, routes, tokens[email], http.MethodGet, "/api/positions?is_open=true", nil)
		if status != 200 || len(data["positions"].([]any)) != 1 {
			t.Fatalf("%s 团队列表错误：%d %v", email, status, data)
		}
		item := data["positions"].([]any)[0].(map[string]any)
		if item["creator_email"] != member || item["can_edit"] != (email != colleague) || item["is_current_user"] != (email == member) {
			t.Fatalf("%s 权限标记错误：%v", email, item)
		}
	}
	for email, expected := range map[string]int{owner: 200, admin: 200, member: 200, colleague: 403, outsider: 404} {
		status, data := candidateTestRequest(t, routes, tokens[email], http.MethodPost, "/api/positions", map[string]any{"id": position.ID, "name": "代改后的岗位", "creator_email": email, "user_email": email, "is_open": false})
		if status != expected {
			t.Fatalf("%s 保存状态=%d，预期%d：%v", email, status, expected, data)
		}
	}
	saved, err := store.PositionByID("", member, position.ID, false)
	if err != nil || saved.UserEmail != member || saved.ScannedCount != 9 || saved.DailyGreetedCount != 3 || saved.IsOpen {
		t.Fatalf("代改改变了归属、历史或开关：%+v %v", saved, err)
	}
	for _, action := range []struct{ method, path string }{{http.MethodDelete, ""}, {http.MethodPost, "/start"}, {http.MethodPost, "/stop"}} {
		status, _ := candidateTestRequest(t, routes, tokens[admin], action.method, "/api/positions/"+position.ID+action.path, map[string]any{})
		if status == 200 {
			t.Fatalf("代改权限意外放开了%s %s", action.method, action.path)
		}
	}
	if err = tenants.UpdateMemberRole(tenant.ID, admin, "user"); err != nil {
		t.Fatal(err)
	}
	status, _ := candidateTestRequest(t, routes, tokens[admin], http.MethodPost, "/api/positions", map[string]any{"id": position.ID, "name": "降级后越权"})
	if status != 403 {
		t.Fatal("降级后仍能代改", status)
	}
	if err = tenants.RemoveMember(tenant.ID, member); err != nil {
		t.Fatal(err)
	}
	status, data := candidateTestRequest(t, routes, tokens[owner], http.MethodGet, "/api/positions?is_open=all", nil)
	if status != 200 || len(data["positions"].([]any)) != 0 {
		t.Fatal("移出成员的岗位仍在团队列表", status, data)
	}
	status, _ = candidateTestRequest(t, routes, tokens[owner], http.MethodPost, "/api/positions", map[string]any{"id": position.ID, "name": "跨团队越权"})
	if status != 404 {
		t.Fatal("移出后仍能代改", status)
	}
}

// TestPositionCopyCreatesOwnPosition 验证复制表单提交生成本人新岗位，不复制统计、状态、时间或原归属。
func TestPositionCopyCreatesOwnPosition(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "copy-member@example.com"
	token := loginForTest(t, routes, email)
	status, data := candidateTestRequest(t, routes, token, http.MethodPost, "/api/positions", map[string]any{
		"id": "", "name": "Java工程师", "platform_id": "51job", "is_open": true,
		"creator_email": "source@example.com", "user_email": "source@example.com", "status": "running", "scanned_count": 999, "today_greeted_count": 999,
		"created_at": "2020-01-01T00:00:00Z", "common_config": map[string]any{"request_phone": true}, "ai_config": map[string]any{"position_requirement": "三年Java经验"},
	})
	if status != 200 {
		t.Fatal("复制保存失败", status, data)
	}
	item := data["position"].(map[string]any)
	if item["creator_email"] != email || item["status"] != "created" || item["scanned_count"] != float64(0) || item["today_greeted_count"] != float64(0) || item["platform_id"] != "51job" || item["is_open"] != true || item["ai_config"].(map[string]any)["position_requirement"] != "三年Java经验" {
		t.Fatalf("复制归属或配置错误：%v", item)
	}
}
