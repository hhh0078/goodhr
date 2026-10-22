// 本文件验证真实简历计数、异步回执、按版本续算、重启及付费不明保护，使用合成模型与搜索副本。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// vectorTestSearch 保存测试调用量并允许注入索引失败，不连接搜索服务器。
type vectorTestSearch struct {
	ready bool
	calls atomic.Int32
	fail  atomic.Bool
}

// Ready 返回测试用配置状态。
func (s *vectorTestSearch) Ready() bool { return s.ready }

// Index 验证所有片段均有向量，再按测试开关返回索引结果。
func (s *vectorTestSearch) Index(_ context.Context, item resumeVectorRecord) error {
	s.calls.Add(1)
	for _, chunk := range item.Chunks {
		if len(chunk.Vector) != 1024 {
			return errors.New("索引缺少完整向量")
		}
	}
	if s.fail.Load() {
		return errors.New("合成搜索故障")
	}
	return nil
}

// Search 只满足统一搜索合同，本组测试不验证检索排序。
func (s *vectorTestSearch) Search(context.Context, CandidateLibraryScope, string, []float64) ([]resumeSearchHit, error) {
	return nil, nil
}

// resumeVectorsFixture 创建真实范围的内存候选人、会话与钱包，返回一份可访问简历和模型调用次数。
func resumeVectorsFixture(t *testing.T) (*ResumeVectorService, PositionCandidate, *atomic.Int32, *vectorTestSearch) {
	t.Helper()
	wallet, _ := embeddingFixture(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("测试不应调用网络") }))
	tenantStore := NewMemoryTenantStore()
	tenant, err := tenantStore.GetOrCreateTenant("embedding@example.com")
	if err != nil {
		t.Fatal(err)
	}
	auth := &AuthService{store: NewMemoryAuthStore()}
	if err = auth.store.SaveSession("vector-test-session", Session{Email: "embedding@example.com"}, time.Hour); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryCandidateStore()
	candidate, err := store.SaveCandidateProfile(CandidateProfileInput{TenantID: tenant.ID, UserEmail: "embedding@example.com", CandidateName: "测试候选人", ExpectedPosition: "Go工程师", RawText: "负责支付服务与数据库优化", WorkExperiences: []CandidateWorkExperience{{CompanyName: "示例公司", PositionName: "工程师", Content: "主导Go支付服务", StartYM: "2020-01", EndYM: "2025-01"}}})
	if err != nil {
		t.Fatal(err)
	}
	candidates := NewCandidateService(auth, store, tenantStore)
	search := &vectorTestSearch{ready: true}
	service := NewResumeVectorService(candidates, wallet, nil, search)
	calls := &atomic.Int32{}
	service.embed = func(_ context.Context, _ string, texts []string) ([][]float64, error) {
		calls.Add(1)
		vectors := make([][]float64, len(texts))
		for i := range vectors {
			vectors[i] = make([]float64, 1024)
			vectors[i][0] = 1
		}
		return vectors, nil
	}
	return service, candidate, calls, search
}

// vectorTaskForTest 创建可恢复任务，明确分离HTTP请求和任务上下文。
func vectorTaskForTest(candidate PositionCandidate) resumeVectorTask {
	now := time.Now()
	return resumeVectorTask{ID: vectorTaskID(), Owner: candidate.UserEmail, TenantID: candidate.TenantID, Status: "queued", Stage: "ready", CreatedAt: now, ExpiresAt: now.Add(resumeVectorTaskTTL)}
}

// vectorStatusRequest 用本人登录态读取或启动后台向量任务。
func vectorStatusRequest(service *ResumeVectorService, method string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/api/resume-library/vectors", nil)
	request.Header.Set("Authorization", "Bearer vector-test-session")
	response := httptest.NewRecorder()
	service.Route(response, request)
	return response
}

