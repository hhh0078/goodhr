// Package resume_import 文件作用：通过无扣费替身验证账号隔离、持久恢复、取消与分步幂等入库。
package resume_import

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestImportRejectsBuiltinIdentity 验证示例不能借批量确认或标记已存绕过只读边界。
func TestImportRejectsBuiltinIdentity(t *testing.T) {
	manager, _, client, _, _ := newTestManager(t, "13800138000")
	task := waitProcessed(t, manager, createTestTask(t, manager, 1).ID)
	item := task.Items[0]
	for _, extra := range []map[string]any{{"id": cloud.BuiltinCandidateID}, {"candidate_id": cloud.BuiltinCandidateID}, {"is_builtin": true}, {"is_builtin": false}, {"readonly": true}, {"source_type": "builtin"}} {
		extra["candidate_name"], extra["phone"] = "示例合成资料", "13800138000"
		fields, _ := json.Marshal(extra)
		_, err := manager.Confirm(context.Background(), "owner-a", "token", task.ID, []ConfirmItem{{ID: item.ID, Candidate: fields, Note: "不得保存", PositionIDs: []string{"position-1"}}})
		if err == nil || !strings.Contains(err.Error(), "示例") {
			t.Fatalf("builtin fields accepted: %v", err)
		}
	}
	fields, _ := json.Marshal(item.Candidate)
	_, err := manager.Confirm(context.Background(), "owner-a", "token", task.ID, []ConfirmItem{{ID: item.ID, Candidate: fields, CandidateID: cloud.BuiltinCandidateID}})
	if err == nil {
		t.Fatal("builtin merge accepted")
	}
	_, err = manager.MarkSaved(context.Background(), "owner-a", "token", task.ID, []SavedItem{{ID: item.ID, CandidateID: cloud.BuiltinCandidateID}})
	if err == nil {
		t.Fatal("builtin mark-saved accepted")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.creates != 0 || client.notes != 0 || client.positions != 0 {
		t.Fatal("builtin request had cloud write effects")
	}
}

// memorySettings 使用锁保护本地持久替身，允许重启管理器复用同一数据。
type memorySettings struct {
	mu     sync.Mutex
	values map[string]string
}

// GetSetting 读取测试持久键。
func (s *memorySettings) GetSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key], nil
}

// SetSetting 写入测试持久键。
func (s *memorySettings) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

// fakeCloud 记录幂等候选人写入和附属记录尝试数。
type fakeCloud struct {
	mu          sync.Mutex
	creates     int
	notes       int
	positions   int
	failNote    bool
	blockSave   chan struct{}
	enteredSave chan struct{}
}

// EffectiveAI 返回无密钥测试配置，不连接外部服务。
func (c *fakeCloud) EffectiveAI(context.Context, string) (cloud.AIConfig, error) {
	return cloud.AIConfig{}, nil
}

// SaveLibraryCandidate 模拟成功入库并可阻塞来验证并发保护。
func (c *fakeCloud) SaveLibraryCandidate(ctx context.Context, _ string, _ string, _ json.RawMessage, _ string) (cloud.CandidateSaveResult, error) {
	c.mu.Lock()
	c.creates++
	c.mu.Unlock()
	if c.enteredSave != nil {
		select {
		case c.enteredSave <- struct{}{}:
		default:
		}
	}
	if c.blockSave != nil {
		select {
		case <-c.blockSave:
		case <-ctx.Done():
			return cloud.CandidateSaveResult{}, ctx.Err()
		}
	}
	return cloud.CandidateSaveResult{ID: "candidate-1", RowVersion: 3}, nil
}

// LibraryCandidateDetail 模拟当前账号的候选人访问验证。
func (c *fakeCloud) LibraryCandidateDetail(_ context.Context, _ string, id string) (cloud.CandidateSaveResult, error) {
	return cloud.CandidateSaveResult{ID: id, RowVersion: 3}, nil
}

