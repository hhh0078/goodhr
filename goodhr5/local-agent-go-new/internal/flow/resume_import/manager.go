// Package resume_import 文件作用：持久保存本地解析任务，以最多两个工作者处理，支持账号隔离、取消和重启恢复。
package resume_import

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const taskIndexKey = "resume_import_tasks_v1"

// Manager 编排本地导入；令牌只存在每次运行的内存参数中。
type Manager struct {
	mu        sync.Mutex
	store     SettingsStore
	cloud     CloudAPI
	parser    ResumeParser
	extractor TextExtractor
	root      string
	tasks     map[string]*storedTask
	active    map[string]context.CancelFunc
	saving    map[string]bool
	slots     chan struct{}
	wg        sync.WaitGroup
	closed    bool
}

// New 创建导入管理器并把上次中断项恢复为排队；验证账号后才恢复调用AI。
func New(store SettingsStore, cloudClient CloudAPI, parser ResumeParser, extractor TextExtractor, root string) (*Manager, error) {
	if store == nil || cloudClient == nil || parser == nil || extractor == nil || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("本地解析能力还没准备好")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	m := &Manager{store: store, cloud: cloudClient, parser: parser, extractor: extractor, root: root,
		tasks: map[string]*storedTask{}, active: map[string]context.CancelFunc{}, saving: map[string]bool{}, slots: make(chan struct{}, 2)}
	raw, err := store.GetSetting(context.Background(), taskIndexKey)
	if err != nil {
		return nil, err
	}
	var ids []string
	if raw != "" && json.Unmarshal([]byte(raw), &ids) != nil {
		return nil, fmt.Errorf("导入草稿索引暂时读不出来，请保留本地数据后重试")
	}
	for _, id := range ids {
		data, err := store.GetSetting(context.Background(), taskStorageKey(id))
		if err != nil {
			return nil, err
		}
		var stored storedTask
		if data == "" || json.Unmarshal([]byte(data), &stored) != nil {
			return nil, fmt.Errorf("导入草稿%s暂时读不出来", id)
		}
		if stored.Task.ID != id || stored.OwnerID == "" {
			return nil, fmt.Errorf("导入草稿记录不完整")
		}
		if stored.Confirmations == nil {
			stored.Confirmations = map[string]*confirmation{}
		}
		if stored.AIStarted == nil {
			stored.AIStarted = map[string]bool{}
		}
		for i := range stored.Task.Items {
			if stored.Task.Items[i].Status == "parsing" || stored.Task.Items[i].Status == "queued" {
				stored.Task.Items[i].Status = "queued"
				if stored.AIStarted[stored.Task.Items[i].ID] {
					stored.Task.Items[i].Status, stored.Task.Items[i].Error = "failed", "上次AI请求中断，可能已计费。草稿保留，请手动确认后重试"
				}
			}
		}
		m.tasks[id] = &stored
		m.refreshLocked(&stored)
		if err := m.persistLocked(&stored); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Create 接收用户文件流并先落盘再异步解析，不阻塞HTTP等待AI完成。
func (m *Manager) Create(ctx context.Context, owner, token string, uploads []Upload, text string) (Task, error) {
	if owner == "" || token == "" || len(uploads) > MaxFiles || len(uploads) == 0 && strings.TrimSpace(text) == "" {
		return Task{}, ErrInvalid
	}
	id, err := randomID()
	if err != nil {
		return Task{}, err
	}
	dir := filepath.Join(m.root, id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Task{}, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	if text = strings.TrimSpace(text); text != "" {
		if len(text) > MaxFileBytes {
			return Task{}, fmt.Errorf("粘贴文字超过10MB，请分开导入")
		}
		uploads = append(append([]Upload{}, uploads...), Upload{Name: "粘贴的简历.txt", Reader: strings.NewReader(text)})
	}
	if len(uploads) > MaxFiles {
		return Task{}, fmt.Errorf("每批最多20份，请分开导入")
	}
	stored := &storedTask{Task: Task{ID: id, Status: "running", Items: []Item{}}, OwnerID: owner,
		Paths: map[string]string{}, Confirmations: map[string]*confirmation{}, AIStarted: map[string]bool{}}
	stored.CreatedAt = time.Now().UnixNano()
	total := int64(0)
	for _, upload := range uploads {
		if upload.Reader == nil {
			return Task{}, ErrInvalid
		}
		if err := ctx.Err(); err != nil {
			return Task{}, err
		}
		name := safeFileName(upload.Name)
		ext := strings.ToLower(filepath.Ext(name))
		if !supportedExtension(ext) {
			return Task{}, fmt.Errorf("%s暂不支持，请选择PDF、DOCX、TXT、PNG或JPEG", name)
		}
		itemID, err := randomID()
		if err != nil {
			return Task{}, err
		}
		path := filepath.Join(dir, itemID+ext)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return Task{}, err
		}
		n, copyErr := io.Copy(file, io.LimitReader(upload.Reader, MaxFileBytes+1))
		closeErr := file.Close()
		if copyErr != nil {
			return Task{}, copyErr
		}
		if closeErr != nil {
			return Task{}, closeErr
		}
		total += n
		if n == 0 || n > MaxFileBytes || total > MaxTotalBytes {
			return Task{}, fmt.Errorf("文件为空或超过限制：每份10MB，每批60MB")
		}
		stored.Paths[itemID] = path
		stored.Task.Items = append(stored.Task.Items, Item{ID: itemID, FileName: name, Status: "queued"})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Task{}, fmt.Errorf("本地程序正在退出，请稍后再试")
	}
	m.tasks[id] = stored
	m.refreshLocked(stored)
	if err := m.persistLocked(stored); err != nil {
		delete(m.tasks, id)
		return Task{}, err
	}
	if err := m.persistIndexLocked(); err != nil {
		delete(m.tasks, id)
		return Task{}, err
	}
	keep = true
	m.startLocked(stored, token)
	return cloneTask(stored.Task), nil
}

// Get 只允许原账号读取任务，并以本次验证过的Token恢复上次中断项。
func (m *Manager) Get(owner, token, id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		return Task{}, err
	}
	m.startLocked(stored, token)
	return cloneTask(stored.Task), nil
}

// List 返回当前账号的历史草稿批次，不泄露其它账号或本地路径。
func (m *Manager) List(owner, token string) []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Task{}
	for _, stored := range m.tasks {
		if stored.OwnerID == owner {
			m.startLocked(stored, token)
			result = append(result, cloneTask(stored.Task))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := m.tasks[result[i].ID], m.tasks[result[j].ID]
		if left.CreatedAt == right.CreatedAt {
			return result[i].ID < result[j].ID
		}
		return left.CreatedAt > right.CreatedAt
	})
	return result
}

