// Package resume_download 文件作用：为当前账号持久排队新招聘下载，复用AI导入并幂等通知，凭证仅在内存保留。
package resume_download

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	resumeimport "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
)

const stateKey = "resume_download_queue_v1"

// CloudAPI 只提供会话、开关、查重和站内消息，测试不调用真实AI或账号。
type CloudAPI interface {
	ValidateSession(context.Context, string) (cloud.UserSession, error)
	ResumeDownloadSettings(context.Context, string) (cloud.ResumeLibrarySettings, error)
	LibraryCandidateHasDuplicate(context.Context, string, cloud.LibraryCandidate) (bool, error)
	PushResumeMessage(context.Context, string, string, string, string, string) error
}

// ImportAPI 复用已有导入任务，已付费失败不会在自动队列里重新解析。
type ImportAPI interface {
	CreateDownload(context.Context, string, string, resumeimport.Upload, string, func(context.Context) error) (resumeimport.Task, error)
	AuthorizeDownload(string, string, func(context.Context) error) error
	Get(string, string, string) (resumeimport.Task, error)
	Cancel(string, string) (resumeimport.Task, error)
	Confirm(context.Context, string, string, string, []resumeimport.ConfirmItem) (resumeimport.Task, error)
}

// entry 记录文件来源、所属账号和通知收据，不持久化Token。
type entry struct {
	ID        string `json:"id"`
	Owner     string `json:"owner"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Platform  string `json:"platform"`
	SourceURL string `json:"source_url"`
	Hash      string `json:"hash,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Notified  bool   `json:"notified"`
}

// Manager 每秒推进本地队列，30秒刷新账号开关；切号时立即取消旧会话操作。
type Manager struct {
	mu                           sync.Mutex
	workMu                       sync.Mutex
	store                        resumeimport.SettingsStore
	cloud                        CloudAPI
	imports                      ImportAPI
	entries                      map[string]entry
	owner, token                 string
	generation                   uint64
	started, boundAt, settingsAt time.Time
	enabled, known               bool
	session                      context.Context
	cancelSession                context.CancelFunc
	cancelRun                    context.CancelFunc
	done                         chan struct{}
}

// New 恢复本地队列，只在重新绑定原账号后继续，无历史补扫和凭证落盘。
func New(store resumeimport.SettingsStore, client CloudAPI, imports ImportAPI) (*Manager, error) {
	if store == nil || client == nil || imports == nil {
		return nil, fmt.Errorf("下载导入能力尚未准备好")
	}
	m := &Manager{store: store, cloud: client, imports: imports, entries: map[string]entry{}, started: time.Now(), enabled: true}
	raw, err := store.GetSetting(context.Background(), stateKey)
	if err != nil {
		return nil, err
	}
	if raw != "" && json.Unmarshal([]byte(raw), &m.entries) != nil {
		return nil, fmt.Errorf("下载导入队列暂时读不出来，请保留本地数据后重试")
	}
	if m.entries == nil {
		m.entries = map[string]entry{}
	}
	return m, nil
}

// Start 启动串行后台推进，不阻塞下载监控和网页操作。
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancelRun != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelRun = cancel
	m.done = make(chan struct{})
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.Tick(ctx); err != nil && ctx.Err() == nil {
					log.Printf("[下载简历导入] 本轮稍后重试 err=%v", err)
				}
			}
		}
	}()
}

// Close 停止后台队列并等待当前步骤收尾，原草稿和通知收据都保留。
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.cancelSession != nil {
		m.cancelSession()
	}
	if m.cancelRun != nil {
		m.cancelRun()
	}
	done := m.done
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Bind 设置本次已验证网页登录，刷新同账号凭证不把既有下载改归他人。
func (m *Manager) Bind(owner, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner == owner && m.token == token {
		return
	}
	if m.cancelSession != nil {
		m.cancelSession()
	}
	if m.owner != owner {
		m.boundAt = time.Now()
		m.settingsAt = time.Time{}
		m.known = false
	}
	m.owner, m.token = owner, token
	m.generation++
	m.session, m.cancelSession = context.WithCancel(context.Background())
}

// Observe 仅接收本次启动和账号绑定后开始的新下载，关掉开关的下载不会日后补扫。
func (m *Manager) Observe(ctx context.Context, records []contract.DownloadRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner == "" || m.token == "" {
		return nil
	}
	changed := false
	for _, record := range records {
		if _, exists := m.entries[record.ID]; exists || record.ID == "" || record.Status != "saved" {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if err != nil || created.Before(m.started) || created.Before(m.boundAt) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(record.FilePath))
		if ext != ".pdf" && ext != ".doc" && ext != ".docx" {
			continue
		}
		platformID, sourceURL := platform.DownloadSource(record.PageURL, record.URL)
		if platformID == "" {
			continue
		}
		status := "queued"
		if m.known && !m.enabled {
			status = "disabled"
		}
		m.entries[record.ID] = entry{ID: record.ID, Owner: m.owner, Path: record.FilePath, Name: filepath.Base(record.FilePath), Platform: platformID, SourceURL: sourceURL, Status: status}
		changed = true
	}
	if changed {
		return m.persistLocked(ctx)
	}
	return nil
}