// AddLibraryNote 第一次可失败以验证主体收据不会丢失。
func (c *fakeCloud) AddLibraryNote(context.Context, string, string, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notes++
	if c.failNote {
		c.failNote = false
		return errors.New("备注暂时没记好")
	}
	return nil
}

// AddLibraryRecommendation 记录人工岗位保存次数。
func (c *fakeCloud) AddLibraryRecommendation(context.Context, string, string, string, string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.positions++
	return nil
}

// fakeExtractor 不读取用户真实文件，返回固定的测试简历文本。
type fakeExtractor struct{}

// Extract 返回固定文本给测试AI。
func (fakeExtractor) Extract(context.Context, string) (string, error) {
	return "虚构测试简历", nil
}

// fakeParser 在内存记录调用数和并发，按测试要求等待取消。
type fakeParser struct {
	mu                sync.Mutex
	calls             int
	active            int
	maxActive         int
	phone             string
	block             bool
	returnAfterCancel bool
	started           chan struct{}
}

// ParseResume 返回虚构草稿或等待ctx取消，绝不请求付费AI。
func (p *fakeParser) ParseResume(ctx context.Context, _ cloud.AIConfig, _ string) (cloud.LibraryCandidate, error) {
	p.mu.Lock()
	p.calls++
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active--; p.mu.Unlock() }()
	if p.started != nil {
		p.started <- struct{}{}
	}
	if p.block {
		<-ctx.Done()
		if !p.returnAfterCancel {
			return cloud.LibraryCandidate{}, ctx.Err()
		}
	}
	return cloud.LibraryCandidate{StructuredCandidate: cloud.StructuredCandidate{CandidateName: "测试甲", Phone: p.phone}}, nil
}

// TestReplacedWebSessionDiscardsLateImportResult 验证替换网页登录后即使模型迟到返回成功，也不接收已取消结果。
func TestReplacedWebSessionDiscardsLateImportResult(t *testing.T) {
	manager, _, _, parser, _ := newTestManager(t, "13800138000")
	parser.block, parser.returnAfterCancel, parser.started = true, true, make(chan struct{}, 1)
	task := createTestTask(t, manager, 1)
	<-parser.started
	manager.CancelActive()
	manager.Close()
	result, err := manager.Get("owner-a", "web-token-never-persist", task.ID)
	if err != nil || result.Items[0].Status != "failed" || result.Items[0].Candidate.CandidateName != "" || !strings.Contains(result.Items[0].Error, "可能已计费") {
		t.Fatalf("取消后接收了迟到结果：%+v %v", result, err)
	}
}

// newTestManager 创建隔离的测试目录和无外部依赖管理器。
func newTestManager(t *testing.T, phone string) (*Manager, *memorySettings, *fakeCloud, *fakeParser, string) {
	t.Helper()
	store := &memorySettings{values: map[string]string{}}
	client := &fakeCloud{}
	parser := &fakeParser{phone: phone}
	root := t.TempDir()
	manager, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	return manager, store, client, parser, root
}

