// 本文件验证两轮筛选状态、真实空值、幂等版本、固定分页及正常登录与本人岗位的HTTP权限边界。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// screeningTestPointer 为测试创建独立可空值，确保零值也能被保留。
func screeningTestPointer[T any](value T) *T { return &value }

// screeningTestSnapshot 返回有真实零分和零耗时、第二轮尚未执行的最小完整快照。
func screeningTestSnapshot(index int) ScreeningSnapshot {
	at := time.Date(2026, 10, 26, 6, 0, index, 0, time.UTC)
	return ScreeningSnapshot{AttemptID: fmt.Sprintf("00000000-0000-4000-8000-%012d", index+1), TaskID: "local-task", PlatformID: "boss", CandidateKey: fmt.Sprintf("candidate-%d", index), CandidateName: "合成人选", OccurredAt: at, Revision: 1, Completed: true, First: ScreeningRoundSnapshot{Kind: "ai", Status: "rejected", Accepted: screeningTestPointer(false), Score: screeningTestPointer(float64(0)), Threshold: screeningTestPointer(float64(60)), Reason: "真实零分", StartedAt: &at, FinishedAt: &at, DurationMS: screeningTestPointer(int64(0))}, Second: ScreeningRoundSnapshot{Kind: "none", Status: "not_executed", Reason: "第一轮未通过，未执行详情评分"}}
}

// TestScreeningSnapshotValidation 检查通过、关键词、恢复取消及异常字段，避免未执行被写成零分。
func TestScreeningSnapshotValidation(t *testing.T) {
	for _, item := range []struct {
		name   string
		change func(*ScreeningSnapshot)
		valid  bool
	}{
		{"真实零分零耗时", func(s *ScreeningSnapshot) {}, true},
		{"关键词通过", func(s *ScreeningSnapshot) {
			s.First.Kind = "keyword"
			s.First.Score = nil
			s.First.Threshold = nil
			s.First.Status = "passed"
			s.First.Accepted = screeningTestPointer(true)
		}, true},
		{"恢复取消未知结束", func(s *ScreeningSnapshot) {
			s.First.Status = "cancelled"
			s.First.Score = nil
			s.First.Threshold = nil
			s.First.Accepted = nil
			s.First.FinishedAt = nil
			s.First.DurationMS = nil
			s.First.Reason = "恢复时发现上次任务被中断"
		}, true},
		{"平台跳过", func(s *ScreeningSnapshot) { s.First = s.Second }, true},
		{"运行中", func(s *ScreeningSnapshot) {
			s.Completed = false
			s.First.Status = "running"
			s.First.Score = nil
			s.First.Accepted = nil
			s.First.FinishedAt = nil
			s.First.DurationMS = nil
		}, true},
		{"结束但还在运行", func(s *ScreeningSnapshot) {
			s.First.Status = "running"
			s.First.Score = nil
			s.First.Accepted = nil
			s.First.FinishedAt = nil
			s.First.DurationMS = nil
		}, false},
		{"未执行伪造零分", func(s *ScreeningSnapshot) { s.Second.Score = screeningTestPointer(float64(0)) }, false},
		{"关键词伪造分数", func(s *ScreeningSnapshot) { s.First.Kind = "keyword" }, false},
		{"完成AI缺分数", func(s *ScreeningSnapshot) { s.First.Score = nil }, false},
		{"完成缺时间", func(s *ScreeningSnapshot) { s.First.FinishedAt = nil; s.First.DurationMS = nil }, false},
		{"数值不有限", func(s *ScreeningSnapshot) { s.First.Score = screeningTestPointer(math.NaN()) }, false},
		{"超上限", func(s *ScreeningSnapshot) { s.First.Score = screeningTestPointer(float64(101)) }, false},
		{"结果矛盾", func(s *ScreeningSnapshot) { s.First.Accepted = screeningTestPointer(true) }, false},
		{"结束早于开始", func(s *ScreeningSnapshot) { s.First.FinishedAt = screeningTestPointer(s.OccurredAt.Add(-time.Second)) }, false},
		{"耗时缺完成时间", func(s *ScreeningSnapshot) { s.First.FinishedAt = nil }, false},
		{"负耗时", func(s *ScreeningSnapshot) { s.First.DurationMS = screeningTestPointer(int64(-1)) }, false},
		{"原因超限", func(s *ScreeningSnapshot) { s.First.Reason = strings.Repeat("字", 16001) }, false},
		{"版本缺失", func(s *ScreeningSnapshot) { s.Revision = 0 }, false},
		{"非法编号", func(s *ScreeningSnapshot) { s.AttemptID = "not-uuid" }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			snapshot := screeningTestSnapshot(0)
			item.change(&snapshot)
			err := validateScreeningSnapshot(&snapshot)
			if (err == nil) != item.valid {
				t.Fatalf("valid=%t err=%v", item.valid, err)
			}
		})
	}
}