// Cancel 停止当前批次的AI调用并保留已解析和已保存草稿。
func (m *Manager) Cancel(owner, id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		return Task{}, err
	}
	stored.Task.Status = "cancelled"
	if cancel := m.active[id]; cancel != nil {
		cancel()
	}
	for i := range stored.Task.Items {
		if stored.Task.Items[i].Status == "parsing" {
			stored.Task.Items[i].Status = "queued"
			if stored.AIStarted[stored.Task.Items[i].ID] {
				stored.Task.Items[i].Status, stored.Task.Items[i].Error = "failed", "AI请求已停止，可能已计费。草稿保留，请手动确认后重试"
			}
		}
	}
	m.refreshLocked(stored)
	return cloneTask(stored.Task), m.persistLocked(stored)
}

// Retry 只重试明确选中的失败或排队项，已解析和已入库项不会重复AI解析。
func (m *Manager) Retry(owner, token, id string, itemIDs []string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, err := m.findLocked(owner, id)
	if err != nil {
		return Task{}, err
	}
	if m.active[id] != nil {
		return Task{}, ErrBusy
	}
	if len(itemIDs) == 0 {
		return Task{}, ErrInvalid
	}
	for _, itemID := range itemIDs {
		item := findItem(stored, itemID)
		if item == nil {
			return Task{}, ErrInvalid
		}
		if m.saving[itemID] || item.Status == "parsing" {
			return Task{}, ErrBusy
		}
		if item.Status != "failed" && item.Status != "queued" {
			return Task{}, fmt.Errorf("已解析草稿请直接审核保存，不能重新扣费解析")
		}
		if stored.Confirmations[itemID] != nil {
			return Task{}, fmt.Errorf("这条简历已进入确认保存，请继续确认而非重做AI解析")
		}
	}
	for _, itemID := range itemIDs {
		item := findItem(stored, itemID)
		item.Status, item.Error = "queued", ""
		delete(stored.AIStarted, itemID)
	}
	stored.Task.Status = "running"
	m.refreshLocked(stored)
	if err := m.persistLocked(stored); err != nil {
		return Task{}, err
	}
	m.startLocked(stored, token)
	return cloneTask(stored.Task), nil
}

