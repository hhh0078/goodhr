// Package profile 负责浏览器账号 Profile 路径、登录状态和运行占用。
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Manager 管理本机 Profile 目录和占用锁。
type Manager struct {
	root string
	mu   sync.Mutex
	used map[string]string
}

// New 创建 Profile 管理器。
func New(root string) *Manager {
	return &Manager{root: root, used: make(map[string]string)}
}

// Path 返回经过清理的 Profile 目录。
func (m *Manager) Path(profileID string) (string, error) {
	id := safeID(profileID)
	if id == "" {
		return "", fmt.Errorf("profile_id 不能为空")
	}
	return prepareProfilePath(filepath.Join(m.root, id))
}

// prepareProfilePath 创建 Profile 目录并补齐浏览器默认书签。
func prepareProfilePath(path string) (string, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("创建 Profile 目录失败：%w", err)
	}
	if err := cleanupStaleSingletons(path); err != nil {
		return "", err
	}
	if err := ensureProfileBookmarks(path); err != nil {
		return "", err
	}
	return path, nil
}

// cleanupStaleSingletons 仅在锁中记录的 Chromium 进程已经退出时清理三个残留单例文件。
func cleanupStaleSingletons(path string) error {
	lockPath := filepath.Join(path, "SingletonLock")
	info, err := os.Lstat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查 Profile 单例锁失败：%w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, err := os.Readlink(lockPath)
	if err != nil {
		return fmt.Errorf("读取 Profile 单例锁失败：%w", err)
	}
	pid := singletonPID(target)
	if pid <= 0 || processAlive(pid) {
		return nil
	}
	var cleanupErrors []error
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		if err = os.Remove(filepath.Join(path, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("清理 %s 失败：%w", name, err))
		}
	}
	if err = errors.Join(cleanupErrors...); err != nil {
		return fmt.Errorf("清理 Profile 残留单例文件失败：%w", err)
	}
	return nil
}

// singletonPID 从 Chromium 的 hostname-pid 单例锁目标中读取进程编号。
func singletonPID(target string) int {
	index := strings.LastIndex(strings.TrimSpace(target), "-")
	if index < 0 || index+1 >= len(target) {
		return 0
	}
	pid, err := strconv.Atoi(target[index+1:])
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// Resolve 把 Profile 编号或根目录内的绝对路径解析成安全目录。
func (m *Manager) Resolve(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "default"
	}
	if !filepath.IsAbs(value) {
		return m.Path(value)
	}
	root, err := filepath.Abs(m.root)
	if err != nil {
		return "", fmt.Errorf("读取 Profile 根目录失败：%w", err)
	}
	cleaned, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("读取 Profile 路径失败：%w", err)
	}
	relative, err := filepath.Rel(root, cleaned)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("Profile 路径必须位于 GoodHR 账号目录内")
	}
	return prepareProfilePath(cleaned)
}

// Acquire 为任务占用 Profile，防止同账号并发操作。
func (m *Manager) Acquire(profileID string, taskID string) error {
	id := safeID(profileID)
	if id == "" || strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("Profile 占用参数不完整")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner := m.used[id]; owner != "" && owner != taskID {
		return fmt.Errorf("Profile 正被任务 %s 使用", owner)
	}
	m.used[id] = taskID
	return nil
}

// Available 检查 Profile 是否未被其他任务占用。
func (m *Manager) Available(profileID string, taskID string) error {
	id := safeID(profileID)
	if id == "" {
		return fmt.Errorf("profile_id 不能为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner := m.used[id]; owner != "" && owner != taskID {
		return fmt.Errorf("Profile 正被任务 %s 使用", owner)
	}
	return nil
}

// Release 释放指定任务持有的 Profile。
func (m *Manager) Release(profileID string, taskID string) {
	id := safeID(profileID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used[id] == taskID {
		delete(m.used, id)
	}
}

// Exists 判断 Profile 目录是否已存在。
func (m *Manager) Exists(profileID string) bool {
	id := safeID(profileID)
	if id == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(m.root, id))
	return err == nil && info.IsDir()
}

// safeID 清理 Profile 编号中的路径字符。
func safeID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	if value == "." || value == ".." {
		return ""
	}
	return value
}