// screeningStoreContract 在内存与真实PostgreSQL共同验证版本幂等、迟到、冲突、并发和四类范围查询。
func screeningStoreContract(t *testing.T, store ScreeningRecordStore, scope ScreeningScope) {
	t.Helper()
	ctx := context.Background()
	first := screeningTestSnapshot(0)
	saved, err := store.Save(ctx, scope, first)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := store.Save(ctx, scope, first)
	if err != nil || copy.ID != saved.ID || !copy.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatalf("duplicate=%+v err=%v", copy, err)
	}
	newer := recruitmentClone(first)
	newer.Revision = 3
	newer.TerminationReason = "新版本已结束"
	if _, err = store.Save(ctx, scope, newer); err != nil {
		t.Fatal(err)
	}
	late := recruitmentClone(first)
	late.Revision = 2
	late.TerminationReason = "迟到的中间版本"
	ack, err := store.Save(ctx, scope, late)
	if err != nil || ack.Revision != 3 || ack.TerminationReason != newer.TerminationReason {
		t.Fatalf("late ack=%+v err=%v", ack, err)
	}
	late.TerminationReason = "相同旧版本被改动"
	if _, err = store.Save(ctx, scope, late); !errors.Is(err, ErrScreeningRecordConflict) {
		t.Fatalf("historical conflict=%v", err)
	}
	for _, mutate := range []func(*ScreeningSnapshot){func(s *ScreeningSnapshot) { s.TaskID = "other-task" }, func(s *ScreeningSnapshot) { s.PlatformID = "liepin" }, func(s *ScreeningSnapshot) { s.CandidateKey = "other-candidate" }, func(s *ScreeningSnapshot) { s.OccurredAt = s.OccurredAt.Add(time.Second) }} {
		forged := recruitmentClone(newer)
		forged.Revision = 4
		mutate(&forged)
		if _, err = store.Save(ctx, scope, forged); !errors.Is(err, ErrScreeningRecordConflict) {
			t.Fatalf("identity replacement=%v", err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 30)
	for revision := int64(4); revision <= 25; revision++ {
		wg.Add(1)
		go func(revision int64) {
			defer wg.Done()
			item := recruitmentClone(first)
			item.Revision = revision
			item.TerminationReason = fmt.Sprintf("revision-%d", revision)
			_, err := store.Save(ctx, scope, item)
			errs <- err
		}(revision)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := store.List(ctx, scope, ScreeningRecordQuery{})
	if err != nil || list.Total != 1 || list.Records[0].Revision != 25 {
		t.Fatalf("concurrent result=%+v err=%v", list, err)
	}
	// 时间时区不同但同一瞬间的完整重传应保持幂等。
	zoneCopy := recruitmentClone(list.Records[0].ScreeningSnapshot)
	zone := time.FixedZone("CST", 8*3600)
	zoneCopy.OccurredAt = zoneCopy.OccurredAt.In(zone)
	zoneCopy.First.StartedAt = screeningTestPointer(zoneCopy.First.StartedAt.In(zone))
	zoneCopy.First.FinishedAt = screeningTestPointer(zoneCopy.First.FinishedAt.In(zone))
	if _, err = store.Save(ctx, scope, zoneCopy); err != nil {
		t.Fatalf("timezone duplicate=%v", err)
	}
	for index := 1; index <= 25; index++ {
		item := screeningTestSnapshot(index)
		item.First.Score = screeningTestPointer(float64(index))
		item.Second = recruitmentClone(item.First)
		item.Second.Score = screeningTestPointer(float64(100 - index))
		if index == 25 {
			item.First.Kind = "keyword"
			item.First.Score = nil
			item.First.Threshold = nil
			item.Second = ScreeningRoundSnapshot{Kind: "none", Status: "not_executed", Reason: "关键词拒绝"}
		}
		if _, err = store.Save(ctx, scope, item); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := store.List(ctx, scope, ScreeningRecordQuery{Page: 1})
	if err != nil || page1.Total != 26 || len(page1.Records) != 20 || page1.PageSize != 20 {
		t.Fatalf("page1=%+v err=%v", page1, err)
	}
	page2, err := store.List(ctx, scope, ScreeningRecordQuery{Page: 2})
	if err != nil || page2.Total != 26 || len(page2.Records) != 6 {
		t.Fatalf("page2=%+v err=%v", page2, err)
	}
	all := append(page1.Records, page2.Records...)
	seen := map[string]bool{}
	for index, item := range all {
		if seen[item.ID] {
			t.Fatal("duplicate page row")
		}
		seen[item.ID] = true
		if index > 0 && (all[index-1].OccurredAt.Before(item.OccurredAt) || all[index-1].OccurredAt.Equal(item.OccurredAt) && all[index-1].AttemptID < item.AttemptID) {
			t.Fatal("unstable descending order")
		}
	}
	from := screeningTestSnapshot(5).OccurredAt
	to := screeningTestSnapshot(10).OccurredAt
	for _, item := range []struct {
		name  string
		query ScreeningRecordQuery
		want  int
	}{
		{"第一轮零分闭区间", ScreeningRecordQuery{FirstScoreMin: screeningTestPointer(float64(0)), FirstScoreMax: screeningTestPointer(float64(0))}, 1},
		{"第一轮分数上下限", ScreeningRecordQuery{FirstScoreMin: screeningTestPointer(float64(5)), FirstScoreMax: screeningTestPointer(float64(10))}, 6},
		{"第二轮分数上下限", ScreeningRecordQuery{SecondScoreMin: screeningTestPointer(float64(90)), SecondScoreMax: screeningTestPointer(float64(95))}, 6},
		{"第一轮开始左闭右开", ScreeningRecordQuery{FirstStartedFrom: &from, FirstStartedTo: &to}, 5},
		{"第二轮开始左闭右开", ScreeningRecordQuery{SecondStartedFrom: &from, SecondStartedTo: &to}, 5},
		{"四组条件同时", ScreeningRecordQuery{FirstScoreMin: screeningTestPointer(float64(6)), SecondScoreMax: screeningTestPointer(float64(93)), FirstStartedFrom: &from, SecondStartedTo: &to}, 3},
		{"单边上界不包含null", ScreeningRecordQuery{FirstScoreMax: screeningTestPointer(float64(100))}, 25},
		{"第二轮单边不包含null", ScreeningRecordQuery{SecondScoreMin: screeningTestPointer(float64(0))}, 24},
	} {
		result, err := store.List(ctx, scope, item.query)
		if err != nil || result.Total != item.want {
			t.Errorf("%s total=%d want=%d err=%v", item.name, result.Total, item.want, err)
		}
	}
}

// TestScreeningMemoryStoreContract 以独立内存存储运行所有快照与分页合同。
func TestScreeningMemoryStoreContract(t *testing.T) {
	screeningStoreContract(t, NewMemoryScreeningRecordStore(), ScreeningScope{TenantID: "tenant", PositionID: "position", UserEmail: "owner@example.com"})
}

// screeningHTTPFixture 构造不发邮件、不连数据库的岗位与普通成员登录环境。
func screeningHTTPFixture(t *testing.T) (*ScreeningRecordService, *MemoryTenantStore) {
	t.Helper()
	auth := NewMemoryAuthStore()
	tenants := NewMemoryTenantStore()
	tenant, _ := tenants.GetOrCreateTenant("owner@example.com")
	tenants.members[tenant.ID] = append(tenants.members[tenant.ID], TenantMember{Email: "member@example.com", Role: "member", Status: "active"})
	_, _ = tenants.GetOrCreateTenant("outsider@example.com")
	positions := NewMemoryPositionStore()
	positions.positions["own"] = Position{ID: "own", UserEmail: "owner@example.com"}
	positions.positions["member"] = Position{ID: "member", UserEmail: "member@example.com"}
	for token, session := range map[string]Session{"owner": {Email: "owner@example.com"}, "member": {Email: "member@example.com"}, "outsider": {Email: "outsider@example.com"}, "plugin": {Email: "owner@example.com", Kind: extensionSessionKind}} {
		if err := auth.SaveSession(token, session, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	return NewScreeningRecordService(&AuthService{store: auth}, positions, tenants, NewMemoryScreeningRecordStore()), tenants
}

// TestScreeningHTTPPermissionAndContract 通过真实服务路由验证鉴权、严格JSON、空值和固定20条响应。
func TestScreeningHTTPPermissionAndContract(t *testing.T) {
	service, _ := screeningHTTPFixture(t)
	handler := http.HandlerFunc(service.Collection)
	path := "/api/positions/own/screening-records"
	for _, item := range []struct {
		token, method, path string
		want                int
	}{{"", http.MethodGet, path, 401}, {"member", http.MethodGet, path, 404}, {"outsider", http.MethodPost, path, 404}, {"plugin", http.MethodPost, path, 403}, {"owner", http.MethodGet, "/api/positions/member/screening-records", 404}} {
		status, _ := candidateTestRequest(t, handler, item.token, item.method, item.path, screeningTestSnapshot(0))
		if status != item.want {
			t.Fatalf("permission %+v status=%d", item, status)
		}
	}
	status, ack := candidateTestRequest(t, handler, "owner", http.MethodPost, path, screeningTestSnapshot(0))
	if status != 200 || ack["revision"] != float64(1) || ack["id"] == "" {
		t.Fatalf("post=%d %v", status, ack)
	}
	status, response := candidateTestRequest(t, handler, "owner", http.MethodGet, path+"?first_score_min=0&first_score_max=0&page_size=20", nil)
	if status != 200 || response["total"] != float64(1) || response["page_size"] != float64(20) {
		t.Fatalf("list=%d %v", status, response)
	}
	item := response["records"].([]any)[0].(map[string]any)
	first := item["first"].(map[string]any)
	second := item["second"].(map[string]any)
	if first["score"] != float64(0) || first["duration_ms"] != float64(0) || second["score"] != nil || second["duration_ms"] != nil {
		t.Fatalf("null/zero lost=%v", item)
	}
	if _, ok := item["tenant_id"]; ok {
		t.Fatal("tenant leaked")
	}
	if _, ok := item["owner_user_id"]; ok {
		t.Fatal("owner leaked")
	}
	conflict := screeningTestSnapshot(0)
	conflict.CandidateName = "改了同版本内容"
	status, _ = candidateTestRequest(t, handler, "owner", http.MethodPost, path, conflict)
	if status != 409 {
		t.Fatalf("conflict status=%d", status)
	}
	for _, query := range []string{"page_size=21", "page=0", "page=-1", "first_score_min=NaN", "first_score_max=101", "second_score_min=80&second_score_max=20", "first_started_from=bad", "page=1&page=2", "tenant_id=other"} {
		status, _ := candidateTestRequest(t, handler, "owner", http.MethodGet, path+"?"+query, nil)
		if status != 400 {
			t.Errorf("query=%s status=%d", query, status)
		}
	}
	raw, _ := json.Marshal(screeningTestSnapshot(0))
	for _, body := range []string{strings.TrimSuffix(string(raw), "}") + `,"tenant_id":"spoof"}`, string(raw) + string(raw), `{"revision":1,"revision":2}`, strings.Repeat("x", (256<<10)+1)} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer owner")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request)
		if rec.Code != 400 && rec.Code != 413 {
			t.Fatalf("bad body status=%d bodylen=%d", rec.Code, len(body))
		}
	}
	// 服务装配入口只需真实server positionRoute，避免测试绕过新路径分发。
	server := &Server{screeningRecords: service}
	request := httptest.NewRequest(http.MethodGet, path, bytes.NewReader(nil))
	request.Header.Set("Authorization", "Bearer owner")
	rec := httptest.NewRecorder()
	server.positionRoute(rec, request)
	if rec.Code != 200 {
		t.Fatalf("route status=%d", rec.Code)
	}
}
