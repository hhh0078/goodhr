// Package contract 文件作用：兼容单个选择器对象与备用对象数组，并让动态定位约束作用于每条完整路径。
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// UnmarshalJSON 保留原选择器对象格式，并将非空对象数组转换成有序备用路径。
// data 为外部 JSON，数组中的每条路径都必须是完整选择器对象。
func (s *SelectorSpec) UnmarshalJSON(data []byte) error {
	type plainSelector SelectorSpec
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return json.Unmarshal(trimmed, (*plainSelector)(s))
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return err
	}
	if len(entries) == 0 || len(entries) > 8 {
		return fmt.Errorf("选择器数组必须包含一到八个完整对象")
	}
	variants := make([]SelectorSpec, 0, len(entries))
	for index, entry := range entries {
		entry = bytes.TrimSpace(entry)
		if len(entry) == 0 || entry[0] != '{' {
			return fmt.Errorf("选择器数组第 %d 项必须是完整对象", index+1)
		}
		var variant plainSelector
		if err := json.Unmarshal(entry, &variant); err != nil {
			return fmt.Errorf("解析选择器数组第 %d 项失败：%w", index+1, err)
		}
		if len(variant.Target.Selectors) == 0 {
			return fmt.Errorf("选择器数组第 %d 项缺少目标选择器", index+1)
		}
		variants = append(variants, SelectorSpec(variant))
	}
	*s = SelectorVariants(variants)
	return nil
}

// Variants 返回主选择器及其备用路径，单个旧对象仍仅返回原路径。
func (s SelectorSpec) Variants() []SelectorSpec {
	primary := s
	primary.Alternatives = nil
	result := []SelectorSpec{primary}
	for _, alternative := range s.Alternatives {
		result = append(result, alternative.Variants()...)
	}
	return result
}

// MapVariants 将同一定位约束应用于每条完整路径，防止备用路径丢失候选人身份或列表序号。
// transform 只修改传入的当前路径，返回包含全部修改后路径的选择器。
func (s SelectorSpec) MapVariants(transform func(SelectorSpec) SelectorSpec) SelectorSpec {
	if len(s.Alternatives) == 0 {
		return transform(s)
	}
	variants := s.Variants()
	for index, variant := range variants {
		variants[index] = transform(variant)
	}
	return SelectorVariants(variants)
}

// SelectorVariants 把完整路径按原顺序收拢为一个选择器；空输入返回空选择器。
func SelectorVariants(variants []SelectorSpec) SelectorSpec {
	if len(variants) == 0 {
		return SelectorSpec{}
	}
	result := variants[0]
	result.Alternatives = append([]SelectorSpec(nil), result.Alternatives...)
	for _, variant := range variants[1:] {
		result.Alternatives = append(result.Alternatives, variant.Variants()...)
	}
	return result
}

// WithinSelector 把每条子元素路径放入每条父元素路径，完整保留父级身份、序号和框架。
// parent 为已经收紧身份的父元素，返回按主路径优先排列的所有有效组合。
func (s SelectorSpec) WithinSelector(parent SelectorSpec) SelectorSpec {
	variants := make([]SelectorSpec, 0, (1+len(s.Alternatives))*(1+len(parent.Alternatives)))
	for _, parentVariant := range selectorScopeVariants(parent) {
		for _, childVariant := range selectorScopeVariants(s) {
			parents := make([]SelectorGroup, 0, len(parentVariant.Parents)+1+len(childVariant.Parents))
			parents = append(parents, parentVariant.Parents...)
			parents = append(parents, parentVariant.Target)
			parents = append(parents, childVariant.Parents...)
			childVariant.Parents = parents
			childVariant.Frames = append(append([]SelectorGroup(nil), parentVariant.Frames...), childVariant.Frames...)
			variants = append(variants, childVariant)
		}
	}
	return SelectorVariants(variants)
}

// selectorScopeVariants 展开作用域中的备用框架路径，避免父级组合时丢失其框架入口。
func selectorScopeVariants(selector SelectorSpec) []SelectorSpec {
	var result []SelectorSpec
	for _, variant := range selector.Variants() {
		fallbacks := variant.FrameFallbacks
		variant.FrameFallbacks = nil
		result = append(result, variant)
		for _, frames := range fallbacks {
			fallback := variant
			fallback.Frames = frames
			result = append(result, fallback)
		}
	}
	return result
}
