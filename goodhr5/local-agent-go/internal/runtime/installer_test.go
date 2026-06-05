// Package runtime 负责测试运行组件安装器的安全边界。
package runtime

import (
	"strings"
	"testing"
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
