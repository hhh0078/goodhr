// Package resume_summary 文件作用：独立编排只读简历摘要，持久化请求幂等、取消和计费不明状态，绝不自动保存云端简历。
package resume_summary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"sort"
	"strings"
	"sync"
	"time"
)

const indexKey = "resume_summary_tasks_v1"

// Manager 只在本地内存持有每次运行的凭证，两个槽位限制摘要并发。
type Manager struct {
	mu     sync.Mutex
	store  SettingsStore
	cloud  CloudConfig
	ai     Summarizer
	tasks  map[string]*storedTask
	active map[string]context.CancelFunc
	slots  chan struct{}
	wg     sync.WaitGroup
	closed bool
}

// New 恢复草稿；上次运行中断的请求只能由用户明确重试，不能自行再次调用模型。
func New(store SettingsStore, client CloudConfig, ai Summarizer) (*Manager, error) {
	if store == nil || client == nil || ai == nil {
		return nil, fmt.Errorf("本地摘要能力还没准备好")
	}
	m := &Manager{store: store, cloud: client, ai: ai, tasks: map[string]*storedTask{}, active: map[string]context.CancelFunc{}, slots: make(chan struct{}, 2)}
	raw, err := store.GetSetting(context.Background(), indexKey)
	if err != nil {
		return nil, err
	}
	var ids []string
	if raw != "" && json.Unmarshal([]byte(raw), &ids) != nil {
		return nil, fmt.Errorf("摘要索引没读出来，请保留本地数据后重试")
	}
	for _, id := range ids {
		value, err := store.GetSetting(context.Background(), storageKey(id))
		if err != nil {
			return nil, err
		}
		var task storedTask
		if json.Unmarshal([]byte(value), &task) != nil || task.Task.ID != id || task.Owner == "" {
			return nil, fmt.Errorf("摘要草稿没读出来，请保留本地数据后重试")
		}
		if task.Task.Status == "running" || task.Task.Status == "queued" {
			task.Task.Status, task.Task.Error, task.Task.PossiblyCharged = "failed", "上次摘要已中断，请核对后手动重试。", task.Started
			if task.Started {
				task.Task.Error += "可能已计费，不会自动重试。"
			}
		}
		m.tasks[id] = &task
		if err := m.persistLocked(&task); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Create 对账号、安装和请求编号实施幂等，重复POST返回现有草稿而不重复调用AI。
func (m *Manager) Create(owner, scope, key, token string, candidate cloud.LibraryCandidate, check AuthCheck) (Task, error) {
	if owner == "" || scope == "" || len(key) < 8 || len(key) > 128 || token == "" || check == nil {
		return Task{}, fmt.Errorf("摘要请求信息不完整，请重新发起")
	}
	candidate, err := summaryCandidate(candidate)
	if err != nil {
		return Task{}, err
	}
	encoded, _ := json.Marshal(candidate)
	fingerprint := digest(string(encoded))
	id := digest(owner + "\x00" + scope + "\x00" + key)[:32]
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Task{}, fmt.Errorf("本地程序正在退出，请稍后再试")
	}
	if old := m.tasks[id]; old != nil {
		if old.Fingerprint != fingerprint {
			return Task{}, ErrConflict
		}
		return old.Task, nil
	}
	stored := &storedTask{Owner: owner, Scope: scope, Fingerprint: fingerprint, Candidate: candidate, Task: Task{ID: id, Status: "queued"}}
	m.tasks[id] = stored
	if err := m.persistLocked(stored); err != nil {
		delete(m.tasks, id)
		return Task{}, err
	}
	if err := m.persistIndexLocked(); err != nil {
		delete(m.tasks, id)
		return Task{}, err
	}
	m.startLocked(stored, token, check)
	return stored.Task, nil
}

// Get 校验原账号和插件安装后读取，读取操作不会恢复或重新调用模型。
func (m *Manager) Get(owner, scope, id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.findLocked(owner, scope, id)
	if err != nil {
		return Task{}, err
	}
	return task.Task, nil
}

// Cancel 中止一次摘要请求，已完成摘要保留，可能计费的状态留给用户核对。
func (m *Manager) Cancel(owner, scope, id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, err := m.findLocked(owner, scope, id)
	if err != nil {
		return Task{}, err
	}
	if task.Task.Status == "completed" {
		return task.Task, nil
	}
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	task.Task.Status, task.Task.PossiblyCharged = "cancelled", task.Started
	task.Task.Error = "摘要已取消，草稿保留。"
	if task.Started {
		task.Task.Error += "可能已计费，重试需再次确认。"
	}
	return task.Task, m.persistLocked(task)
}

// Retry 仅在用户明确点击后再次摘要，运行中或成功项不能重复扣费。
func (m *Manager) Retry(owner, scope, id, token string, check AuthCheck) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || token == "" || check == nil {
		return Task{}, fmt.Errorf("本地登录或程序还没准备好，请重新连接")
	}
	task, err := m.findLocked(owner, scope, id)
	if err != nil {
		return Task{}, err
	}
	if m.active[id] != nil {
		return Task{}, ErrBusy
	}
	if task.Task.Status != "failed" && task.Task.Status != "cancelled" {
		return Task{}, fmt.Errorf("已有摘要请核对保存，不需要重复生成")
	}
	task.Task.Status, task.Task.Error, task.Task.PossiblyCharged, task.Started = "queued", "", false, false
	if err := m.persistLocked(task); err != nil {
		return Task{}, err
	}
	m.startLocked(task, token, check)
	return task.Task, nil
}

