// Package profile 文件作用：验证 Profile 编号和绝对路径都不能逃出账号根目录。
package profile

import (
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