// RefreshSettings 用云端最新结果立即应用开关，网页保存后可调用此接口。
func (m *Manager) RefreshSettings(ctx context.Context) error {
	m.mu.Lock()
	owner, token, generation := m.owner, m.token, m.generation
	m.mu.Unlock()
	if owner == "" || token == "" {
		return fmt.Errorf("先连接GoodHR账号，再同步下载设置")
	}
	settings, err := m.cloud.ResumeDownloadSettings(ctx, token)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if generation != m.generation {
		m.mu.Unlock()
		return fmt.Errorf("账号已变化，请重新同步设置")
	}
	if m.known && m.enabled != settings.AutoImportDownloads {
		m.generation++
	}
	m.enabled, m.known, m.settingsAt = settings.AutoImportDownloads, true, time.Now()
	// 关闭后只取消自动推进，已有AI草稿保留，重新打开不会重读关闭期间的下载。
	cancelIDs := []string{}
	if !m.enabled {
		for id, item := range m.entries {
			if item.Owner == owner && item.Status == "queued" {
				item.Status = "disabled"
				m.entries[id] = item
			}
			if item.Owner == owner && item.Status == "parsing" && item.TaskID != "" {
				cancelIDs = append(cancelIDs, item.TaskID)
			}
		}
	}
	err = m.persistLocked(ctx)
	m.mu.Unlock()
	for _, id := range cancelIDs {
		_, _ = m.imports.Cancel(owner, id)
	}
	return err
}

// Tick 串行推进所有属于当前账号的条目，网络故障仅重试无付费步骤。
func (m *Manager) Tick(ctx context.Context) error {
	m.workMu.Lock()
	defer m.workMu.Unlock()
	m.mu.Lock()
	owner, token, generation, session := m.owner, m.token, m.generation, m.session
	refresh := !m.known || time.Since(m.settingsAt) >= 30*time.Second
	m.mu.Unlock()
	if owner == "" || token == "" || session == nil {
		return nil
	}
	if refresh {
		if err := m.RefreshSettings(ctx); err != nil {
			return err
		}
	}
	m.mu.Lock()
	enabled := m.enabled
	owner, token, generation, session = m.owner, m.token, m.generation, m.session
	m.mu.Unlock()
	if !enabled {
		return nil
	}
	workCtx, cancel := context.WithTimeout(session, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	check := m.accessCheck(owner, token, generation)
	m.mu.Lock()
	items := []entry{}
	for _, item := range m.entries {
		if item.Owner == owner && item.Status != "saved" && item.Status != "disabled" && item.Status != "duplicate_file" && (item.Status == "queued" || item.Status == "parsing" || !item.Notified) {
			items = append(items, item)
		}
	}
	m.mu.Unlock()
	if len(items) == 0 {
		return nil
	}
	if err := check(workCtx); err != nil {
		return err
	}
	for _, item := range items {
		if err := workCtx.Err(); err != nil {
			return err
		}
		if err := m.process(workCtx, token, item, check); err != nil {
			return err
		}
	}
	return nil
}

// accessCheck 在AI与入库前验证内存账号代次、开关和云端当前会话。
func (m *Manager) accessCheck(owner, token string, generation uint64) func(context.Context) error {
	return func(ctx context.Context) error {
		m.mu.Lock()
		valid := m.owner == owner && m.token == token && m.generation == generation && m.known && m.enabled
		m.mu.Unlock()
		if !valid {
			return fmt.Errorf("账号或下载设置已变化，草稿仍保留")
		}
		session, err := m.cloud.ValidateSession(ctx, token)
		if err != nil {
			return fmt.Errorf("下载导入登录验证未完成，请重新连接GoodHR")
		}
		if session.UserID != owner || session.SessionKind == "browser_extension" {
			return fmt.Errorf("下载导入账号已变化，请重新连接GoodHR")
		}
		m.mu.Lock()
		valid = m.owner == owner && m.token == token && m.generation == generation && m.known && m.enabled
		m.mu.Unlock()
		if !valid {
			return fmt.Errorf("账号或下载设置已变化，草稿仍保留")
		}
		return nil
	}
}

// update 保存每步结果；先落盘再进行下一步网络操作。
func (m *Manager) update(ctx context.Context, item entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.entries[item.ID]; ok && current.Status == "disabled" && item.Status != "disabled" {
		return fmt.Errorf("下载开关已关闭，这份文件不再自动处理")
	}
	m.entries[item.ID] = item
	return m.persistLocked(ctx)
}

// persistLocked 原子替换SQLite队列JSON，登录凭证和网页正文不进入此索引。
func (m *Manager) persistLocked(ctx context.Context) error {
	raw, err := json.Marshal(m.entries)
	if err != nil {
		return err
	}
	return m.store.SetSetting(ctx, stateKey, string(raw))
}
