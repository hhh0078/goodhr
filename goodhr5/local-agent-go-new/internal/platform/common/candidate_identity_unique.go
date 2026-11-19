// Package common 文件作用：复用平台稳定身份字段重新唯一定位已读取的人选，避免推荐名单换位后误用旧序号。
package common

import (
	"fmt"
	"sort"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// uniqueCandidateSelector 按完整已读身份约束卡片，不指定同名序号；Worker必须确认唯一，信息不全时拒绝降级。
func uniqueCandidateSelector(cfg model.Config, candidate model.Candidate, selector contract.SelectorSpec) (contract.SelectorSpec, error) {
	fields := candidateResumeFields(cfg)
	if len(fields) < 2 || strings.TrimSpace(candidate.Fields["name"]) == "" {
		return contract.SelectorSpec{}, fmt.Errorf("候选人的身份信息还没读完整，暂不操作")
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	texts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.Join(strings.Fields(candidate.Fields[key]), " ")
		if value == "" || fields[key].ReadAttribute != "" || fields[key].ReadProperty == "html" {
			return contract.SelectorSpec{}, fmt.Errorf("候选人的身份信息还没读完整，暂不操作")
		}
		texts = append(texts, value)
	}
	return selector.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		variant.Target.Index = nil // 让所有动作的requireUnique生效，重名同经历不能默认选第一位。
		variant.Target.TextLayoutWhitespace = true // innerText的排版换行不一定存在于hasText使用的节点文字中。
		variant.Target.Texts = append(append([]string(nil), variant.Target.Texts...), texts...)
		variant.Description = firstNonEmpty(candidate.Name, variant.Description)
		return variant
	}), nil
}
