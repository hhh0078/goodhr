// Package httpapi 测试简历库筛选、岗位维度第二次评分和排序 SQL。
package httpapi

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestNormalizeCandidateListOptions 验证空值保持旧行为且合法筛选值可被识别。
func TestNormalizeCandidateListOptions(t *testing.T) {
	for _, value := range []string{"boss", " ZHAOPIN ", "hliepin", "LIEPIN"} {
		if got := normalizeCandidatePlatformID(value); got != strings.ToLower(strings.TrimSpace(value)) {
			t.Fatalf("平台筛选归一失败，value=%q got=%q", value, got)
		}
	}
	for _, value := range []string{"", "all", "unknown"} {
		if got := normalizeCandidatePlatformID(value); got != "" {
			t.Fatalf("非法平台应回退为全部，value=%q got=%q", value, got)
		}
	}
	if got := normalizeCandidatePhoneStatus(""); got != "" {
		t.Fatalf("空手机号筛选不应改变旧行为，got=%q", got)
	}
	if got := normalizeCandidatePhoneStatus(" HAS "); got != "has" {
		t.Fatalf("手机号筛选归一失败，got=%q", got)
	}
	if got := normalizeCandidateConditionStatus("all_matched"); got != "all_matched" {
		t.Fatalf("条件状态归一失败，got=%q", got)
	}
	if got := normalizeCandidateConditionStatus("all"); got != "" {
		t.Fatalf("全部条件状态应回退为不筛选，got=%q", got)
	}
	if got := normalizeCandidateSort("second_score_desc"); got != "second_score_desc" {
		t.Fatalf("第二次评分排序归一失败，got=%q", got)
	}
}

// TestCandidateConditionStatusClause 验证前端条件筛选和评估状态集合的对应关系。
func TestCandidateConditionStatusClause(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"all_matched", "review.status IN ('qualified','generating','recommended','failed')"},
		{"pending", "review.status IN ('collecting','pending')"},
		{"unmatched", "review.status = 'unmatched'"},
		{"untracked", "NOT EXISTS"},
	}
	for _, test := range tests {
		clause := candidateConditionStatusClause(test.status, "$3", "$4")
		if !strings.Contains(clause, test.want) || !strings.Contains(clause, "review.position_id::text = $3") {
			t.Fatalf("条件状态 %s 的岗位范围不正确：%s", test.status, clause)
		}
		for _, fragment := range []string{
			"review_engagement.candidate_id = review.candidate_id",
			"review_engagement.position_id = review.position_id",
			"LOWER(review_engagement.platform_id) = $4",
		} {
			if !strings.Contains(clause, fragment) {
				t.Fatalf("条件状态 %s 没有绑定同平台触达 %q：%s", test.status, fragment, clause)
			}
		}
	}
}

// TestCandidateConditionStatusFollowsPlatformWithoutPosition 验证只筛平台时岗位评估也必须属于该平台触达。
func TestCandidateConditionStatusFollowsPlatformWithoutPosition(t *testing.T) {
	where, args := buildCandidateWhere("tenant-1", PositionCandidateQuery{
		PlatformID:      "zhaopin",
		ConditionStatus: "all_matched",
	})
	if !reflect.DeepEqual(args, []any{"tenant-1", "zhaopin"}) {
		t.Fatalf("平台条件筛选参数错误：%#v", args)
	}
	for _, fragment := range []string{
		"review_engagement.candidate_id = review.candidate_id",
		"review_engagement.position_id = review.position_id",
		"LOWER(review_engagement.platform_id) = $2",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("平台条件筛选没有绑定同平台触达 %q：%s", fragment, where)
		}
	}
}

