// Package resume_summary 文件作用：用无付费模型和隔离本地存储验证摘要幂等、权限、重启恢复与并发取消。
package resume_summary

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

type memorySummaryStore struct {
	mu     sync.Mutex
	values map[string]string
}

// GetSetting 读取线程安全的测试存储。
func (s *memorySummaryStore) GetSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}

// SetSetting 保存测试草稿，不访问生产数据库。
func (s *memorySummaryStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

type summaryTestCloud struct{}

// EffectiveAI 提供仅用于替身的模型配置。
func (summaryTestCloud) EffectiveAI(context.Context, string) (cloud.AIConfig, error) {
	return cloud.AIConfig{BaseURL: "https://example.invalid", Model: "test-model", APIKey: "provider-private"}, nil
}

type summaryTestAI struct {
	calls        atomic.Int32
	entered      chan cloud.LibraryCandidate
	release      chan struct{}
	ignoreCancel bool
}

// SummarizeResume 记录已脱敏的输入，并模拟正常完成或请求中断。
func (f *summaryTestAI) SummarizeResume(ctx context.Context, _ cloud.AIConfig, c cloud.LibraryCandidate) (string, error) {
	f.calls.Add(1)
	if f.entered != nil {
		f.entered <- c
	}
	if f.release != nil {
		if f.ignoreCancel {
			<-f.release
		} else {
			select {
			case <-f.release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "五年产品经验；provider-private full-web-private", nil
}

// summaryFixture 返回不含真实人员信息的职业资料。
func summaryFixture() cloud.LibraryCandidate {
	return cloud.LibraryCandidate{StructuredCandidate: cloud.StructuredCandidate{CandidateName: "测试候选人", Phone: "13800138000", Email: "test@example.invalid", RawText: "职业正文", WorkExperiences: []cloud.CandidateWorkExperience{{CompanyName: "合成公司", PositionName: "产品经理", Content: "五年产品经验"}}}, AISummary: "旧AI猜测", PlatformID: "liepin", SourceURL: "https://example.invalid", Tags: []string{"产品"}}
}

// summaryAllow 模拟一次成功的完整登录检查。
func summaryAllow(context.Context) error { return nil }

// newSummaryTestManager 组装隔离的流程与测试模型。
func newSummaryTestManager(t *testing.T, store *memorySummaryStore, ai *summaryTestAI) *Manager {
	t.Helper()
	if store == nil {
		store = &memorySummaryStore{values: map[string]string{}}
	}
	m, err := New(store, summaryTestCloud{}, ai)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

// awaitSummary 等待后台状态稳定，不触发任何重试。
func awaitSummary(t *testing.T, m *Manager, id, status string) Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := m.Get("account-a", "web", id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("摘要未到达%s", status)
	return Task{}
}

// TestSummaryIdempotencyIsolationAndSecrets 验证重复请求只调用一次模型、安装账号隔离和公开响应不含原文及密钥。
func TestSummaryIdempotencyIsolationAndSecrets(t *testing.T) {
	ai := &summaryTestAI{entered: make(chan cloud.LibraryCandidate, 1)}
	store := &memorySummaryStore{values: map[string]string{}}
	m := newSummaryTestManager(t, store, ai)
	first, err := m.Create("account-a", "web", "request-one", "full-web-private", summaryFixture(), summaryAllow)
	if err != nil {
		t.Fatal(err)
	}
	parsed := <-ai.entered
	if parsed.Phone != "" || parsed.Email != "" || parsed.CandidateName != "" || parsed.AISummary != "" || parsed.SourceURL != "" {
		t.Fatal("无关资料被送入AI")
	}
	done := awaitSummary(t, m, first.ID, "completed")
	for range 5 {
		if _, err = m.Create("account-a", "web", "request-one", "full-web-private", summaryFixture(), summaryAllow); err != nil {
			t.Fatal(err)
		}
	}
	if ai.calls.Load() != 1 {
		t.Fatal("幂等请求重复调用AI")
	}
	changed := summaryFixture()
	changed.RawText = "新的职业事实"
	if _, err = m.Create("account-a", "web", "request-one", "full-web-private", changed, summaryAllow); !errors.Is(err, ErrConflict) {
		t.Fatal("同请求键不同内容未冲突")
	}
	for _, pair := range [][2]string{{"account-b", "web"}, {"account-a", "extension:other"}} {
		if _, err = m.Get(pair[0], pair[1], first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("账号或安装隔离失效")
		}
	}
	encoded, _ := json.Marshal(done)
	for _, secret := range []string{"职业正文", "provider-private", "full-web-private", "13800138000"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("公开草稿泄露原文或凭证")
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, value := range store.values {
		if strings.Contains(value, "provider-private") || strings.Contains(value, "full-web-private") {
			t.Fatal("凭证落盘")
		}
	}
}

// TestSummaryInterruptedRestartRequiresExplicitRetry 验证重启后的未知计费项不会自动再次调用模型。
func TestSummaryInterruptedRestartRequiresExplicitRetry(t *testing.T) {
	store := &memorySummaryStore{values: map[string]string{}}
	ai := &summaryTestAI{entered: make(chan cloud.LibraryCandidate, 1), release: make(chan struct{})}
	m := newSummaryTestManager(t, store, ai)
	task, err := m.Create("account-a", "web", "restart-request", "full-web-private", summaryFixture(), summaryAllow)
	if err != nil {
		t.Fatal(err)
	}
	<-ai.entered
	store.mu.Lock()
	snapshot := &memorySummaryStore{values: map[string]string{}}
	for key, value := range store.values {
		snapshot.values[key] = value
	}
	store.mu.Unlock()
	freshAI := &summaryTestAI{}
	restarted := newSummaryTestManager(t, snapshot, freshAI)
	recovered := awaitSummary(t, restarted, task.ID, "failed")
	if !recovered.PossiblyCharged || !strings.Contains(recovered.Error, "可能已计费") {
		t.Fatal("计费不明状态丢失")
	}
	if _, err = restarted.Create("account-a", "web", "restart-request", "full-web-private", summaryFixture(), summaryAllow); err != nil {
		t.Fatal(err)
	}
	if freshAI.calls.Load() != 0 {
		t.Fatal("重启自动调用AI")
	}
	if _, err = restarted.Retry("account-a", "web", task.ID, "full-web-private", summaryAllow); err != nil {
		t.Fatal(err)
	}
	awaitSummary(t, restarted, task.ID, "completed")
	if freshAI.calls.Load() != 1 {
		t.Fatal("明确重试没有单次调用")
	}
	m.CancelActive("")
}

// TestSummaryConcurrentCancellationAndChangedSession 验证两个AI并发槽、排队取消和会话变更后拒收成功响应。
func TestSummaryConcurrentCancellationAndChangedSession(t *testing.T) {
	ai := &summaryTestAI{entered: make(chan cloud.LibraryCandidate, 3), release: make(chan struct{})}
	m := newSummaryTestManager(t, nil, ai)
	first, _ := m.Create("account-a", "web", "concurrent-one", "full-web-private", summaryFixture(), summaryAllow)
	second, _ := m.Create("account-a", "web", "concurrent-two", "full-web-private", summaryFixture(), summaryAllow)
	<-ai.entered
	<-ai.entered
	queued, _ := m.Create("account-a", "web", "concurrent-three", "full-web-private", summaryFixture(), summaryAllow)
	if _, err := m.Cancel("account-a", "web", queued.ID); err != nil {
		t.Fatal(err)
	}
	m.CancelActive("")
	m.Close()
	if ai.calls.Load() != 2 {
		t.Fatal("取消后调用排队AI或超过并发限制")
	}
	if !awaitSummary(t, m, first.ID, "failed").PossiblyCharged || !awaitSummary(t, m, second.ID, "failed").PossiblyCharged || awaitSummary(t, m, queued.ID, "cancelled").PossiblyCharged {
		t.Fatal("取消计费标记不准确")
	}
	if _, err := m.Retry("account-a", "web", first.ID, "full-web-private", summaryAllow); err == nil {
		t.Fatal("关闭后还能重试")
	}
	lateAI := &summaryTestAI{entered: make(chan cloud.LibraryCandidate, 1), release: make(chan struct{}), ignoreCancel: true}
	late := newSummaryTestManager(t, nil, lateAI)
	var invalid atomic.Bool
	check := func(context.Context) error {
		if invalid.Load() {
			return errors.New("已切号")
		}
		return nil
	}
	lateTask, _ := late.Create("account-a", "web", "session-changed", "full-web-private", summaryFixture(), check)
	<-lateAI.entered
	invalid.Store(true)
	close(lateAI.release)
	failed := awaitSummary(t, late, lateTask.ID, "failed")
	if failed.Summary != "" || !failed.PossiblyCharged {
		t.Fatal("切号后接受了AI结果")
	}
}

// TestSummaryRejectsExcessiveInput 验证数组规模和职业段落长度在调用模型前受限。
func TestSummaryRejectsExcessiveInput(t *testing.T) {
	ai := &summaryTestAI{}
	m := newSummaryTestManager(t, nil, ai)
	candidate := summaryFixture()
	candidate.WorkExperiences = make([]cloud.CandidateWorkExperience, 101)
	if _, err := m.Create("account-a", "web", "too-many-items", "full-web-private", candidate, summaryAllow); err == nil {
		t.Fatal("无经历规模限制")
	}
	candidate = summaryFixture()
	candidate.PersonalDescription = strings.Repeat("字", 20001)
	if _, err := m.Create("account-a", "web", "too-long-field", "full-web-private", candidate, summaryAllow); err == nil {
		t.Fatal("无字段长度限制")
	}
	if ai.calls.Load() != 0 {
		t.Fatal("无效输入调用AI")
	}
}
