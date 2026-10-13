// 本文件验证用户简历数量的创建人归属、示例排除、分页搜索和管理员接口权限，数据库测试仅使用显式指定的本机隔离测试库。
package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestAdminUserResumeCountsAPI 验证真实添加后每个用户只返回自己的简历数量，内置示例不计入且普通用户不能读取。
func TestAdminUserResumeCountsAPI(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	owners := []struct {
		email string
		count int
	}{
		{"resume-count-owner@example.com", 2},
		{"resume-count-member@example.com", 1},
		{"resume-count-empty@example.com", 0},
	}
	for ownerIndex, owner := range owners {
		token := loginForTest(t, routes, owner.email)
		for index := 0; index < owner.count; index++ {
			status, _ := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{
				"candidate_name": fmt.Sprintf("计数测试%d-%d", ownerIndex, index),
				"phone":          fmt.Sprintf("13800000%d%02d", ownerIndex, index),
			})
			if status != http.StatusOK {
				t.Fatalf("create candidate status=%d", status)
			}
		}
		status, library := candidateTestRequest(t, routes, token, http.MethodGet, "/api/candidates", nil)
		if status != http.StatusOK || library["builtin_total"] != float64(1) {
			t.Fatalf("library should include the virtual example: status=%d body=%v", status, library)
		}
		for _, access := range []struct {
			token string
			code  int
		}{{"", http.StatusUnauthorized}, {token, http.StatusForbidden}} {
			status, _ := candidateTestRequest(t, routes, access.token, http.MethodGet, "/api/admin/users", nil)
			if status != access.code {
				t.Fatalf("user resume statistics status=%d want=%d", status, access.code)
			}
		}
	}
	for _, owner := range owners {
		path := "/api/admin/users?q=" + url.QueryEscape(owner.email) + "&page_size=1"
		status, body := candidateTestRequest(t, routes, adminToken, http.MethodGet, path, nil)
		users, ok := body["users"].([]any)
		if status != http.StatusOK || !ok || len(users) != 1 {
			t.Fatalf("user list status=%d body=%v", status, body)
		}
		user := users[0].(map[string]any)
		if user["email"] != owner.email || user["resume_count"] != float64(owner.count) {
			t.Fatalf("incorrect owner resume count: %v", user)
		}
		for _, key := range []string{"candidates", "candidate_name", "phone", "raw_text"} {
			if _, exposed := user[key]; exposed {
				t.Fatalf("user list exposed candidate details: %s", key)
			}
		}
	}
}