// waitVectorTask 等待测试任务终态，超过三秒即明确失败，避免无限轮询。
func waitVectorTask(t *testing.T, service *ResumeVectorService) resumeVectorTask {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := service.tasks.Get(context.Background(), "embedding@example.com")
		if err == nil && !vectorTaskActive(task.Status) {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("后台向量任务没有及时结束")
	return resumeVectorTask{}
}

// TestResumeVectorsAsyncAndCounts 验证立即202、重复提交复用任务、HTTP断开后继续及准确真实版本计数。
func TestResumeVectorsAsyncAndCounts(t *testing.T) {
	service, candidate, calls, _ := resumeVectorsFixture(t)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	base := service.embed
	service.embed = func(ctx context.Context, email string, texts []string) ([][]float64, error) {
		close(entered)
		<-release
		vectors, err := base(ctx, email, texts)
		close(finished)
		return vectors, err
	}
	response := vectorStatusRequest(service, http.MethodGet)
	var status struct {
		Total, Indexed, Pending int
		Eligible                bool
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.Total != 1 || status.Indexed != 0 || status.Pending != 1 || !status.Eligible {
		t.Fatalf("数量状态=%s", response.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/api/resume-library/vectors", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer vector-test-session")
	response = httptest.NewRecorder()
	service.Route(response, request)
	cancel()
	if response.Code != 202 {
		t.Fatalf("没有立即收到任务=%d %s", response.Code, response.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("后台任务没有启动")
	}
	first, _ := service.tasks.Get(context.Background(), candidate.UserEmail)
	response = vectorStatusRequest(service, http.MethodPost)
	var receipt struct {
		ID string `json:"task_id"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &receipt)
	if response.Code != 202 || receipt.ID != first.ID || calls.Load() != 0 {
		t.Fatal("重复点击未复用活动任务")
	}
	close(release)
	<-finished
	if task := waitVectorTask(t, service); task.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("HTTP断开后任务失败=%+v", task)
	}
	response = vectorStatusRequest(service, http.MethodGet)
	_ = json.Unmarshal(response.Body.Bytes(), &status)
	if status.Total != 1 || status.Indexed != 1 || status.Pending != 0 {
		t.Fatalf("完成数=%s", response.Body.String())
	}
}

// TestResumeVectorsReuseAfterSearchFailure 验证搜索失败后重新启动只重试索引，不再次付费计算成功的分片。
func TestResumeVectorsReuseAfterSearchFailure(t *testing.T) {
	service, candidate, calls, search := resumeVectorsFixture(t)
	search.fail.Store(true)
	_, _ = service.tasks.Reserve(context.Background(), vectorTaskForTest(candidate))
	service.run(candidate.UserEmail)
	first, _ := service.tasks.Get(context.Background(), candidate.UserEmail)
	if first.Status != "failed" || calls.Load() != 1 {
		t.Fatalf("搜索失败状态=%+v", first)
	}
	search.fail.Store(false)
	_, _ = service.tasks.Reserve(context.Background(), vectorTaskForTest(candidate))
	service.run(candidate.UserEmail)
	last, _ := service.tasks.Get(context.Background(), candidate.UserEmail)
	if last.Status != "completed" || calls.Load() != 1 || search.calls.Load() != 2 {
		t.Fatalf("成功分片被重新计算=%+v calls=%d", last, calls.Load())
	}
}

// TestResumeVectorsRecoveryProtectsUncertainCalls 验证重启遇到无回执的付费调用时终止，已保存的完整批次则可直接继续。
func TestResumeVectorsRecoveryProtectsUncertainCalls(t *testing.T) {
	for _, saved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "saved"}[saved], func(t *testing.T) {
			service, candidate, calls, _ := resumeVectorsFixture(t)
			task := vectorTaskForTest(candidate)
			task.Stage = "embedding"
			task.CandidateID = candidate.ID
			chunks := buildResumeVectorChunks(candidate)
			for i := range chunks {
				task.BatchIDs = append(task.BatchIDs, chunks[i].ID)
				chunks[i].Vector = make([]float64, 1024)
				chunks[i].Vector[0] = 1
			}
			if saved {
				scope := CandidateLibraryScope{TenantID: candidate.TenantID, UserEmail: candidate.UserEmail, IsAdmin: true}
				err := service.store.Save(context.Background(), scope, resumeVectorRecord{CandidateID: candidate.ID, TenantID: candidate.TenantID, OwnerEmail: candidate.UserEmail, ResumeVersion: candidate.RowVersion, Profile: resumeVectorProfile, Chunks: chunks})
				if err != nil {
					t.Fatal(err)
				}
			}
			_, _ = service.tasks.Reserve(context.Background(), task)
			service.run(candidate.UserEmail)
			actual, _ := service.tasks.Get(context.Background(), candidate.UserEmail)
			want := "uncertain"
			if saved {
				want = "completed"
			}
			if actual.Status != want || calls.Load() != 0 {
				t.Fatalf("重启结果=%+v calls=%d", actual, calls.Load())
			}
		})
	}
}

// TestResumeVectorsRechecksVersionsAndEligibility 验证生成中简历变更不写旧索引，生产无Redis或外部AI配置不可启动。
func TestResumeVectorsRechecksVersionsAndEligibility(t *testing.T) {
	service, candidate, _, search := resumeVectorsFixture(t)
	base := service.embed
	service.embed = func(ctx context.Context, email string, texts []string) ([][]float64, error) {
		store := service.candidates.store.(*MemoryCandidateStore)
		store.mu.Lock()
		changed := store.profiles[candidate.ID]
		changed.RowVersion++
		store.profiles[candidate.ID] = changed
		store.mu.Unlock()
		return base(ctx, email, texts)
	}
	_, _ = service.tasks.Reserve(context.Background(), vectorTaskForTest(candidate))
	service.run(candidate.UserEmail)
	if search.calls.Load() != 0 {
		t.Fatal("简历变更后写入了旧索引")
	}
	scope := CandidateLibraryScope{TenantID: candidate.TenantID, UserEmail: candidate.UserEmail, IsAdmin: true}
	total, indexed, err := service.store.Counts(context.Background(), scope)
	if err != nil || total != 1 || indexed != 0 {
		t.Fatal("旧版本被当成当前完成")
	}
	service.persistent = true
	if eligible, _ := service.eligibility(scope); eligible {
		t.Fatal("无Redis生产环境允许付费")
	}
	service.persistent = false
	_, _ = service.wallet.aiConfigs.SaveUserConfig(candidate.UserEmail, AIConfig{BaseURL: "https://external.test/goodhr.example.com", APIKey: "embedding-user-key", Enabled: true})
	if eligible, _ := service.eligibility(scope); eligible {
		t.Fatal("外部AI配置允许生成内置向量")
	}
}

// TestResumeVectorChunksFullAndPrivate 验证长经历后半段完整覆盖、偏移稳定、敏感字段遮盖和片段ID可重复核验。
func TestResumeVectorChunksFullAndPrivate(t *testing.T) {
	body := strings.Repeat("负责常规业务开发。", 500) + "最后一段主导实时音视频系统，电话销售业务经验。"
	candidate := PositionCandidate{CandidateName: "张测试", Phone: "13800138000", Email: "private@example.com", ExpectedPosition: "Go工程师", RawText: "张测试\n性别：男\n年龄：28岁\n婚育：已婚已育\n邮箱：private@example.com\n手机：13800138000", WorkExperiences: []CandidateWorkExperience{{Content: body}}}
	chunks := buildResumeVectorChunks(candidate)
	again := buildResumeVectorChunks(candidate)
	seenTail, summary := false, 0
	for i, chunk := range chunks {
		if chunk.ID != again[i].ID {
			t.Fatal("相同原文的片段ID不稳定")
		}
		if strings.Contains(chunk.Text, "张测试") || strings.Contains(chunk.Text, "13800138000") || strings.Contains(chunk.Text, "private@example.com") || strings.Contains(chunk.Text, "28岁") || strings.Contains(chunk.Text, "已婚") {
			t.Fatal("敏感内容进入了向量")
		}
		if chunk.Kind == "summary" {
			summary++
		}
		if chunk.Path == "work_experiences[0].content" {
			if utf8.RuneCountInString(chunk.Text) != chunk.End-chunk.Start {
				t.Fatal("片段偏移不正确")
			}
			if strings.Contains(chunk.Text, "最后一段主导实时音视频系统") && strings.Contains(chunk.Text, "电话销售") {
				seenTail = true
			}
		}
	}
	if !seenTail || summary != 1 {
		t.Fatal("长文本尾部遗漏或概况数量不正确")
	}
}

// TestResumeVectorTaskConcurrency 验证账号重复任务复用、全局最多两个租约以及终态释放名额。
func TestResumeVectorTaskConcurrency(t *testing.T) {
	store := &memoryResumeVectorTasks{records: map[string]resumeVectorTask{}}
	for _, owner := range []string{"one", "two", "three"} {
		now := time.Now()
		task := resumeVectorTask{ID: vectorTaskID(), Owner: owner, Status: "queued", CreatedAt: now, ExpiresAt: now.Add(resumeVectorTaskTTL)}
		_, _ = store.Reserve(context.Background(), task)
	}
	first, ok, err := store.Claim(context.Background(), "one")
	if err != nil || !ok {
		t.Fatal("第一个任务未领取")
	}
	if _, ok, _ = store.Claim(context.Background(), "two"); !ok {
		t.Fatal("第二个任务未领取")
	}
	if _, ok, _ = store.Claim(context.Background(), "three"); ok {
		t.Fatal("全局并发超过两个")
	}
	duplicate, _ := store.Reserve(context.Background(), resumeVectorTask{ID: "duplicate", Owner: "one", Status: "queued", ExpiresAt: time.Now().Add(resumeVectorTaskTTL)})
	if duplicate.ID != first.ID {
		t.Fatal("重复请求创建新任务")
	}
	expected := first.Version
	first.Version++
	first.Status = "completed"
	first.LeaseUntil = 0
	if ok, err = store.Save(context.Background(), first, expected); err != nil || !ok {
		t.Fatal("任务未保存终态")
	}
	if _, ok, _ = store.Claim(context.Background(), "three"); !ok {
		t.Fatal("完成后没有释放名额")
	}
}
