// Package runtime 文件作用：验证 Windows 运行库缺失清单与非浏览器原生模块检查边界。
package runtime

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestMissingVisualCppLibrariesListsExactFiles 验证组件页能明确列出缺失的核心 DLL。
func TestMissingVisualCppLibrariesListsExactFiles(t *testing.T) {
	root := t.TempDir()
	system32 := filepath.Join(root, "System32")
	if err := os.MkdirAll(system32, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(system32, "vcruntime140.dll"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"msvcp140.dll", "vcruntime140_1.dll"}
	if got := missingVisualCppLibraries(root); !reflect.DeepEqual(got, want) {
		t.Fatalf("缺失清单不准确：got=%v want=%v", got, want)
	}
}

// TestSystemRuntimeStatusCacheRefresh 验证普通状态读取复用结果，显式重新检查会刷新缓存。
func TestSystemRuntimeStatusCacheRefresh(t *testing.T) {
	manager := &Manager{systemChecked: true, systemStatus: SystemRuntimeStatus{Required: true, Code: "OLD", Message: "旧结果"}}
	if got := manager.SystemRuntimeStatus(false); got.Code != "OLD" {
		t.Fatalf("没有复用缓存：%+v", got)
	}
	got := manager.SystemRuntimeStatus(true)
	if got.Code == "OLD" {
		t.Fatalf("重新检查没有刷新：%+v", got)
	}
}