// TestAdminUserResumeCountsMemory 验证同一团队按创建账号计数，不重复计算触达关系，并忽略误写入存储的内置示例。
func TestAdminUserResumeCountsMemory(t *testing.T) {
	subscriptions := NewMemorySubscriptionStore()
	candidates := NewMemoryCandidateStore()
	for _, email := range []string{"count-owner@example.test", "count-member@example.test", "count-empty@example.test"} {
		if _, err := subscriptions.UserSubscription(email); err != nil {
			t.Fatal(err)
		}
	}
	candidates.profiles = map[string]PositionCandidate{
		"first":            {ID: "first", TenantID: "same-team", UserEmail: "count-owner@example.test"},
		"second":           {ID: "second", TenantID: "same-team", UserEmail: "count-owner@example.test"},
		"third":            {ID: "third", TenantID: "same-team", UserEmail: "count-member@example.test"},
		builtinCandidateID: {ID: builtinCandidateID, TenantID: "same-team", UserEmail: "count-owner@example.test"},
	}
	for _, positionID := range []string{"one", "two"} {
		if _, err := candidates.UpsertCandidateEngagement(CandidateEngagement{CandidateID: "first", PositionID: positionID}); err != nil {
			t.Fatal(err)
		}
	}
	store := NewMemoryAdminUserStore(subscriptions, nil, nil, candidates)
	for email, want := range map[string]int64{"count-owner@example.test": 2, "count-member@example.test": 1, "count-empty@example.test": 0} {
		result, err := store.ListUsers(AdminUserListQuery{Query: email, PageSize: 1})
		if err != nil || result.Total != 1 || len(result.Users) != 1 || result.Users[0].ResumeCount != want {
			t.Fatalf("memory resume count email=%s result=%+v err=%v", email, result, err)
		}
	}
	candidates.mu.Lock()
	delete(candidates.profiles, "second")
	candidates.mu.Unlock()
	result, err := store.ListUsers(AdminUserListQuery{Query: "count-owner@example.test", PageSize: 1})
	if err != nil || result.Users[0].ResumeCount != 1 {
		t.Fatalf("deleted candidate stayed in count: %+v err=%v", result, err)
	}
	legacy := NewMemoryAdminUserStore(subscriptions, nil, nil)
	result, err = legacy.ListUsers(AdminUserListQuery{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range result.Users {
		if user.ResumeCount != 0 {
			t.Fatalf("missing candidate store should return zero: %+v", user)
		}
	}
}

// TestAdminUserResumeCountsPostgres 验证真实聚合 SQL 的创建人隔离、空库零值、示例排除与搜索分页，不使用任何生产数据。
func TestAdminUserResumeCountsPostgres(t *testing.T) {
	subscriptionJSON := toJSONB(Subscription{MemberType: memberTypePlus, ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)})
	if _, err := parseSubscription(subscriptionJSON); err != nil {
		t.Fatalf("fixture subscription must match the production parser: %v", err)
	}
	if _, err := decodeNotificationProfile([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := parseUserFlowState([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	db := candidatePostgresTestDB(t)
	if _, err := db.Exec(`
		ALTER TABLE users
			ADD COLUMN wechat_openid TEXT NOT NULL DEFAULT '',
			ADD COLUMN role TEXT NOT NULL DEFAULT 'user',
			ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
			ADD COLUMN ai_balance_units BIGINT NOT NULL DEFAULT 0,
			ADD COLUMN subscription JSONB NOT NULL DEFAULT '{}'::jsonb,
			ADD COLUMN notification_profile JSONB NOT NULL DEFAULT '{}'::jsonb,
			ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			ADD COLUMN last_login_at TIMESTAMPTZ,
			ADD COLUMN inviter_id UUID,
			ADD COLUMN flow_state JSONB NOT NULL DEFAULT '{}'::jsonb;
		COMMENT ON COLUMN users.wechat_openid IS '测试用户公众号身份';
		COMMENT ON COLUMN users.role IS '测试用户角色';
		COMMENT ON COLUMN users.status IS '测试用户状态';
		COMMENT ON COLUMN users.ai_balance_units IS '测试用户AI余额';
		COMMENT ON COLUMN users.subscription IS '测试用户会员信息';
		COMMENT ON COLUMN users.notification_profile IS '测试用户通知画像';
		COMMENT ON COLUMN users.created_at IS '测试用户创建时间';
		COMMENT ON COLUMN users.last_login_at IS '测试用户最后登录时间';
		COMMENT ON COLUMN users.inviter_id IS '测试用户邀请人';
		COMMENT ON COLUMN users.flow_state IS '测试用户流程状态';
	`); err != nil {
		t.Fatal(err)
	}
	var tenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for index := 0; index < 3; index++ {
		var id string
		if err := db.QueryRow(`INSERT INTO users(email,tenant_id,created_at,subscription) VALUES($1,$2,now()-$3*interval '1 day',$4::jsonb) RETURNING id::text`, fmt.Sprintf("pg-count-%d@example.test", index), tenantID, index, string(subscriptionJSON)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for index, ownerID := range []string{ids[0], ids[0], ids[1]} {
		if _, err := db.Exec(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_candidate_id) VALUES($1,$2,$3)`, tenantID, ownerID, fmt.Sprintf("count-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO candidate_profiles(id,tenant_id,created_by_user_id,source_platform_candidate_id) VALUES($1,$2,$3,'count-builtin')`, builtinCandidateID, tenantID, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO candidate_profiles(tenant_id,source_platform_candidate_id) VALUES($1,'count-unowned')`, tenantID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresAdminUserStore(db)
	result, err := store.ListUsers(AdminUserListQuery{PageSize: 100})
	if err != nil || result.Total != 3 || len(result.Users) != 3 {
		t.Fatalf("Postgres user list result=%+v err=%v", result, err)
	}
	for index, want := range []int64{2, 1, 0} {
		if result.Users[index].ResumeCount != want {
			t.Fatalf("Postgres owner count index=%d got=%d want=%d", index, result.Users[index].ResumeCount, want)
		}
	}
	result, err = store.ListUsers(AdminUserListQuery{Page: 2, PageSize: 1})
	if err != nil || result.Total != 3 || len(result.Users) != 1 || result.Users[0].ResumeCount != 1 {
		t.Fatalf("Postgres paged count result=%+v err=%v", result, err)
	}
	result, err = store.ListUsers(AdminUserListQuery{Query: "pg-count-0", PageSize: 1})
	if err != nil || result.Total != 1 || len(result.Users) != 1 || result.Users[0].ResumeCount != 2 {
		t.Fatalf("Postgres search count result=%+v err=%v", result, err)
	}
}
