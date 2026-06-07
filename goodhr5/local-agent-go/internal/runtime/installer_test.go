// Package runtime 负责测试运行组件安装器的安全边界。
package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go/internal/config"
)

// TestSafeJoinRejectsTraversal 验证解压路径不能逃出目标目录。
func TestSafeJoinRejectsTraversal(t *testing.T) {
	if _, err := safeJoin("/tmp/goodhr-runtime", "../evil.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

// TestSafeJoinAcceptsNestedPath 验证正常嵌套路径可以解压。
func TestSafeJoinAcceptsNestedPath(t *testing.T) {
	path, err := safeJoin("/tmp/goodhr-runtime", "node/bin/node")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "node") {
		t.Fatalf("unexpected path: %s", path)
	}
}

// TestArchiveNameFromURL 验证下载文件名会忽略查询参数。
func TestArchiveNameFromURL(t *testing.T) {
	name := archiveName("https://oss.58it.cn/goodhr-node.zip?version=1", "node")
	if name != "goodhr-node.zip" {
		t.Fatalf("archive name = %s", name)
	}
}

// TestAssetIsCurrentWhenFileAndVersionMatch 验证文件存在且版本一致时会跳过下载。
func TestAssetIsCurrentWhenFileAndVersionMatch(t *testing.T) {
	manager := testRuntimeManager(t)
	nodePath := filepath.Join(manager.cfg.RuntimeDir, "node", "bin", "node")
	if err := os.MkdirAll(filepath.Dir(nodePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, []byte("node"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := Asset{Version: "22.19.0", URL: "https://oss.58it.cn/node.tar.gz", SHA256: "abc"}
	if err := manager.saveVersion("node_runtime", asset); err != nil {
		t.Fatal(err)
	}
	if !manager.assetIsCurrent("node_runtime", asset) {
		t.Fatal("expected node_runtime to be current")
	}
}

// TestAssetIsCurrentRejectsVersionMismatch 验证版本不一致时不会跳过下载。
func TestAssetIsCurrentRejectsVersionMismatch(t *testing.T) {
	manager := testRuntimeManager(t)
	nodePath := filepath.Join(manager.cfg.RuntimeDir, "node", "bin", "node")
	if err := os.MkdirAll(filepath.Dir(nodePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodePath, []byte("node"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manager.saveVersion("node_runtime", Asset{Version: "22.18.0", SHA256: "abc"}); err != nil {
		t.Fatal(err)
	}
	asset := Asset{Version: "22.19.0", URL: "https://oss.58it.cn/node.tar.gz", SHA256: "abc"}
	if manager.assetIsCurrent("node_runtime", asset) {
		t.Fatal("expected node_runtime version mismatch to require download")
	}
}

// TestAssetIsCurrentRejectsMissingFile 验证文件缺失时不会跳过下载。
func TestAssetIsCurrentRejectsMissingFile(t *testing.T) {
	manager := testRuntimeManager(t)
	asset := Asset{Version: "22.19.0", URL: "https://oss.58it.cn/node.tar.gz", SHA256: "abc"}
	if err := manager.saveVersion("node_runtime", asset); err != nil {
		t.Fatal(err)
	}
	if manager.assetIsCurrent("node_runtime", asset) {
		t.Fatal("expected missing node file to require download")
	}
}

// testRuntimeManager 创建测试用运行组件管理器。
// t 为测试对象。
func testRuntimeManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		RuntimeDir: filepath.Join(root, "runtime"),
		OCRDir:     filepath.Join(root, "runtime", "ocr"),
	}
	if err := os.MkdirAll(cfg.RuntimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return NewManager(cfg)
}
