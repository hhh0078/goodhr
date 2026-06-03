// Package boss 测试 Boss 平台运行时中的选择器合并逻辑。
package boss

import "testing"

// TestPositionListItemElementMergesListParents 验证岗位列表父级选择器会参与岗位项定位。
func TestPositionListItemElementMergesListParents(t *testing.T) {
	list := map[string]any{
		"parent_classes": [][]string{{".outer-panel"}},
		"target_classes": [][]string{{".job-list"}},
	}
	item := map[string]any{
		"parent_classes": [][]string{{".item-group"}},
		"target_classes": [][]string{{".job-item"}},
	}

	merged := positionListItemElement(list, item)
	parents := valueAsSliceList(merged["parent_classes"])
	expected := [][]string{
		{".outer-panel"},
		{".job-list"},
		{".item-group"},
	}
	if len(parents) != len(expected) {
		t.Fatalf("parent_classes length = %d, want %d: %#v", len(parents), len(expected), parents)
	}
	for index := range expected {
		if len(parents[index]) != len(expected[index]) || parents[index][0] != expected[index][0] {
			t.Fatalf("parent_classes[%d] = %#v, want %#v", index, parents[index], expected[index])
		}
	}
}
