// Package common 文件作用：验证推荐名单换位后按完整身份唯一定位，不放松重名和缺失信息保护。
package common

import (
	"encoding/json"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// uniqueIdentityFixture 构造只有合成信息的卡片配置，旧版本会忽略新增行为开关。
func uniqueIdentityFixture(t *testing.T) (model.Config, model.Candidate) {
	t.Helper()
	cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
		"candidate.item":           {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".row"}}}},
		"candidate.identity_check": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ":scope"}}}},
	}, CandidateFields: map[string]contract.SelectorSpec{"name": {}, "basic_info": {}, "education": {}}}
	if err := json.Unmarshal([]byte(`{"candidate_unique_identity":true}`), &cfg.Behavior); err != nil {
		t.Fatal(err)
	}
	candidate := model.Candidate{Index: 12, IdentityByIndex: true, Name: "合成人选", IdentityTexts: []string{"合成人选", "30岁"},
		Fields: map[string]string{"name": "合成人选", "basic_info": "30岁  5年经验", "education": "合成学校\n本科"}}
	return cfg, candidate
}

// TestUniqueCandidateIdentityDoesNotBindOldIndex 验证原索引12不会写入动作定位，完整字段参与唯一匹配。
func TestUniqueCandidateIdentityDoesNotBindOldIndex(t *testing.T) {
	cfg, candidate := uniqueIdentityFixture(t)
	selector, err := CandidateSelector(cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if selector.Target.Index != nil || !selector.Target.TextLayoutWhitespace || len(selector.Parents) != 0 || selector.Target.Selectors[0].Value != ".row" {
		t.Fatalf("候选人仍绑定旧行号：%+v", selector)
	}
	if got := strings.Join(selector.Target.Texts, "|"); !strings.Contains(got, "30岁 5年经验") || !strings.Contains(got, "合成学校 本科") {
		t.Fatalf("没有核对完整身份：%q", got)
	}
}

// TestUniqueCandidateIdentityMissingFieldStops 验证教育或基础字段缺失时不降级到姓名、年龄或旧行号。
func TestUniqueCandidateIdentityMissingFieldStops(t *testing.T) {
	for _, field := range []string{"name", "basic_info", "education"} {
		cfg, candidate := uniqueIdentityFixture(t)
		delete(candidate.Fields, field)
		if _, err := CandidateSelector(cfg, candidate); err == nil {
			t.Fatalf("缺失%s仍允许操作", field)
		}
	}
}

// TestUniqueCandidateIdentityOnlyChangesEnabledViewport 验证未开启的平台和非视口起跑的搜索页保留原行为。
func TestUniqueCandidateIdentityOnlyChangesEnabledViewport(t *testing.T) {
	cfg, candidate := uniqueIdentityFixture(t)
	if err := json.Unmarshal([]byte(`{"candidate_unique_identity":false}`), &cfg.Behavior); err != nil {
		t.Fatal(err)
	}
	selector, err := CandidateSelector(cfg, candidate)
	if err != nil || selector.Target.TextLayoutWhitespace || len(selector.Parents) != 1 || selector.Parents[0].Index == nil || *selector.Parents[0].Index != 12 {
		t.Fatalf("其他平台的行内核对被改变：%+v %v", selector, err)
	}
	cfg, candidate = uniqueIdentityFixture(t)
	candidate.IdentityByIndex, candidate.IdentityOccurrence = false, 2
	selector, err = CandidateSelector(cfg, candidate)
	if err != nil || selector.Target.TextLayoutWhitespace || selector.Target.Index == nil || *selector.Target.Index != 2 {
		t.Fatalf("搜索页原同名顺序被改变：%+v %v", selector, err)
	}
}
