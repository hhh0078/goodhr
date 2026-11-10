// Package contract 文件作用：验证旧选择器对象往返不变，以及新数组的完整路径与边界校验。
package contract

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSelectorSpecLegacyObjectRoundTrip 验证旧对象保留全部约束且不会增加备用路径字段。
func TestSelectorSpecLegacyObjectRoundTrip(t *testing.T) {
	content := `{"frames":[{"selectors":[{"type":"css","value":"iframe"}]}],"frame_fallbacks":[[]],"parents":[{"selectors":[{"type":"css","value":".list"}],"text":"原父级"}],"target":{"selectors":[{"type":"css","value":".card"}],"index":2,"texts":["身份"],"attributes":{"role":"listitem"}},"state":"enabled","timeout_ms":900,"read_attribute":"href","description":"旧定位"}`
	var spec SelectorSpec
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(spec)
	if err != nil || strings.Contains(string(encoded), "alternatives") {
		t.Fatalf("旧对象格式发生变化：%s，错误：%v", encoded, err)
	}
	var before, after any
	if err := json.Unmarshal([]byte(content), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &after); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("旧对象往返后不同：%s，错误：%v", encoded, err)
	}
}

// TestSelectorSpecArrayRetainsIndependentPaths 验证数组不会交叉合并不同父级、目标和读取方式。
func TestSelectorSpecArrayRetainsIndependentPaths(t *testing.T) {
	var spec SelectorSpec
	content := `[{"parents":[{"selectors":[{"type":"css","value":".recommend"}]}],"target":{"selectors":[{"type":"css","value":".name"}]},"description":"推荐"},{"parents":[{"selectors":[{"type":"css","value":".search"}]},{"selectors":[{"type":"css","value":".row"}]}],"target":{"selectors":[{"type":"css","value":"a.person"}]},"read_attribute":"title","description":"搜索"}]`
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		t.Fatal(err)
	}
	variants := spec.Variants()
	if len(variants) != 2 || len(variants[0].Parents) != 1 || len(variants[1].Parents) != 2 || variants[1].ReadAttribute != "title" {
		t.Fatalf("完整路径没有保留：%+v", variants)
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var restored SelectorSpec
	if err := json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(spec, restored) {
		t.Fatalf("规范化对象无法往返：%s，错误：%v", encoded, err)
	}
}

// TestSelectorSpecArrayRejectsInvalidEntries 验证数组为空或包含非完整对象时立即报错。
func TestSelectorSpecArrayRejectsInvalidEntries(t *testing.T) {
	for _, content := range []string{`[]`, `[null]`, `[".card"]`, `[[]]`, `[{}]`, `[{"target":{"selectors":[]}}]`} {
		var spec SelectorSpec
		if err := json.Unmarshal([]byte(content), &spec); err == nil {
			t.Fatalf("不应接受损坏的选择器数组：%s", content)
		}
	}
}

// TestSelectorScopeVariantsKeepParentFrameFallback 验证完整路径组合不会丢失父级备用框架。
func TestSelectorScopeVariantsKeepParentFrameFallback(t *testing.T) {
	group := func(value string) SelectorGroup {
		return SelectorGroup{Selectors: []SelectorCandidate{{Type: "css", Value: value}}}
	}
	parent := SelectorSpec{Frames: []SelectorGroup{group("iframe.primary")}, FrameFallbacks: [][]SelectorGroup{{group("iframe.backup")}}, Target: group(".person")}
	child := SelectorSpec{Target: group(".greet"), Alternatives: []SelectorSpec{{Target: group(".greet-backup")}}}
	variants := child.WithinSelector(parent).Variants()
	if len(variants) != 4 || variants[0].Frames[0].Selectors[0].Value != "iframe.primary" || variants[2].Frames[0].Selectors[0].Value != "iframe.backup" {
		t.Fatalf("父级备用框架没有完整组合：%+v", variants)
	}
}
