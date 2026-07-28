// Package runtime 文件作用：验证运行组件校验、平台选择和解压路径安全规则。
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestVerifySHA256 验证正确校验值通过、错误校验值失败。
func TestVerifySHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.zip")
	content := []byte("goodhr-runtime")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifySHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("verifySHA256() error = %v", err)
	}
	if err := verifySHA256(path, "bad"); err == nil {
		t.Fatal("verifySHA256() accepted wrong digest")
	}
}

// TestSafeJoinRejectsTraversal 验证组件压缩包不能越界写文件。
func TestSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := safeJoin(t.TempDir(), "../outside"); err == nil {
		t.Fatal("safeJoin() accepted traversal path")
	}
}

// TestWorkerDependencyPathFindsParentNodeModules 验证 Worker 编译入口可以向父目录找到 CloakBrowser 依赖。
func TestWorkerDependencyPathFindsParentNodeModules(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "worker", "dist", "main.js")
	dependency := filepath.Join(root, "worker", "node_modules", "cloakbrowser", "package.json")
	for _, path := range []string{entry, dependency} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manager := &Manager{entryPath: entry}
	if got := manager.WorkerDependencyPath(); got != dependency {
		t.Fatalf("WorkerDependencyPath() = %s, want %s", got, dependency)
	}
	if err := manager.CheckWorkerBuild(); err != nil {
		t.Fatalf("CheckWorkerBuild() error = %v", err)
	}
}
