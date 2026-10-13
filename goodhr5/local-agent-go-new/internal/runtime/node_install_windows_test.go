//go:build windows

// Package runtime 文件作用：验证 Windows 旧 Node 目录禁止重命名时仍可完成新安装。
package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// TestInstallNodeWithLockedLegacyDirectory 验证旧目录被占用时，原地安装不会执行目录替换。
func TestInstallNodeWithLockedLegacyDirectory(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	runtimeDir := t.TempDir()
	legacyPath := writeLegacyTestNode(t, runtimeDir, []byte("locked legacy"))
	path, err := syscall.UTF16PtrFromString(filepath.Dir(legacyPath))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	// 实际尝试重命名，确保本测试确实复现了 Windows 的目录占用限制。
	if err := os.Rename(filepath.Dir(legacyPath), filepath.Dir(legacyPath)+".backup"); err == nil {
		t.Fatal("测试锁没有阻止旧目录重命名")
	}
	manager := &Manager{runtimeDir: runtimeDir}
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, nodeTestExecutable(t)))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err != nil {
		t.Fatalf("旧目录被占用时安装失败：%v", err)
	}
	if err := manager.CheckNode(); err != nil {
		t.Fatalf("安装完成后新 Node 不可用：%v", err)
	}
}

// TestWindowsNodeRenameBlockedByOpenHandle 区分目录与子文件占用造成的移动失败，保留实际系统错误。
func TestWindowsNodeRenameBlockedByOpenHandle(t *testing.T) {
	for _, kind := range []string{"directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			runtimeDir := t.TempDir()
			sourceDir := filepath.Join(runtimeDir, ".node-install-fixture", "node-v22-test")
			if err := os.MkdirAll(sourceDir, 0o755); err != nil {
				t.Fatal(err)
			}
			lockPath := sourceDir
			flags := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
			if kind == "file" {
				lockPath = filepath.Join(sourceDir, "node.exe")
				flags = 0
				if err := os.WriteFile(lockPath, []byte("fixture"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			path, err := syscall.UTF16PtrFromString(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, flags, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.CloseHandle(handle)
			err = os.Rename(sourceDir, filepath.Join(runtimeDir, "node"))
			if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("占用应阻止移动，实际 %v", err)
			}
			t.Logf("已复现：%v", err)
		})
	}
}

// TestInstallNodeLockedStatePreservesPreviousRecord 验证记录文件被占用时不会覆盖旧记录或破坏旧 Node。
func TestInstallNodeLockedStatePreservesPreviousRecord(t *testing.T) {
	t.Setenv("GOODHR_TEST_NODE_VERSION", "v22.19.0")
	runtimeDir := t.TempDir()
	legacyPath := writeLegacyTestNode(t, runtimeDir, nodeTestExecutable(t))
	manager := &Manager{runtimeDir: runtimeDir}
	if err := manager.saveVersion("node_runtime", Asset{Version: "previous"}, filepath.ToSlash(filepath.Join("node", nodeBinaryName())), ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(manager.statePath())
	if err != nil {
		t.Fatal(err)
	}
	path, err := syscall.UTF16PtrFromString(manager.statePath())
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(path, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	asset, _ := serveNodeTestArchive(t, makeNodeTestArchive(t, nodeTestExecutable(t)))
	if err := manager.installAsset(context.Background(), "node_runtime", "Node 运行环境", "node", asset); err == nil {
		t.Fatal("记录被占用时不应报告成功")
	}
	after, err := os.ReadFile(manager.statePath())
	if err != nil || !bytes.Equal(before, after) || manager.NodePath() != legacyPath || manager.CheckNode() != nil {
		t.Fatalf("记录被占用时旧组件未保留：err=%v", err)
	}
}