// TestBuildCandidateWhereKeepsListAndCountScope 验证岗位、平台、手机号、状态和用户筛选共用稳定参数顺序。
func TestBuildCandidateWhereKeepsListAndCountScope(t *testing.T) {
	query := PositionCandidateQuery{
		PositionID:      "position-1",
		PlatformID:      "liepin",
		Keyword:         "候选人",
		UserEmail:       "hr@example.com",
		PhoneStatus:     "has",
		ConditionStatus: "all_matched",
	}
	where, args := buildCandidateWhere("tenant-1", query)
	wantArgs := []any{"tenant-1", "hr@example.com", "position-1", "liepin", "%候选人%"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("筛选参数顺序错误：got=%#v want=%#v", args, wantArgs)
	}
	for _, fragment := range []string{
		"u.email = $2",
		"ce_filter.position_id::text = $3",
		"LOWER(ce_filter.platform_id) = $4",
		"BTRIM(cp.normalized_phone)",
		"review.position_id::text = $3",
		"cp.candidate_name ILIKE $5",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("筛选 SQL 缺少 %q：%s", fragment, where)
		}
	}
	if strings.Contains(where, "cp.source_platform_id") || strings.Contains(where, "identity_filter") {
		t.Fatalf("岗位和平台必须由同一个触达命中，不能使用档案来源或独立平台身份：%s", where)
	}
	if scope := candidateEngagementScope(query); !strings.Contains(scope, "ce2.position_id::text = $3") {
		t.Fatalf("最新触达没有限定当前岗位：%s", scope)
	} else if !strings.Contains(scope, "LOWER(ce2.platform_id) = $4") {
		t.Fatalf("最新触达没有限定当前平台：%s", scope)
	} else if !strings.Contains(scope, "review_scope.position_id = ce2.position_id") {
		t.Fatalf("最新触达没有限定满足筛选状态的岗位评估：%s", scope)
	}
	if countSQL := candidateCountSQL(where); !strings.Contains(countSQL, "LEFT JOIN users u") {
		t.Fatalf("非管理员总数查询缺少用户范围：%s", countSQL)
	}
}

// TestCandidatePlatformFilterUsesEngagementAndIdentity 验证仅按平台筛选时兼容跨平台合并身份。
func TestCandidatePlatformFilterUsesEngagementAndIdentity(t *testing.T) {
	query := PositionCandidateQuery{PlatformID: "zhaopin"}
	where, args := buildCandidateWhere("tenant-1", query)
	if !reflect.DeepEqual(args, []any{"tenant-1", "zhaopin"}) {
		t.Fatalf("平台筛选参数错误：%#v", args)
	}
	for _, fragment := range []string{
		"candidate_engagements ce_filter",
		"candidate_platform_identities identity_filter",
		"LOWER(ce_filter.platform_id) = $2",
		"LOWER(identity_filter.platform_id) = $2",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("跨平台筛选缺少 %q：%s", fragment, where)
		}
	}
	if strings.Contains(where, "cp.source_platform_id") {
		t.Fatalf("跨平台筛选不能依赖候选人档案首次来源：%s", where)
	}
	if scope := candidateEngagementScope(query); !strings.Contains(scope, "LOWER(ce2.platform_id) = $2") {
		t.Fatalf("返回触达没有同步限定平台：%s", scope)
	}
	if scope := candidatePlatformIdentityScope(query); !strings.Contains(scope, "LOWER(identity.platform_id) = $2") {
		t.Fatalf("返回平台身份没有同步限定平台：%s", scope)
	}
}

// TestCandidateSecondScoreUsesLatestPositionEngagement 验证第二次评分优先读取当前岗位最新触达的最新事件。
func TestCandidateSecondScoreUsesLatestPositionEngagement(t *testing.T) {
	selectSQL := candidateSelectSQL(
		"WHERE cp.tenant_id = $1",
		"AND ce2.position_id::text = $2",
		"AND LOWER(identity.platform_id) = $3",
	)
	for _, fragment := range []string{
		"ORDER BY ce2.created_at DESC, ce2.id DESC",
		"event.engagement_id = latest_engagement.id",
		"event.event_type = 'greet_analysis'",
		"ORDER BY event.created_at DESC, event.id DESC",
		"latest_greet_analysis.score",
		"LOWER(identity.platform_id) = $3",
	} {
		if !strings.Contains(selectSQL, fragment) {
			t.Fatalf("岗位第二次评分 SQL 缺少 %q", fragment)
		}
	}
	if strings.Contains(selectSQL, "cp.ai_greet_score") || strings.Contains(selectSQL, "cp.ai_greet_reason") {
		t.Fatalf("列表第二次评分禁止回退候选人档案旧分数：%s", selectSQL)
	}
	orderSQL := candidateOrderSQL("second_score_desc")
	if !strings.Contains(orderSQL, "DESC NULLS LAST") {
		t.Fatalf("第二次评分排序必须空值靠后：%s", orderSQL)
	}
	if !strings.Contains(orderSQL, "cp.id ASC") {
		t.Fatalf("第二次评分排序必须用候选人编号稳定分页：%s", orderSQL)
	}
	if legacyOrder := candidateOrderSQL(""); strings.Contains(legacyOrder, "latest_greet_analysis.score") {
		t.Fatalf("空排序参数不应改变旧接口行为：%s", legacyOrder)
	} else if !strings.Contains(legacyOrder, "cp.id ASC") {
		t.Fatalf("最近入库排序必须用候选人编号稳定分页：%s", legacyOrder)
	}
	if strings.Contains(orderSQL, "cp.ai_greet_score") {
		t.Fatalf("第二次评分排序禁止回退候选人档案旧分数：%s", orderSQL)
	}
}

