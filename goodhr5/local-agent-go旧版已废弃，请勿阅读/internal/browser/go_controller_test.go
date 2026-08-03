// Package browser 测试实验性 Go 浏览器控制库的通用协议转换。
package browser

import (
	goruntime "runtime"
	"testing"
)

// TestCombinedSelectorFromCloudConfig 验证 Go 控制器兼容云端父级和目标选择器结构。
func TestCombinedSelectorFromCloudConfig(t *testing.T) {
	config := map[string]any{
		"parent_classes": []any{[]any{".job-list"}},
		"target_classes": []any{[]any{".label"}},
	}
	if selector := combinedSelectorFromAny(config); selector != ".job-list .label" {
		t.Fatalf("组合选择器不对：%s", selector)
	}
}

// TestSelectorFromPayloadReadsElementRef 验证元素引用和可见参数可以通过统一入口解析。
func TestSelectorFromPayloadReadsElementRef(t *testing.T) {
	selector := selectorFromPayload(map[string]any{
		"element_ref":  "go-el-1",
		"visible_only": false,
	})
	if selector.Ref != "go-el-1" || selector.Visible {
		t.Fatalf("元素引用参数解析不对：%+v", selector)
	}
}

// TestParseControlOrMetaKey 验证组合键会按当前系统选择 Control 或 Meta。
func TestParseControlOrMetaKey(t *testing.T) {
	stroke := parseKeyStroke("ControlOrMeta+A")
	if stroke.Code != "KeyA" || stroke.CodeValue != 65 {
		t.Fatalf("按键信息不对：%+v", stroke)
	}
	wantModifier := 2
	if goruntime.GOOS == "darwin" {
		wantModifier = 4
	}
	if stroke.Modifiers != wantModifier {
		t.Fatalf("组合键修饰符不对：got=%d want=%d", stroke.Modifiers, wantModifier)
	}
}
