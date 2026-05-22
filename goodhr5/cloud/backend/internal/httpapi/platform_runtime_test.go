// 本文件用于验证平台运行时分发与未实现平台阻断行为。
package httpapi

import (
	"strings"
	"testing"
)

// TestRuntimeByPlatformIDBoss 验证 Boss 平台能获取到运行时实现。
func TestRuntimeByPlatformIDBoss(t *testing.T) {
	rt, err := runtimeByPlatformID("boss")
	if err != nil {
		t.Fatalf("expected boss runtime, got error: %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil runtime for boss")
	}
}

// TestRuntimeByPlatformIDUnknown 验证未实现平台会被严格阻断。
func TestRuntimeByPlatformIDUnknown(t *testing.T) {
	_, err := runtimeByPlatformID("zhaopin")
	if err == nil {
		t.Fatal("expected error for unimplemented platform")
	}
	if !strings.Contains(err.Error(), "未实现 runtime") {
		t.Fatalf("unexpected error: %v", err)
	}
}