// TestMemoryCandidatePlatformFilterRequiresEngagement 验证内存平台筛选不回退候选人首次来源字段。
func TestMemoryCandidatePlatformFilterRequiresEngagement(t *testing.T) {
	store := NewMemoryCandidateStore()
	createdAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	store.profiles["candidate-source-only"] = PositionCandidate{
		ID: "candidate-source-only", PlatformID: "zhaopin", CandidateName: "只有来源平台",
	}
	store.profileTenantIDs["candidate-source-only"] = "tenant-1"
	store.profiles["candidate-engaged"] = PositionCandidate{
		ID: "candidate-engaged", PlatformID: "boss", CandidateName: "有智联触达",
	}
	store.profileTenantIDs["candidate-engaged"] = "tenant-1"
	store.engagements["engagement-a"] = CandidateEngagement{
		ID: "engagement-a", CandidateID: "candidate-engaged", PlatformID: "zhaopin", CreatedAt: createdAt,
	}
	store.engagements["engagement-b"] = CandidateEngagement{
		ID: "engagement-b", CandidateID: "candidate-engaged", PlatformID: "zhaopin", CreatedAt: createdAt,
	}
	lowScore, highScore := 61.0, 88.0
	store.events["candidate-engaged"] = []CandidateEvent{
		{ID: "event-a", EngagementID: "engagement-b", EventType: "greet_analysis", Score: &lowScore, CreatedAt: createdAt},
		{ID: "event-b", EngagementID: "engagement-b", EventType: "greet_analysis", Score: &highScore, CreatedAt: createdAt},
	}

	for attempt := 0; attempt < 10; attempt++ {
		result, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{PlatformID: "zhaopin"})
		if err != nil {
			t.Fatalf("第 %d 次平台筛选失败：%v", attempt+1, err)
		}
		if result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != "candidate-engaged" {
			t.Fatalf("首次来源不能代替真实触达：%+v", result.Items)
		}
		if result.Items[0].EngagementID != "engagement-b" || result.Items[0].AIGreetScore == nil || *result.Items[0].AIGreetScore != highScore {
			t.Fatalf("同时间触达或分析事件选择不稳定：%+v", result.Items[0])
		}
	}
}

// TestMemoryCandidateFiltersUseSameEngagement 验证内存实现按同一个岗位平台触达取最新第二次评分。
func TestMemoryCandidateFiltersUseSameEngagement(t *testing.T) {
	store := NewMemoryCandidateStore()
	oldProfileScore := 99.0
	store.profiles["candidate-1"] = PositionCandidate{
		ID: "candidate-1", PlatformID: "zhaopin", CandidateName: "候选人一",
		Phone: "13800000000", AIGreetScore: &oldProfileScore,
	}
	store.profileTenantIDs["candidate-1"] = "tenant-1"
	store.engagements["boss-position-1"] = CandidateEngagement{
		ID: "boss-position-1", CandidateID: "candidate-1", PositionID: "position-1",
		PlatformID: "boss", CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}
	store.engagements["zhaopin-position-2"] = CandidateEngagement{
		ID: "zhaopin-position-2", CandidateID: "candidate-1", PositionID: "position-2",
		PlatformID: "zhaopin", CreatedAt: time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC),
	}
	result, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{
		PositionID: "position-1", PlatformID: "zhaopin", PhoneStatus: "has",
	})
	if err != nil {
		t.Fatalf("内存筛选失败：%v", err)
	}
	if result.Total != 0 {
		t.Fatalf("岗位和平台分属两个触达时不应命中：%+v", result.Items)
	}

	engagement := store.engagements["zhaopin-position-2"]
	engagement.PositionID = "position-1"
	store.engagements[engagement.ID] = engagement
	olderScore, latestScore, otherEngagementScore := 70.0, 82.0, 100.0
	store.events["candidate-1"] = []CandidateEvent{
		{EngagementID: engagement.ID, EventType: "greet_analysis", Score: &olderScore, CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)},
		{EngagementID: engagement.ID, EventType: "greet_analysis", Score: &latestScore, CreatedAt: time.Date(2026, 8, 1, 13, 0, 0, 0, time.UTC)},
		{EngagementID: "boss-position-1", EventType: "greet_analysis", Score: &otherEngagementScore, CreatedAt: time.Date(2026, 8, 1, 14, 0, 0, 0, time.UTC)},
	}
	result, err = store.ListPositionCandidates("tenant-1", PositionCandidateQuery{
		PositionID: "position-1", PlatformID: "zhaopin", PhoneStatus: "has", Sort: "second_score_desc",
	})
	if err != nil || result.Total != 1 {
		t.Fatalf("同岗位平台触达应命中：total=%d err=%v", result.Total, err)
	}
	if result.Items[0].AIGreetScore == nil || *result.Items[0].AIGreetScore != latestScore {
		t.Fatalf("应只使用目标触达最新第二次评分：%+v", result.Items[0].AIGreetScore)
	}

	delete(store.events, "candidate-1")
	result, err = store.ListPositionCandidates("tenant-1", PositionCandidateQuery{
		PositionID: "position-1", PlatformID: "zhaopin", PhoneStatus: "has", Sort: "second_score_desc",
	})
	if err != nil || result.Total != 1 || result.Items[0].AIGreetScore != nil {
		t.Fatalf("没有目标触达评分时禁止回退档案旧分数：items=%+v err=%v", result.Items, err)
	}
}

