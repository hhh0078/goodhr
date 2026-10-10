// 文件作用：使用本机独立测试schema验证插件来源SQL的平台别名、多来源查找、历史兼容与账号/团队隔离。
package httpapi

import (
	"fmt"
	"testing"
)

// TestCandidatePostgresPluginSourcePlatforms 验证来源筛选条件在真实PostgreSQL中正确匹配全部来源及历史别名。
func TestCandidatePostgresPluginSourcePlatforms(t *testing.T) {
	db := candidatePostgresTestDB(t)
	var tenantID, otherTenantID string
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id::text`).Scan(&otherTenantID); err != nil {
		t.Fatal(err)
	}
	owner, member := "plugin-source-owner@example.test", "plugin-source-member@example.test"
	if _, err := db.Exec(`INSERT INTO users(email,tenant_id) VALUES($1,$3::uuid),($2,$3::uuid)`, owner, member, tenantID); err != nil {
		t.Fatal(err)
	}
	store := NewPostgresCandidateStore(db)
	fixtures := []struct {
		platform, kind, email string
		sources               []CandidateSource
	}{
		{"boss", "manual", owner, []CandidateSource{{PlatformID: "hliepin", SourceType: "browser_extension"}, {PlatformID: "zhaopin", SourceType: "browser_extension"}, {PlatformID: "boss", SourceType: "automatic"}}},
		{"hliepin", "browser_extension", owner, []CandidateSource{{PlatformID: "zhaopin", SourceType: ""}}},
		{"boss", "automatic", owner, []CandidateSource{{PlatformID: "boss", SourceType: "automatic"}}},
		{"51job", "browser_extension", member, nil},
	}
	for index, fixture := range fixtures {
		candidate, err := store.SaveCandidateProfile(CandidateProfileInput{UserEmail: fixture.email, PlatformID: fixture.platform, PlatformCandidateID: fmt.Sprintf("fixture-platform-%d", index), CandidateName: "平台来源测试"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`UPDATE candidate_profiles SET library_fields=jsonb_build_object('source_type',$1::text) WHERE id=$2::uuid`, fixture.kind, candidate.ID); err != nil {
			t.Fatal(err)
		}
		// 合成记录按实际首次来源创建，删除采集器为了测试建档自动补写的来源。
		if _, err = db.Exec(`DELETE FROM candidate_profile_sources WHERE candidate_id=$1::uuid`, candidate.ID); err != nil {
			t.Fatal(err)
		}
		for number, source := range fixture.sources {
			if _, err = db.Exec(`INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type) VALUES($1::uuid,$2::uuid,$3,$4,$5)`, tenantID, candidate.ID, source.PlatformID, fmt.Sprintf("secondary-%d-%d", index, number), source.SourceType); err != nil {
				t.Fatal(err)
			}
		}
		if index == 0 {
			if _, err = db.Exec(`INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type) VALUES($1::uuid,$2::uuid,'boss','wrong-team','browser_extension')`, otherTenantID, candidate.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, test := range []struct {
		source string
		count  int
	}{{"", 3}, {"manual", 1}, {"browser_extension", 2}, {"browser_extension:liepin", 2}, {"browser_extension:hliepin", 2}, {"browser_extension:zhilian", 2}, {"browser_extension:zhaopin", 2}, {"browser_extension:boss", 0}, {"browser_extension:51job", 0}, {"automatic", 2}, {"ai_import", 0}, {"browser_extension:unknown", 0}, {"browser_extension:boss' OR 1=1", 0}} {
		result, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{UserEmail: owner, SourceType: test.source, PageSize: 100})
		if err != nil || result.Total != test.count || len(result.Items) != test.count {
			t.Fatalf("source=%s want=%d got=%+v err=%v", test.source, test.count, result, err)
		}
	}
	admin, err := store.ListPositionCandidates(tenantID, PositionCandidateQuery{SourceType: "browser_extension:wuyou", PageSize: 100})
	if err != nil || admin.Total != 1 || admin.Items[0].UserEmail != member {
		t.Fatal(admin, err)
	}
}
