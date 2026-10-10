// 文件作用：提供账号独立的简历下载AI导入开关，默认开启，数据库持久化并短期缓存。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ResumeLibrarySettings 是简历库账号设置，未保存时采用默认开启。
type ResumeLibrarySettings struct {
	AutoImportDownloads bool       `json:"auto_import_downloads"`
	UpdatedAt           *time.Time `json:"updated_at,omitempty"`
}

// ResumeLibrarySettingsStore 只读写下载导入开关，不能覆盖其他用户配置。
type ResumeLibrarySettingsStore interface {
	Get(userEmail string) (ResumeLibrarySettings, error)
	Save(userEmail string, enabled bool) (ResumeLibrarySettings, error)
}

// MemoryResumeLibrarySettingsStore 为开发与测试提供账号隔离的设置存储。
type MemoryResumeLibrarySettingsStore struct {
	mu    sync.Mutex
	items map[string]ResumeLibrarySettings
	now   func() time.Time
}

// NewMemoryResumeLibrarySettingsStore 创建默认开启的设置存储。
func NewMemoryResumeLibrarySettingsStore() *MemoryResumeLibrarySettingsStore {
	return &MemoryResumeLibrarySettingsStore{items: map[string]ResumeLibrarySettings{}, now: time.Now}
}

// Get 返回账号设置，首次读取不写入数据库或自动关闭功能。
func (s *MemoryResumeLibrarySettingsStore) Get(email string) (ResumeLibrarySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, found := s.items[strings.ToLower(strings.TrimSpace(email))]
	if !found {
		item.AutoImportDownloads = true
	}
	return item, nil
}

// Save 仅保存当前账号的下载导入开关。
func (s *MemoryResumeLibrarySettingsStore) Save(email string, enabled bool) (ResumeLibrarySettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	item := ResumeLibrarySettings{AutoImportDownloads: enabled, UpdatedAt: &now}
	s.items[strings.ToLower(strings.TrimSpace(email))] = item
	return item, nil
}

// PostgresResumeLibrarySettingsStore 按账号外键持久化下载导入设置。
type PostgresResumeLibrarySettingsStore struct{ db *sql.DB }

// NewPostgresResumeLibrarySettingsStore 创建简历库设置数据库存储。
func NewPostgresResumeLibrarySettingsStore(db *sql.DB) *PostgresResumeLibrarySettingsStore {
	return &PostgresResumeLibrarySettingsStore{db: db}
}

// Get 查询账号开关，历史账号没有设置行时默认开启。
func (s *PostgresResumeLibrarySettingsStore) Get(email string) (ResumeLibrarySettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var item ResumeLibrarySettings
	var updated time.Time
	err := s.db.QueryRowContext(ctx, `SELECT rs.auto_import_downloads,rs.updated_at FROM resume_library_settings rs JOIN users u ON u.id=rs.user_id WHERE u.email=$1`, email).Scan(&item.AutoImportDownloads, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ResumeLibrarySettings{AutoImportDownloads: true}, nil
	}
	if err == nil {
		item.UpdatedAt = &updated
	}
	return item, err
}

// Save 复用账号查找并仅更新指定布尔字段，其他偏好设置不参与此表。
func (s *PostgresResumeLibrarySettingsStore) Save(email string, enabled bool) (ResumeLibrarySettings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	id, err := ensureUserID(ctx, s.db, email)
	if err != nil {
		return ResumeLibrarySettings{}, err
	}
	var item ResumeLibrarySettings
	var updated time.Time
	err = s.db.QueryRowContext(ctx, `INSERT INTO resume_library_settings(user_id,auto_import_downloads) VALUES($1::uuid,$2) ON CONFLICT(user_id) DO UPDATE SET auto_import_downloads=EXCLUDED.auto_import_downloads,updated_at=now() RETURNING auto_import_downloads,updated_at`, id, enabled).Scan(&item.AutoImportDownloads, &updated)
	if err == nil {
		item.UpdatedAt = &updated
	}
	return item, err
}

type resumeSettingsSnapshot struct {
	settings ResumeLibrarySettings
	expires  time.Time
}

// ResumeLibrarySettingsService 通过账号分片锁防止旧读取覆盖刚刚保存的开关。
type ResumeLibrarySettingsService struct {
	auth  *AuthService
	store ResumeLibrarySettingsStore
	locks [64]sync.Mutex
	mu    sync.Mutex
	cache map[string]resumeSettingsSnapshot
	now   func() time.Time
}

// NewResumeLibrarySettingsService 创建带60秒内存缓存的设置服务。
func NewResumeLibrarySettingsService(auth *AuthService, store ResumeLibrarySettingsStore) *ResumeLibrarySettingsService {
	return &ResumeLibrarySettingsService{auth: auth, store: store, cache: map[string]resumeSettingsSnapshot{}, now: time.Now}
}

// userLock 使用有限锁分片合并同一账号并发读取和修改。
func (s *ResumeLibrarySettingsService) userLock(email string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(email))
	return &s.locks[h.Sum32()%uint32(len(s.locks))]
}

// cacheSettings 保存最新设置并回收过期缓存账号。
func (s *ResumeLibrarySettingsService) cacheSettings(email string, item ResumeLibrarySettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.cache) >= 4096 {
		for key, value := range s.cache {
			if !value.expires.After(now) {
				delete(s.cache, key)
			}
		}
	}
	s.cache[email] = resumeSettingsSnapshot{item, now.Add(60 * time.Second)}
}

// getSettings 在持有账号锁时读取设置，使GET与PATCH保持顺序。
func (s *ResumeLibrarySettingsService) getSettings(email string) (ResumeLibrarySettings, error) {
	s.mu.Lock()
	snapshot, found := s.cache[email]
	s.mu.Unlock()
	if found && snapshot.expires.After(s.now()) {
		return snapshot.settings, nil
	}
	item, err := s.store.Get(email)
	if err == nil {
		s.cacheSettings(email, item)
	}
	return item, err
}

// User 读取或仅修改auto_import_downloads，拒绝受限插件凭证与未知字段。
func (s *ResumeLibrarySettingsService) User(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, "请使用GET或PATCH处理简历库设置")
		return
	}
	session, ok := privateAccountSession(s.auth, w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "设置服务还没准备好，请稍后再试")
		return
	}
	var enabled *bool
	if r.Method == http.MethodPatch {
		var input struct {
			AutoImportDownloads *bool `json:"auto_import_downloads"`
		}
		if !decodePrivateJSON(w, r, &input) {
			return
		}
		if input.AutoImportDownloads == nil {
			writeError(w, http.StatusBadRequest, "请明确选择是否导入下载的简历")
			return
		}
		enabled = input.AutoImportDownloads
	}
	lock := s.userLock(session.Email)
	lock.Lock()
	defer lock.Unlock()
	var item ResumeLibrarySettings
	var err error
	if enabled != nil {
		item, err = s.store.Save(session.Email, *enabled)
		if err == nil {
			s.cacheSettings(session.Email, item)
		}
	} else {
		item, err = s.getSettings(session.Email)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "简历库设置没保存好，请稍后再试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "settings": item})
}