// createTestTask 使用虚构文件建立一个或多个本地任务项。
func createTestTask(t *testing.T, manager *Manager, count int) Task {
	t.Helper()
	uploads := []Upload{}
	for i := 0; i < count; i++ {
		uploads = append(uploads, Upload{Name: fmt.Sprintf("测试%d.txt", i), Reader: strings.NewReader("虚构文本")})
	}
	task, err := manager.Create(context.Background(), "owner-a", "web-token-never-persist", uploads, "")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// waitProcessed 等待后台状态，不通过固定长休眠掩盖竞争。
func waitProcessed(t *testing.T, manager *Manager, id string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.Get("owner-a", "web-token-never-persist", id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == "completed" {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("任务未完成")
	return Task{}
}

// TestDownloadTaskStableAcrossReplayAndRestart 验证真实Manager按账号与文件摘要复用一次解析，来源不可由AI覆盖。
func TestDownloadTaskStableAcrossReplayAndRestart(t *testing.T) {
	manager, store, client, parser, root := newTestManager(t, "13800138000")
	check := func(context.Context) error { return nil }
	upload := Upload{Name: "合成下载.pdf", Reader: strings.NewReader("synthetic"), Source: &DownloadSource{PlatformID: "boss", SourceURL: "https://www.zhipin.com/resume"}}
	task, err := manager.CreateDownload(context.Background(), "owner-a", "web-token-never-persist", upload, "stable-file-sha256", check)
	if err != nil {
		t.Fatal(err)
	}
	result := waitProcessed(t, manager, task.ID)
	if result.Items[0].Candidate.PlatformID != "boss" || result.Items[0].Status != "failed" {
		t.Fatalf("未经AI确认的材料被自动放行：%+v", result)
	}
	upload.Reader = strings.NewReader("synthetic")
	again, err := manager.CreateDownload(context.Background(), "owner-a", "web-token-never-persist", upload, "stable-file-sha256", check)
	if err != nil || again.ID != task.ID {
		t.Fatalf("重放任务不稳定：%+v %v", again, err)
	}
	manager.Close()
	restored, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	upload.Reader = strings.NewReader("synthetic")
	again, err = restored.CreateDownload(context.Background(), "owner-a", "web-token-never-persist", upload, "stable-file-sha256", check)
	if err != nil || again.ID != task.ID {
		t.Fatal("重启任务不稳定")
	}
	parser.mu.Lock()
	calls := parser.calls
	parser.mu.Unlock()
	if calls != 1 {
		t.Fatalf("重复AI次数=%d", calls)
	}
}

// TestOwnerPhoneAndTokenPersistence 验证跨账号不可读、掩码不补全、凭证不进入SQLite。
func TestOwnerPhoneAndTokenPersistence(t *testing.T) {
	manager, store, _, _, _ := newTestManager(t, "138****0000")
	task := waitProcessed(t, manager, createTestTask(t, manager, 1).ID)
	if task.Items[0].Status != "needs_phone" || task.Items[0].Candidate.Phone != "" {
		t.Fatalf("掩码手机号被接受：%+v", task.Items[0])
	}
	if _, err := manager.Get("owner-b", "other", task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("账号隔离失效：%v", err)
	}
	if len(manager.List("owner-b", "other")) != 0 {
		t.Fatal("跨账号列表泄漏")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, raw := range store.values {
		if strings.Contains(raw, "web-token-never-persist") {
			t.Fatal("Token进入持久数据")
		}
	}
}

// TestPartialSuccessRestartAndIdempotency 验证附属失败、重启和只传id重试都不再次创建或AI解析。
func TestPartialSuccessRestartAndIdempotency(t *testing.T) {
	manager, store, client, parser, root := newTestManager(t, "13800138000")
	client.failNote = true
	task := waitProcessed(t, manager, createTestTask(t, manager, 1).ID)
	item := task.Items[0]
	fields, _ := json.Marshal(item.Candidate)
	task, err := manager.Confirm(context.Background(), "owner-a", "token", task.ID, []ConfirmItem{{ID: item.ID, Candidate: fields, Note: "测试备注", PositionIDs: []string{"position-1", "position-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if task.Items[0].Status != "partial_success" || task.Items[0].CandidateID != "candidate-1" || task.Items[0].Candidate.RowVersion != 3 {
		t.Fatalf("主体收据丢失：%+v", task.Items[0])
	}
	manager.Close()
	restored, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	task, err = restored.Confirm(context.Background(), "owner-a", "token", task.ID, []ConfirmItem{{ID: item.ID}})
	if err != nil || task.Items[0].Status != "saved" {
		t.Fatalf("重启补记失败：%+v %v", task, err)
	}
	_, err = restored.Confirm(context.Background(), "owner-a", "token", task.ID, []ConfirmItem{{ID: item.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if client.creates != 1 || client.notes != 2 || client.positions != 1 || parser.calls != 1 {
		t.Fatalf("发生重复副作用：create=%d note=%d position=%d ai=%d", client.creates, client.notes, client.positions, parser.calls)
	}
}

// TestCancelAndRestartNeverAutomaticallyRepeatAI 验证两个并发槽位和结果不明AI请求的人工重试边界。
func TestCancelAndRestartNeverAutomaticallyRepeatAI(t *testing.T) {
	manager, store, client, parser, root := newTestManager(t, "13800138000")
	parser.block = true
	parser.started = make(chan struct{}, 4)
	task := createTestTask(t, manager, 3)
	for i := 0; i < 2; i++ {
		select {
		case <-parser.started:
		case <-time.After(3 * time.Second):
			t.Fatal("工作者没启动")
		}
	}
	parser.mu.Lock()
	if parser.maxActive > 2 {
		t.Fatal("并发超过2")
	}
	parser.mu.Unlock()
	if _, err := manager.Cancel("owner-a", task.ID); err != nil {
		t.Fatal(err)
	}
	manager.Close()
	parser.mu.Lock()
	calls := parser.calls
	parser.mu.Unlock()
	restored, err := New(store, client, parser, fakeExtractor{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	state, err := restored.Get("owner-a", "new-token", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "cancelled" {
		t.Fatal("取消状态未保留")
	}
	failed := 0
	for _, item := range state.Items {
		if item.Status == "failed" && strings.Contains(item.Error, "计费") {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("结果不明请求没有保留提醒：%+v", state.Items)
	}
	parser.mu.Lock()
	defer parser.mu.Unlock()
	if parser.calls != calls {
		t.Fatal("重启自动重复付费AI")
	}
}

// TestConcurrentConfirmationIsRejected 验证云端保存尚未返回时，第二个确认不能创建第二条简历。
func TestConcurrentConfirmationIsRejected(t *testing.T) {
	manager, _, client, _, _ := newTestManager(t, "13800138000")
	task := waitProcessed(t, manager, createTestTask(t, manager, 1).ID)
	client.blockSave = make(chan struct{})
	client.enteredSave = make(chan struct{}, 1)
	fields, _ := json.Marshal(task.Items[0].Candidate)
	input := []ConfirmItem{{ID: task.Items[0].ID, Candidate: fields}}
	done := make(chan error, 1)
	go func() {
		_, err := manager.Confirm(context.Background(), "owner-a", "token", task.ID, input)
		done <- err
	}()
	select {
	case <-client.enteredSave:
	case <-time.After(3 * time.Second):
		t.Fatal("保存没启动")
	}
	_, err := manager.Confirm(context.Background(), "owner-a", "token", task.ID, input)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("没有保护并发确认：%v", err)
	}
	close(client.blockSave)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if client.creates != 1 {
		t.Fatal("发生二次入库")
	}
}

// TestSQLiteDraftSurvivesProcessRestart 验证实际SQLite关闭和重新打开后保留草稿，且不会重复调用AI。
func TestSQLiteDraftSurvivesProcessRestart(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "local.db")
	store, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeCloud{}
	parser := &fakeParser{phone: "13800138000"}
	manager, err := New(store, client, parser, fakeExtractor{}, filepath.Join(root, "imports"))
	if err != nil {
		t.Fatal(err)
	}
	task := waitProcessed(t, manager, createTestTask(t, manager, 1).ID)
	manager.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored, err := New(store, client, parser, fakeExtractor{}, filepath.Join(root, "imports"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	loaded, err := restored.Get("owner-a", "new-web-token", task.ID)
	if err != nil || loaded.Items[0].Status != "ready" || loaded.Items[0].Candidate.Phone != "13800138000" {
		t.Fatalf("SQLite草稿恢复失败：%+v %v", loaded, err)
	}
	raw, err := store.GetSetting(context.Background(), taskStorageKey(task.ID))
	if err != nil || strings.Contains(raw, "web-token-never-persist") || strings.Contains(raw, "new-web-token") {
		t.Fatal("持久数据错误或含Token")
	}
	if parser.calls != 1 {
		t.Fatal("恢复草稿再次调用了AI")
	}
}