// CancelActive 登录代次变化时取消后台摘要；已完成草稿保留，未完成请求不自动重试。
func (m *Manager) CancelActive(scope string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, cancel := range m.active {
		if scope == "" || m.tasks[id].Scope == scope {
			cancel()
		}
	}
}

// Close 取消全部后台请求并等待状态保存，防止关闭后遗留AI调用。
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	for _, cancel := range m.active {
		cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// startLocked 调度一个有时间上限的只读摘要任务，调用者须持有管理器锁。
func (m *Manager) startLocked(task *storedTask, token string, check AuthCheck) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	m.active[task.Task.ID] = cancel
	m.wg.Add(1)
	go m.run(ctx, task.Task.ID, token, check)
}

// run 平铺执行授权检查、模型配置、一次AI请求和结果授权复核，所有结果仅留在本地草稿。
func (m *Manager) run(ctx context.Context, id, token string, check AuthCheck) {
	defer m.wg.Done()
	defer func() {
		m.mu.Lock()
		if cancel := m.active[id]; cancel != nil {
			cancel()
		}
		delete(m.active, id)
		m.mu.Unlock()
	}()
	select {
	case m.slots <- struct{}{}:
		defer func() { <-m.slots }()
	case <-ctx.Done():
		m.finishStopped(id)
		return
	}
	err := check(ctx)
	var cfg cloud.AIConfig
	if err == nil {
		cfg, err = m.cloud.EffectiveAI(ctx, token)
	}
	if err == nil && (strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "") {
		err = fmt.Errorf("模型配置不完整")
	}
	if err == nil {
		err = check(ctx)
	}
	m.mu.Lock()
	task := m.tasks[id]
	if ctx.Err() != nil {
		m.mu.Unlock()
		m.finishStopped(id)
		return
	}
	if err != nil {
		task.Task.Status, task.Task.Error = "failed", "本地登录或AI配置没准备好，请重新连接并检查模型设置。"
		_ = m.persistLocked(task)
		m.mu.Unlock()
		return
	}
	task.Task.Status, task.Task.Model, task.Started = "running", hideSecrets(cfg.Model, token, cfg.APIKey), true
	if err = m.persistLocked(task); err != nil {
		task.Started = false
		task.Task.Status, task.Task.Error = "failed", "摘要请求未发出，草稿落盘失败，请检查磁盘后重试。"
		m.mu.Unlock()
		return
	}
	candidate := task.Candidate
	m.mu.Unlock()
	summary, err := m.ai.SummarizeResume(ctx, cfg, candidate)
	if err == nil {
		err = check(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil || err != nil {
		if task.Task.Status != "cancelled" {
			task.Task.Status = "failed"
		}
		task.Task.Error, task.Task.PossiblyCharged = "摘要没有完成或登录已变化，可能已计费；草稿保留，请核对后手动重试。", true
	} else {
		task.Task.Status, task.Task.Summary, task.Task.Error, task.Task.PossiblyCharged = "completed", hideSecrets(summary, token, cfg.APIKey), "", false
	}
	if err := m.persistLocked(task); err != nil {
		task.Task.Error = "本地草稿没落盘，请保留当前结果并检查磁盘。"
	}
}

// summaryCandidate 保留已提供的职业事实，移除联系方式、头像、旧摘要和来源授权，避免发出无关敏感字段。
func summaryCandidate(candidate cloud.LibraryCandidate) (cloud.LibraryCandidate, error) {
	candidate.ID, candidate.RowVersion, candidate.Phone, candidate.Email = "", 0, "", ""
	candidate.AISummary, candidate.SourceType, candidate.PlatformID, candidate.PlatformCandidateID, candidate.SourceURL = "", "", "", "", ""
	candidate.CandidateName, candidate.BirthYM, candidate.Gender, candidate.Age = "", "", "", nil
	candidate.LibraryStatus, candidate.OnlineStatus, candidate.ColleagueCommunications = "", "", nil
	raw, _ := json.Marshal(candidate)
	if len(raw) > 1<<20 || len([]rune(string(raw))) > 200000 || len(candidate.WorkExperiences) > 100 || len(candidate.Educations) > 100 || len(candidate.ProjectExperiences) > 100 || len(candidate.Certificates) > 100 || len(candidate.Honors) > 100 || len(candidate.Tags) > 30 {
		return candidate, fmt.Errorf("简历内容或经历太多，请精简后再生成摘要")
	}
	var fields any
	_ = json.Unmarshal(raw, &fields)
	if !summaryFieldsWithinLimit(fields, "") {
		return candidate, fmt.Errorf("某段简历内容太长，请精简后再生成摘要")
	}
	if strings.TrimSpace(candidate.RawText+candidate.BasicInfo+candidate.PersonalDescription+candidate.CurrentPosition+candidate.ExpectedPosition) == "" && len(candidate.WorkExperiences) == 0 && len(candidate.Educations) == 0 && len(candidate.ProjectExperiences) == 0 {
		return candidate, fmt.Errorf("先补一点经历或简历正文，我再生成摘要")
	}
	return candidate, nil
}

// summaryFieldsWithinLimit 将正文限制为15万字，单个职业字段限制为2万字，数组规模由上层统一限制。
func summaryFieldsWithinLimit(value any, key string) bool {
	switch item := value.(type) {
	case string:
		limit := 20000
		if key == "raw_text" {
			limit = 150000
		}
		return len([]rune(item)) <= limit
	case map[string]any:
		for name, child := range item {
			if !summaryFieldsWithinLimit(child, name) {
				return false
			}
		}
	case []any:
		for _, child := range item {
			if !summaryFieldsWithinLimit(child, key) {
				return false
			}
		}
	}
	return true
}

// finishStopped 将排队或配置阶段的中断保存为可手动重试状态，不让轮询永久停在排队中。
func (m *Manager) finishStopped(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	if task.Task.Status != "cancelled" {
		task.Task.Status = "failed"
	}
	task.Task.Error = "摘要已停止，请核对后手动重试。"
	task.Task.PossiblyCharged = task.Started
	_ = m.persistLocked(task)
}

// findLocked 将账号和安装范围作为权限边界，任务编号本身不提供访问权。
func (m *Manager) findLocked(owner, scope, id string) (*storedTask, error) {
	task := m.tasks[id]
	if task == nil || owner == "" || task.Owner != owner || task.Scope != scope {
		return nil, ErrNotFound
	}
	return task, nil
}

// persistLocked 写入现有SQLite设置表；存储模型中没有任何密钥或会话。
func (m *Manager) persistLocked(task *storedTask) error {
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return m.store.SetSetting(context.Background(), storageKey(task.Task.ID), string(raw))
}

// persistIndexLocked 保留草稿索引供本地程序重启后查阅。
func (m *Manager) persistIndexLocked() error {
	ids := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(ids)
	return m.store.SetSetting(context.Background(), indexKey, string(raw))
}

// storageKey 返回固定摘要前缀，避免碰到批量导入与其它本地配置。
func storageKey(id string) string { return "resume_summary_task_v1:" + id }

// digest 稳定计算请求隔离编号和内容指纹，不将明文安装信息放进键名。
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// hideSecrets 防止第三方输出意外回显本次会话或模型密钥。
func hideSecrets(value string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[凭证已隐藏]")
		}
	}
	return value
}
