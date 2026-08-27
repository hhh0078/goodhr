// Package process 文件作用：验证 Worker 子进程环境变量覆盖规则。
package process

import (
	"strings"
	"testing"
)

// TestMergeEnvironmentClearsLegacyBinaryPath 验证空值也能覆盖系统里的旧 CloakBrowser 二进制路径。
func TestMergeEnvironmentClearsLegacyBinaryPath(t *testing.T) {
	result := mergeEnvironment(
		[]string{"PATH=/usr/bin", "CLOAKBROWSER_BINARY_PATH=/legacy/chrome"},
		[]string{"CLOAKBROWSER_BINARY_PATH=", "CLOAKBROWSER_AUTO_UPDATE=false"},
	)
	joined := strings.Join(result, "\n")
	if strings.Contains(joined, "/legacy/chrome") {
		t.Fatalf("旧版二进制路径仍在 Worker 环境里：%s", joined)
	}
	if !strings.Contains(joined, "CLOAKBROWSER_BINARY_PATH=") {
		t.Fatalf("清空后的二进制路径没有传给 Worker：%s", joined)
	}
}
