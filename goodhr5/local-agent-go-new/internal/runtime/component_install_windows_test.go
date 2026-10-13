//go:build windows

// Package runtime 文件作用：复现 Windows 浏览器文件占用引起的 Access is denied，验证新目录登记与旧目录占用安装。
package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// lockComponentTestPath 用不允许删除共享的 Windows 句柄模拟文件扫描或目录占用，退出测试时释放。
func lockComponentTestPath(t *testing.T, path string, directory bool) {
	t.Helper()
	value, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	flags := uint32(0)
	if directory {
		flags = syscall.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := syscall.CreateFile(value, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(handle) })
}

// TestWindowsRegisterOccupiedBrowser 验证新浏览器子文件被占用时旧移动方式失败，而原地检查和登记可以成功。
func TestWindowsRegisterOccupiedBrowser(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	root := t.TempDir()
	componentRoot := filepath.Join(root, "camoufox-runtime-fixture")
	writeComponentTestFiles(t, componentRoot, componentTestFiles("camoufox", "browser"))
	binary := findCamoufoxBinary(componentRoot, "windows")
	lockComponentTestPath(t, binary, false)
	err := os.Rename(componentRoot, filepath.Join(root, "camoufox"))
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		t.Fatalf("未复现截图中的拒绝移动：%v", err)
	}
	t.Logf("复现旧流程错误：%v", err)
	path, err := checkExtractedComponent(context.Background(), "camoufox", componentRoot)
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{runtimeDir: root}
	if err := manager.writeCamoufoxVersionFile(componentRoot, "152.0.4-beta.30"); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.saveVersion("camoufox", Asset{Version: "152.0.4-beta.30"}, filepath.ToSlash(relative), "camoufox-runtime-fixture"); err != nil {
		t.Fatal(err)
	}
	if manager.CamoufoxPath() != binary || manager.CamoufoxDir() != componentRoot {
		t.Fatal("被占用的新浏览器未能原地登记")
	}
}

// TestWindowsInstallComponentsWithOccupiedLegacy 验证旧浏览器和 OCR 目录被占用时完整安装仍成功并保留旧文件。
func TestWindowsInstallComponentsWithOccupiedLegacy(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, component := range []string{"camoufox", "ocr"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			legacy := filepath.Join(root, component)
			writeComponentTestFiles(t, legacy, componentTestFiles(component, "legacy"))
			manager := &Manager{runtimeDir: root}
			oldPath := componentTestPath(manager, component)
			lockComponentTestPath(t, legacy, true)
			if err := os.Rename(legacy, legacy+".backup"); err == nil {
				t.Fatal("测试句柄没有阻止目录移动")
			}
			asset, _ := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles(component, "new")))
			if err := manager.installAsset(context.Background(), component, component, component, asset); err != nil {
				t.Fatal(err)
			}
			if content, err := os.ReadFile(oldPath); err != nil || string(content) != "legacy" || componentTestPath(manager, component) == oldPath {
				t.Fatalf("旧目录被占用时未保留旧组件并启用新组件：err=%v", err)
			}
		})
	}
}

// TestWindowsComponentRecordLockedPreservesPrevious 验证版本文件被占用时浏览器与 OCR 安装失败仍保留原记录和路径。
func TestWindowsComponentRecordLockedPreservesPrevious(t *testing.T) {
	t.Setenv("CAMOUFOX_BINARY_PATH", "")
	for _, component := range []string{"camoufox", "ocr"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			writeComponentTestFiles(t, filepath.Join(root, component), componentTestFiles(component, "legacy"))
			manager := &Manager{runtimeDir: root}
			oldPath := componentTestPath(manager, component)
			if err := manager.saveVersion(component, Asset{Version: "previous"}, "", ""); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(manager.statePath())
			if err != nil {
				t.Fatal(err)
			}
			lockComponentTestPath(t, manager.statePath(), false)
			asset, _ := serveNodeTestArchive(t, makeComponentTestArchive(t, componentTestFiles(component, "new")))
			if err := manager.installAsset(context.Background(), component, component, component, asset); err == nil {
				t.Fatal("版本文件被占用时不能报告成功")
			}
			after, err := os.ReadFile(manager.statePath())
			if err != nil || !bytes.Equal(before, after) || componentTestPath(manager, component) != oldPath {
				t.Fatalf("版本记录失败破坏了旧组件：err=%v", err)
			}
			remaining, err := filepath.Glob(filepath.Join(root, component+"-runtime-*"))
			if err != nil || len(remaining) != 0 {
				t.Fatalf("登记失败仍留下新目录：%v err=%v", remaining, err)
			}
		})
	}
}