// CancelActive 完整网页登录被替换时停止后台解析，已解析结果仍按原账号保留。
func (m *Manager) CancelActive() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cancel := range m.active {
		cancel()
	}
}

// Close 取消全部后台解析并等待状态落盘，关闭程序时不丢失草稿。
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

// startLocked 为每批只创建一个调度器，所有批次共享最多两个并发槽位。
func (m *Manager) startLocked(stored *storedTask, token string) {
	if m.closed || token == "" || stored.Task.Status == "cancelled" || m.active[stored.Task.ID] != nil {
		return
	}
	hasQueued := false
	for _, item := range stored.Task.Items {
		hasQueued = hasQueued || item.Status == "queued"
	}
	if !hasQueued {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.active[stored.Task.ID] = cancel
	m.wg.Add(1)
	go m.run(ctx, stored.Task.ID, token)
}

// run 平铺调度提取与AI两步骤，取消后保留排队项以便再次恢复。
func (m *Manager) run(ctx context.Context, id, token string) {
	defer m.wg.Done()
	defer func() { m.mu.Lock(); delete(m.active, id); m.mu.Unlock() }()
	var workers sync.WaitGroup
	for {
		select {
		case m.slots <- struct{}{}:
		case <-ctx.Done():
			workers.Wait()
			return
		}
		m.mu.Lock()
		stored := m.tasks[id]
		var item *Item
		if ctx.Err() == nil && stored.Task.Status != "cancelled" {
			for i := range stored.Task.Items {
				if stored.Task.Items[i].Status == "queued" {
					item = &stored.Task.Items[i]
					break
				}
			}
		}
		if item == nil {
			m.mu.Unlock()
			<-m.slots
			workers.Wait()
			return
		}
		item.Status, item.Error = "parsing", ""
		itemID, path := item.ID, stored.Paths[item.ID]
		if err := m.persistLocked(stored); err != nil {
			item.Status, item.Error = "failed", "草稿落盘失败，请检查磁盘后重试"
			m.refreshLocked(stored)
			m.mu.Unlock()
			<-m.slots
			continue
		}
		m.mu.Unlock()
		workers.Add(1)
		go func() { defer workers.Done(); defer func() { <-m.slots }(); m.parseItem(ctx, id, itemID, path, token) }()
	}
}

// parseItem 执行一次文件提取、获取AI配置和结构化解析，并在每次状态变化后落盘。
func (m *Manager) parseItem(ctx context.Context, taskID, itemID, path, token string) {
	text, err := m.extractor.Extract(ctx, path)
	var candidate cloud.LibraryCandidate
	var aiKey string
	if err == nil {
		cfg, configErr := m.cloud.EffectiveAI(ctx, token)
		aiKey = cfg.APIKey
		err = configErr
		if err == nil {
			m.mu.Lock()
			stored := m.tasks[taskID]
			stored.AIStarted[itemID] = true
			err = m.persistLocked(stored)
			m.mu.Unlock()
			if err == nil {
				candidate, err = m.parser.ParseResume(ctx, cfg, text)
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored := m.tasks[taskID]
	item := findItem(stored, itemID)
	if ctx.Err() != nil {
		item.Status, item.Error = "queued", ""
		if stored.AIStarted[itemID] {
			item.Status, item.Error = "failed", "AI请求已停止，可能已计费。草稿保留，请手动确认后重试"
		}
	} else if err != nil {
		item.Status, item.Error = "failed", safeCredentialError(err, token, aiKey)
	} else {
		candidate.Phone = NormalizePhone(candidate.Phone)
		item.Candidate, item.Status, item.Error = candidate, "ready", ""
		if candidate.Phone == "" {
			item.Status = "needs_phone"
		}
	}
	m.refreshLocked(stored)
	if err := m.persistLocked(stored); err != nil {
		item.Status, item.Error = "failed", "草稿落盘失败，请检查磁盘后重试"
	}
}

// findLocked 校验存储中的账号归属，任务编号不能替代权限。
func (m *Manager) findLocked(owner, id string) (*storedTask, error) {
	stored := m.tasks[id]
	if stored == nil || owner == "" || stored.OwnerID != owner {
		return nil, ErrNotFound
	}
	return stored, nil
}

// persistLocked 将结构化草稿和原件索引写入SQLite，Token不在存储模型中。
func (m *Manager) persistLocked(stored *storedTask) error {
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return m.store.SetSetting(context.Background(), taskStorageKey(stored.Task.ID), string(encoded))
}

// persistIndexLocked 保存批次索引供重启恢复。
func (m *Manager) persistIndexLocked() error {
	ids := make([]string, 0, len(m.tasks))
	for id := range m.tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	encoded, _ := json.Marshal(ids)
	return m.store.SetSetting(context.Background(), taskIndexKey, string(encoded))
}

// refreshLocked 更新批次计数；缺手机号和解析失败都是已处理但未保存项。
func (m *Manager) refreshLocked(stored *storedTask) {
	stored.Task.Total, stored.Task.Processed = len(stored.Task.Items), 0
	for _, item := range stored.Task.Items {
		if item.Status != "queued" && item.Status != "parsing" {
			stored.Task.Processed++
		}
	}
	if stored.Task.Status != "cancelled" {
		stored.Task.Status = "running"
		if stored.Task.Processed == stored.Task.Total {
			stored.Task.Status = "completed"
		}
	}
}

// cloneTask 脱离内部切片与指针，避免响应序列化时与后台更新竞争。
func cloneTask(task Task) Task {
	raw, _ := json.Marshal(task)
	var cloned Task
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

// findItem 在一个已验证的批次内查找条目。
func findItem(stored *storedTask, id string) *Item {
	for i := range stored.Task.Items {
		if stored.Task.Items[i].ID == id {
			return &stored.Task.Items[i]
		}
	}
	return nil
}

// randomID 生成只能作为安全目录名使用的随机编号。
func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// taskStorageKey 生成固定前缀的草稿键名。
func taskStorageKey(id string) string { return "resume_import_task_v1:" + id }

// safeFileName 只保留文件名用于展示，原路径不控制本地落盘位置。
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if len(name) > 250 {
		name = "简历" + filepath.Ext(name)
	}
	return name
}

// supportedExtension 限制已实现的导入格式，旧DOC不假装当成DOCX解析。
func supportedExtension(ext string) bool {
	switch ext {
	case ".pdf", ".docx", ".txt", ".png", ".jpg", ".jpeg":
		return true
	}
	return false
}

// safeError 隐藏原始AI响应和底层请求参数，防止错误字段泄露凭证。
func safeError(err error) string {
	text := err.Error()
	if len([]rune(text)) > 500 {
		return "这份简历暂时没解析成功，请检查文件后重试"
	}
	return text
}

// safeCredentialError 从持久错误文案移除本次使用的凭证，第三方错误不能把Token写进草稿。
func safeCredentialError(err error, secrets ...string) string {
	text := safeError(err)
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[凭证已隐藏]")
		}
	}
	return text
}
