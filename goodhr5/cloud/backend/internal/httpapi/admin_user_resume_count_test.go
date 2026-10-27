// 本文件验证用户简历数量的创建人归属、示例排除、分页搜索和管理员接口权限，数据库测试仅使用显式指定的本机隔离测试库。
package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

// TestAdminUserResumeCountSortingAPI 验证管理员按全部匹配用户的简历数排序后分页，保留零值并稳定排列相同数量。
func TestAdminUserResumeCountSortingAPI(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	owners := []struct {
		email string
		count int
	}{
		{"resume-sort-z-empty@example.com", 0},
		{"resume-sort-b-two@example.com", 2},
		{"resume-sort-d-one@example.com", 1},
		{"resume-sort-a-two@example.com", 2},
	}
	for ownerIndex, owner := range owners {
		token := loginForTest(t, routes, owner.email)
		for index := 0; index < owner.count; index++ {
			status, body := candidateTestRequest(t, routes, token, http.MethodPost, "/api/candidates", map[string]any{
				"candidate_name": fmt.Sprintf("排序测试%d-%d", ownerIndex, index),
				"phone":          fmt.Sprintf("13900000%d%02d", ownerIndex, index),
			})
			if status != http.StatusOK {
				t.Fatalf("create sorting fixture status=%d body=%v", status, body)
			}
		}
	}
	for _, test := range []struct {
		order string
		want  []string
	}{
		{"asc", []string{owners[0].email, owners[2].email, owners[3].email, owners[1].email}},
		{"desc", []string{owners[3].email, owners[1].email, owners[2].email, owners[0].email}},
	} {
		t.Run(test.order, func(t *testing.T) {
			for repeat := 0; repeat < 3; repeat++ {
				for index, email := range test.want {
					path := fmt.Sprintf("/api/admin/users?q=resume-sort-&sort_by=resume_count&sort_order=%s&page=%d&page_size=1", test.order, index+1)
					status, body := candidateTestRequest(t, routes, adminToken, http.MethodGet, path, nil)
					users, ok := body["users"].([]any)
					if status != http.StatusOK || !ok || len(users) != 1 || body["total"] != float64(4) || body["page"] != float64(index+1) {
						t.Fatalf("sorted page status=%d body=%v", status, body)
					}
					if user := users[0].(map[string]any); user["email"] != email {
						t.Fatalf("sorted page=%d repeat=%d got=%v want=%s", index+1, repeat, user, email)
					}
				}
			}
		})
	}
	status, body := candidateTestRequest(t, routes, adminToken, http.MethodGet, "/api/admin/users?q=resume-sort-z-empty&sort_by=resume_count&sort_order=desc&page_size=1", nil)
	users, ok := body["users"].([]any)
	if status != http.StatusOK || !ok || len(users) != 1 || body["total"] != float64(1) || users[0].(map[string]any)["resume_count"] != float64(0) {
		t.Fatalf("sorting must preserve a searched zero-count user: status=%d body=%v", status, body)
	}
	status, body = candidateTestRequest(t, routes, adminToken, http.MethodGet, "/api/admin/users?q=resume-sort-&sort_by=resume_count&sort_order=asc&page=5&page_size=1", nil)
	users, ok = body["users"].([]any)
	if status != http.StatusOK || !ok || len(users) != 0 || body["total"] != float64(4) {
		t.Fatalf("out-of-range sorted page should be empty: status=%d body=%v", status, body)
	}
}

// TestAdminUserResumeCountSortWhitelist 验证请求和存储层仅接受固定排序白名单，非法输入不会进入 SQL。
func TestAdminUserResumeCountSortWhitelist(t *testing.T) {
	for _, test := range []struct {
		name      string
		sortBy    string
		sortOrder string
		wantBy    string
		wantOrder string
		wantSQL   string
	}{
		{"default", "", "", "", "", "u.created_at DESC"},
		{"ascending", "resume_count", "asc", "resume_count", "asc", "COALESCE(resume_stats.resume_count, 0) ASC, u.email ASC, u.id ASC"},
		{"descending", "resume_count", "desc", "resume_count", "desc", "COALESCE(resume_stats.resume_count, 0) DESC, u.email ASC, u.id ASC"},
		{"normalized", " RESUME_COUNT ", " ASC ", "resume_count", "asc", "COALESCE(resume_stats.resume_count, 0) ASC, u.email ASC, u.id ASC"},
		{"missing direction", "resume_count", "", "", "", "u.created_at DESC"},
		{"unknown field", "email", "asc", "", "", "u.created_at DESC"},
		{"unknown direction", "resume_count", "ascending", "", "", "u.created_at DESC"},
		{"field injection", "resume_count DESC; DROP TABLE users", "asc", "", "", "u.created_at DESC"},
		{"direction injection", "resume_count", "desc; DROP TABLE users", "", "", "u.created_at DESC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := url.Values{"sort_by": {test.sortBy}, "sort_order": {test.sortOrder}}
			request := httptest.NewRequest(http.MethodGet, "/api/admin/users?"+values.Encode(), nil)
			query := adminUserListQueryFromRequest(request)
			if query.SortBy != test.wantBy || query.SortOrder != test.wantOrder {
				t.Fatalf("normalized sort got=(%q,%q) want=(%q,%q)", query.SortBy, query.SortOrder, test.wantBy, test.wantOrder)
			}
			if got := adminUserOrderBy(AdminUserListQuery{SortBy: test.sortBy, SortOrder: test.sortOrder}); got != test.wantSQL {
				t.Fatalf("store SQL must only use fixed whitelist: got=%q want=%q", got, test.wantSQL)
			}
		})
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
	var tiedID string
	if err := db.QueryRow(`INSERT INTO users(email,tenant_id,created_at,subscription) VALUES('pg-count-3@example.test',$1,now()-interval '3 days',$2::jsonb) RETURNING id::text`, tenantID, string(subscriptionJSON)).Scan(&tiedID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := db.Exec(`INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_candidate_id) VALUES($1,$2,$3)`, tenantID, tiedID, fmt.Sprintf("count-tied-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		order string
		want  []string
	}{
		{"asc", []string{"pg-count-2@example.test", "pg-count-1@example.test", "pg-count-0@example.test", "pg-count-3@example.test"}},
		{"desc", []string{"pg-count-0@example.test", "pg-count-3@example.test", "pg-count-1@example.test", "pg-count-2@example.test"}},
	} {
		t.Run("sort_"+test.order, func(t *testing.T) {
			for repeat := 0; repeat < 2; repeat++ {
				for index, email := range test.want {
					result, err := store.ListUsers(AdminUserListQuery{Query: "pg-count-", Page: index + 1, PageSize: 1, SortBy: "resume_count", SortOrder: test.order})
					if err != nil || result.Total != 4 || len(result.Users) != 1 || result.Users[0].Email != email {
						t.Fatalf("Postgres sorted page=%d repeat=%d result=%+v err=%v want=%s", index+1, repeat, result, err, email)
					}
				}
			}
		})
	}
}
