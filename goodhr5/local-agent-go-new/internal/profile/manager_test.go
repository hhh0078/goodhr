// Package profile 文件作用：验证 Profile 编号和绝对路径都不能逃出账号根目录。
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveAcceptsIDAndContainedPath 验证编号和根目录内绝对路径可以安全创建。
func TestResolveAcceptsIDAndContainedPath(t *testing.T) {
	root := t.TempDir()
	manager := New(root)
	byID, err := manager.Resolve("account-1")
	if err != nil || byID != filepath.Join(root, "account-1") {
		t.Fatalf("Resolve(id) = %q, %v", byID, err)
	}
	contained := filepath.Join(root, "account-2")
	byPath, err := manager.Resolve(contained)
	if err != nil || byPath != contained {
		t.Fatalf("Resolve(path) = %q, %v", byPath, err)
	}
}

// TestResolveRejectsOutsidePath 验证绝对路径不能逃出账号根目录。
func TestResolveRejectsOutsidePath(t *testing.T) {
	manager := New(filepath.Join(t.TempDir(), "profiles"))
	if _, err := manager.Resolve(filepath.Join(t.TempDir(), "outside")); err == nil {
		t.Fatal("Resolve() accepted path outside profile root")
	}
}

// TestCleanupStaleSingletons 验证已退出进程留下的 Chromium 单例文件会一起清理。
func TestCleanupStaleSingletons(t *testing.T) {
	profilePath := t.TempDir()
	if err := os.Symlink("test-host-99999999", filepath.Join(profilePath, "SingletonLock")); err != nil {
		t.Fatalf("创建测试锁失败：%v", err)
	}
	for _, name := range []string{"SingletonCookie", "SingletonSocket"} {
		if err := os.WriteFile(filepath.Join(profilePath, name), []byte("stale"), 0o600); err != nil {
			t.Fatalf("创建测试单例文件失败：%v", err)
		}
	}
	if err := cleanupStaleSingletons(profilePath); err != nil {
		t.Fatalf("清理残留单例文件失败：%v", err)
	}
	for _, name := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		if _, err := os.Lstat(filepath.Join(profilePath, name)); !os.IsNotExist(err) {
			t.Fatalf("%s 没有被清理", name)
		}
	}
}

// TestCleanupStaleSingletonsKeepsLiveProcess 验证仍存活进程的 Chromium 单例锁不会被误删。
func TestCleanupStaleSingletonsKeepsLiveProcess(t *testing.T) {
	profilePath := t.TempDir()
	lockPath := filepath.Join(profilePath, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("test-host-%d", os.Getpid()), lockPath); err != nil {
		t.Fatalf("创建测试锁失败：%v", err)
	}
	if err := cleanupStaleSingletons(profilePath); err != nil {
		t.Fatalf("检查存活单例锁失败：%v", err)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatalf("存活进程的单例锁被误删：%v", err)
	}
}
