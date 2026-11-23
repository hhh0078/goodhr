// Package common 文件作用：验证猎聘稳定编号仅在原行内核对，显示文字变化不影响动作，缺失编号不得降级且不污染平台配置。
package common

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// candidateFieldConfig 读取实际发布配置，避免稳定编号选择器和测试夹具使用不同规则。
func candidateFieldConfig(t *testing.T) model.Config {
	t.Helper()
	data, err := os.ReadFile("../hliepin/config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestCandidateFieldIdentityIgnoresDisplayChanges 验证所有页都按原行稳定编号核对，同名年龄和显示文字变化不会进入选择条件。
func TestCandidateFieldIdentityIgnoresDisplayChanges(t *testing.T) {
	cfg := candidateFieldConfig(t)
	before, _ := json.Marshal(cfg)
	var reference contract.SelectorSpec
	for _, byIndex := range []bool{false, true} {
		for _, display := range []string{"旧姓名", "新显示名"} {
			candidate := model.Candidate{Index: 7, Name: display, IdentityByIndex: byIndex, IdentityTexts: []string{display, "52岁"}, Fields: map[string]string{"platform_candidate_id": "same-id-\"\\value"}}
			selector, err := CandidateSelector(cfg, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if len(selector.Parents) != 2 || selector.Parents[0].Index == nil || *selector.Parents[0].Index != 7 || selector.Parents[1].Attributes["value"] != candidate.Fields["platform_candidate_id"] {
				t.Fatalf("编号没有限定在原行内：%+v", selector)
			}
			if selector.Target.Selectors[0].Value != "xpath=ancestor::tr[1]" || selector.Parents[1].Index != nil {
				t.Fatalf("编号核对后没有返回原行或错误默认选择编号第一项：%+v", selector)
			}
			for _, group := range append(append([]contract.SelectorGroup(nil), selector.Parents...), selector.Target) {
				if group.Text != "" || len(group.Texts) != 0 {
					t.Fatalf("稳定编号路径仍夹带显示姓名年龄：%+v", group)
				}
			}
			selector.Description = ""
			if reference.Target.Selectors == nil {
				reference = selector
			} else if !reflect.DeepEqual(reference, selector) {
				t.Fatal("显示文字或分页后的IdentityByIndex变化改变了稳定编号路径")
			}
		}
	}
	after, _ := json.Marshal(cfg)
	if string(before) != string(after) {
		t.Fatal("候选人动态编号污染了共享平台配置")
	}
}

// TestCandidateFieldIdentityRefusesMissingIdentity 验证缺少稳定编号或必要配置时拒绝生成动作，不使用仍存在的姓名年龄降级。
func TestCandidateFieldIdentityRefusesMissingIdentity(t *testing.T) {
	for _, name := range []string{"缺少编号", "空白编号", "负行号", "缺少字段", "缺少属性", "缺少原行返回目标"} {
		t.Run(name, func(t *testing.T) {
			cfg := candidateFieldConfig(t)
			candidate := model.Candidate{Index: 3, Name: "仍有姓名", IdentityTexts: []string{"仍有姓名", "30岁"}, Fields: map[string]string{"platform_candidate_id": "expected-id"}}
			switch name {
			case "缺少编号":
				candidate.Fields = nil
			case "空白编号":
				candidate.Fields["platform_candidate_id"] = "  "
			case "负行号":
				candidate.Index = -1
			case "缺少字段":
				delete(cfg.CandidateFields, "platform_candidate_id")
			case "缺少属性":
				field := cfg.CandidateFields["platform_candidate_id"]
				field.ReadAttribute = ""
				cfg.CandidateFields["platform_candidate_id"] = field
			case "缺少原行返回目标":
				delete(cfg.Selectors, "candidate.identity_check")
			}
			if _, err := CandidateSelector(cfg, candidate); err == nil {
				t.Fatal("缺少稳定身份时仍生成了可能误认人的动作")
			}
		})
	}
}

// TestCandidateActionsKeepOriginalRowIdentity 验证详情与开聊入口都保留原行号和已读编号条件，不把核对范围扩到其他行。
func TestCandidateActionsKeepOriginalRowIdentity(t *testing.T) {
	cfg := candidateFieldConfig(t)
	candidate := model.Candidate{Index: 9, Name: "显示名", Fields: map[string]string{"platform_candidate_id": "original-id"}}
	for _, key := range []string{"candidate.open_target", "candidate.greet", "candidate.continue"} {
		selector, err := CandidateActionSelector(cfg, key, candidate)
		if err != nil {
			t.Fatal(err)
		}
		if len(selector.Parents) < 3 || selector.Parents[0].Index == nil || *selector.Parents[0].Index != 9 || selector.Parents[1].Attributes["value"] != "original-id" || selector.Parents[2].Selectors[0].Value != "xpath=ancestor::tr[1]" {
			t.Fatalf("%s没有保留原行编号核对：%+v", key, selector)
		}
	}
}