// TestMemoryCandidateConditionStatusTreatsMissingReviewAsUntracked 验证内存回退不会把没有评估记录的候选人冒充为全部满足。
func TestMemoryCandidateConditionStatusTreatsMissingReviewAsUntracked(t *testing.T) {
	store := NewMemoryCandidateStore()
	store.profiles["candidate-1"] = PositionCandidate{ID: "candidate-1", CandidateName: "候选人一"}
	store.profileTenantIDs["candidate-1"] = "tenant-1"

	matched, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{ConditionStatus: "all_matched"})
	if err != nil {
		t.Fatalf("内存全部满足筛选失败：%v", err)
	}
	if matched.Total != 0 {
		t.Fatalf("没有评估记录的候选人不应视为全部满足：%+v", matched.Items)
	}

	untracked, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{ConditionStatus: "untracked"})
	if err != nil {
		t.Fatalf("内存未跟踪筛选失败：%v", err)
	}
	if untracked.Total != 1 || len(untracked.Items) != 1 || untracked.Items[0].ID != "candidate-1" {
		t.Fatalf("没有评估记录的候选人应视为未跟踪：%+v", untracked.Items)
	}
}

// TestMemoryCandidateDefaultSort 验证内存回退默认按最近入库稳定排序，不受 Go map 遍历顺序影响。
func TestMemoryCandidateDefaultSort(t *testing.T) {
	store := NewMemoryCandidateStore()
	latest := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)
	store.profiles = map[string]PositionCandidate{
		"candidate-b":   {ID: "candidate-b", CandidateName: "候选人乙", CreatedAt: latest},
		"candidate-old": {ID: "candidate-old", CandidateName: "较早候选人", CreatedAt: latest.Add(-time.Hour)},
		"candidate-a":   {ID: "candidate-a", CandidateName: "候选人甲", CreatedAt: latest},
	}
	store.profileTenantIDs = map[string]string{
		"candidate-a": "tenant-1", "candidate-b": "tenant-1", "candidate-old": "tenant-1",
	}
	want := []string{"candidate-a", "candidate-b", "candidate-old"}

	for _, sortValue := range []string{"", "unsupported"} {
		for attempt := 0; attempt < 10; attempt++ {
			result, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{Sort: sortValue, PageSize: 10})
			if err != nil {
				t.Fatalf("排序 %q 查询失败：%v", sortValue, err)
			}
			got := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				got = append(got, item.ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("排序 %q 第 %d 次结果不稳定：got=%v want=%v", sortValue, attempt+1, got, want)
			}
		}
	}

	for page, wantID := range want {
		result, err := store.ListPositionCandidates("tenant-1", PositionCandidateQuery{Page: page + 1, PageSize: 1})
		if err != nil || len(result.Items) != 1 || result.Items[0].ID != wantID {
			t.Fatalf("第 %d 页结果错误：items=%v err=%v", page+1, result.Items, err)
		}
	}
}
