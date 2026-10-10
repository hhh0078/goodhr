// Package resume_download 文件作用：用本地合成文件和无付费替身验证历史过滤、账号隔离、文件防重、开关与失败通知。
package resume_download

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	resumeimport "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// memoryStore 保存测试队列，重建Manager后继续读取同一份数据。
type memoryStore struct{ values map[string]string }

// GetSetting 返回已有本地键值。
func (s *memoryStore) GetSetting(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

// SetSetting 持久保存测试JSON。
func (s *memoryStore) SetSetting(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

// fakeCloud 模拟开关、账号、查重和消息提交失败，不访问真实服务器。
type fakeCloud struct {
	enabled, duplicate, failMessage bool
	owner                           string
	pushes                          int
	duplicateErr                    error
}

// ValidateSession 返回当前测试账号。
func (c *fakeCloud) ValidateSession(context.Context, string) (cloud.UserSession, error) {
	return cloud.UserSession{UserID: c.owner, SessionKind: "web"}, nil
}

// ResumeDownloadSettings 返回测试开关。
func (c *fakeCloud) ResumeDownloadSettings(context.Context, string) (cloud.ResumeLibrarySettings, error) {
	return cloud.ResumeLibrarySettings{AutoImportDownloads: c.enabled}, nil
}

// LibraryCandidateHasDuplicate 控制当前草稿是否存在重复。
func (c *fakeCloud) LibraryCandidateHasDuplicate(context.Context, string, cloud.LibraryCandidate) (bool, error) {
	return c.duplicate, c.duplicateErr
}

// PushResumeMessage 第一次可以失败，以验证只重试消息、不重复解析。
func (c *fakeCloud) PushResumeMessage(context.Context, string, string, string, string, string) error {
	c.pushes++
	if c.failMessage {
		c.failMessage = false
		return errors.New("消息网络中断")
	}
	return nil
}

// fakeImports 只记录解析调用和完整入库，文件内容仍由Reader读取。
type fakeImports struct {
	creates, saves, cancels int
	status                  string
	resume                  bool
	tasks                   map[string]resumeimport.Task
	getErr, confirmErr      error
	confirmedBeforeError    bool
}

// CreateDownload 模拟同次AI判断和提取结果，检查可信来源传入。
func (f *fakeImports) CreateDownload(ctx context.Context, owner, token string, upload resumeimport.Upload, hash string, check func(context.Context) error) (resumeimport.Task, error) {
	if err := check(ctx); err != nil {
		return resumeimport.Task{}, err
	}
	if _, err := io.ReadAll(upload.Reader); err != nil {
		return resumeimport.Task{}, err
	}
	if upload.Source == nil || upload.Source.PlatformID == "" {
		return resumeimport.Task{}, errors.New("缺来源")
	}
	if task, ok := f.tasks[owner+hash]; ok {
		return task, nil
	}
	f.creates++
	candidate := cloud.LibraryCandidate{StructuredCandidate: cloud.StructuredCandidate{CandidateName: "合成测试候选人", Phone: "13800138000"}, IsResume: &f.resume}
	task := resumeimport.Task{ID: owner + hash, Status: "completed", Items: []resumeimport.Item{{ID: "one", Status: f.status, Candidate: candidate}}}
	f.tasks[task.ID] = task
	return task, nil
}

// AuthorizeDownload 模拟恢复同账号任务。
func (f *fakeImports) AuthorizeDownload(string, string, func(context.Context) error) error {
	return nil
}

// Get 返回解析草稿。
func (f *fakeImports) Get(_, _, id string) (resumeimport.Task, error) { return f.tasks[id], f.getErr }

// Confirm 模拟幂等保存完整草稿。
func (f *fakeImports) Confirm(_ context.Context, _, _, id string, _ []resumeimport.ConfirmItem) (resumeimport.Task, error) {
	f.saves++
	task := f.tasks[id]
	if f.confirmErr != nil && !f.confirmedBeforeError {
		return resumeimport.Task{}, f.confirmErr
	}
	task.Items[0].Status = "saved"
	f.tasks[id] = task
	return task, f.confirmErr
}

// Cancel 只取消自动批次，保留草稿。
func (f *fakeImports) Cancel(_, id string) (resumeimport.Task, error) {
	f.cancels++
	task := f.tasks[id]
	task.Status = "cancelled"
	if len(task.Items) > 0 {
		task.Items[0].Status = "failed"
	}
	f.tasks[id] = task
	return task, nil
}

// TestDisableCancelsDownloadBatchAndInvalidatesOldAccess 验证关闭会取消自动批次，重新开启旧授权仍无效且只提示恢复草稿。
func TestDisableCancelsDownloadBatchAndInvalidatesOldAccess(t *testing.T) {
	m, _, client, imports, item := fixture(t)
	imports.status = "queued"
	ctx := context.Background()
	if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	check := m.accessCheck("a", "a-web-token", m.generation)
	client.enabled = false
	if err := m.RefreshSettings(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.cancels != 1 || check(ctx) == nil {
		t.Fatal("关闭未取消任务或旧授权未失效")
	}
	client.enabled = true
	if err := m.RefreshSettings(ctx); err != nil {
		t.Fatal(err)
	}
	if check(ctx) == nil {
		t.Fatal("重新开启复活了旧授权")
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 1 || imports.saves != 0 || client.pushes != 1 || m.entries[item.ID].Status != "needs_review" {
		t.Fatalf("暂停任务被重做或无人提示：%+v", m.entries[item.ID])
	}
}

// fixture 建立隔离Manager和合成下载，时间晚于本次绑定。
func fixture(t *testing.T) (*Manager, *memoryStore, *fakeCloud, *fakeImports, contract.DownloadRecord) {
	t.Helper()
	store := &memoryStore{values: map[string]string{}}
	client := &fakeCloud{enabled: true, owner: "a"}
	imports := &fakeImports{status: "ready", resume: true, tasks: map[string]resumeimport.Task{}}
	m, err := New(store, client, imports)
	if err != nil {
		t.Fatal(err)
	}
	m.Bind("a", "a-web-token")
	path := filepath.Join(t.TempDir(), "合成简历.PDF")
	if err := os.WriteFile(path, []byte("synthetic resume"), 0600); err != nil {
		t.Fatal(err)
	}
	return m, store, client, imports, contract.DownloadRecord{ID: "download-one", FilePath: path, PageURL: "https://www.zhipin.com/web/resume?token=hidden", URL: "https://cdn.example.invalid/file", Status: "saved", CreatedAt: time.Now().Add(time.Second).Format(time.RFC3339Nano)}
}

// TestNewOnlyAndContentDedup 验证历史、未完成、空referrer CDN 和恶意域名不收费，同文件重下载不重复收费。
func TestNewOnlyAndContentDedup(t *testing.T) {
	m, store, _, imports, item := fixture(t)
	ctx := context.Background()
	history := item
	history.ID = "old"
	history.CreatedAt = m.started.Add(-time.Hour).Format(time.RFC3339Nano)
	unknown := item
	unknown.ID = "unknown"
	unknown.PageURL = ""
	spoof := item
	spoof.ID = "spoof"
	spoof.PageURL = "https://zhipin.com.evil.invalid/path"
	pending := item
	pending.ID = "pending"
	pending.Status = "pending"
	if err := m.Observe(ctx, []contract.DownloadRecord{history, unknown, spoof, pending, item}); err != nil {
		t.Fatal(err)
	}
	if len(m.entries) != 1 {
		t.Fatalf("误收下载：%+v", m.entries)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	second := item
	second.ID = "download-again"
	if err := m.Observe(ctx, []contract.DownloadRecord{second}); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 1 || imports.saves != 1 {
		t.Fatalf("重复AI或入库：%d %d", imports.creates, imports.saves)
	}
	restored, err := New(store, m.cloud, imports)
	if err != nil {
		t.Fatal(err)
	}
	restored.Bind("a", "a-web-token")
	if err := restored.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 1 {
		t.Fatal("重启重复AI")
	}
	var persisted map[string]entry
	if json.Unmarshal([]byte(store.values[stateKey]), &persisted) != nil || persisted[item.ID].SourceURL != "https://www.zhipin.com/web/resume" {
		t.Fatal("来源保留敏感参数或队列未落盘")
	}
}

// TestDisabledDownloadsNeverReplayed 验证关闭期间的新下载不在重新打开时补收，之后新下载正常导入。
func TestDisabledDownloadsNeverReplayed(t *testing.T) {
	m, _, client, imports, item := fixture(t)
	ctx := context.Background()
	client.enabled = false
	if err := m.RefreshSettings(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	client.enabled = true
	if err := m.RefreshSettings(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 0 {
		t.Fatal("关闭期间文件被补收")
	}
	item.ID = "new-after-enabled"
	if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 1 {
		t.Fatal("开启后的新下载未导入")
	}
}

// TestFailureDuplicateAndNonResumeNotifyWithoutRepeatingAI 验证收费失败、缺号、重复与非简历均保留待处理且消息可重试。
func TestFailureDuplicateAndNonResumeNotifyWithoutRepeatingAI(t *testing.T) {
	for _, scenario := range []struct {
		name, status      string
		duplicate, resume bool
	}{{"AI失败", "failed", false, true}, {"缺手机号", "needs_phone", false, true}, {"简历重复", "ready", true, true}, {"非简历", "not_resume", false, false}, {"未经确认材料", "ready", false, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			m, _, client, imports, item := fixture(t)
			imports.status, imports.resume = scenario.status, scenario.resume
			client.duplicate, client.failMessage = scenario.duplicate, true
			ctx := context.Background()
			if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
				t.Fatal(err)
			}
			if err := m.Tick(ctx); err == nil {
				t.Fatal("第一次消息失败未返回")
			}
			if err := m.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if imports.creates != 1 || imports.saves != 0 || client.pushes != 2 {
				t.Fatalf("副作用计数：AI=%d 保存=%d 消息=%d", imports.creates, imports.saves, client.pushes)
			}
		})
	}
}

// TestAccountSwitchKeepsOwnership 验证旧下载不能被新账号解析、入库或收到通知，旧校验闭包失效。
func TestAccountSwitchKeepsOwnership(t *testing.T) {
	m, _, client, imports, item := fixture(t)
	ctx := context.Background()
	if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
		t.Fatal(err)
	}
	if err := m.RefreshSettings(ctx); err != nil {
		t.Fatal(err)
	}
	oldCheck := m.accessCheck("a", "a-web-token", m.generation)
	m.Bind("b", "b-web-token")
	client.owner = "b"
	if err := m.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if imports.creates != 0 {
		t.Fatal("旧下载交给新账号")
	}
	if oldCheck(ctx) == nil {
		t.Fatal("切号后的旧授权仍有效")
	}
	client.owner = "other"
	newItem := item
	newItem.ID = "forged-account"
	newItem.CreatedAt = time.Now().Add(time.Second).Format(time.RFC3339Nano)
	if err := m.Observe(ctx, []contract.DownloadRecord{newItem}); err != nil {
		t.Fatal(err)
	}
	if err := m.Tick(ctx); err == nil {
		t.Fatal("云端账号不一致仍解析")
	}
}

// TestCloudWriteAndLookupFailuresBecomeNotifiedDrafts 验证查重、读取和不明确入库失败只提示待核对，不每秒重复写入或解析。
func TestCloudWriteAndLookupFailuresBecomeNotifiedDrafts(t *testing.T) {
	for _, scenario := range []string{"查重失败", "入库结果未知", "草稿读取失败", "收据确认已保存"} {
		t.Run(scenario, func(t *testing.T) {
			m, _, client, imports, item := fixture(t)
			switch scenario {
			case "查重失败":
				client.duplicateErr = errors.New("offline")
			case "入库结果未知":
				imports.confirmErr = errors.New("timeout")
			case "草稿读取失败":
				imports.getErr = errors.New("draft offline")
			case "收据确认已保存":
				imports.confirmErr = errors.New("response lost")
				imports.confirmedBeforeError = true
			}
			ctx := context.Background()
			if err := m.Observe(ctx, []contract.DownloadRecord{item}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				if err := m.Tick(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if imports.creates != 1 || imports.saves > 1 {
				t.Fatalf("重复收费或写入：AI=%d 保存=%d", imports.creates, imports.saves)
			}
			if scenario == "收据确认已保存" {
				if client.pushes != 0 || m.entries[item.ID].Status != "saved" {
					t.Fatal("已确认的成功被当成失败")
				}
			} else if client.pushes != 1 || m.entries[item.ID].Status != "needs_review" {
				t.Fatalf("失败未通知或继续自动执行：%+v", m.entries[item.ID])
			}
		})
	}
}
