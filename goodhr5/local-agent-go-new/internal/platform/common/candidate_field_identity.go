// Package common 文件作用：复用平台已有属性字段在原候选人行内核对稳定编号，避免显示姓名年龄变化误拒绝或跨行追人。
package common

import (
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// candidateFieldIdentitySelector 先固定原行，再核对该行内的已读属性编号，最后按配置返回同一行；编号缺失时禁止降级。
func candidateFieldIdentitySelector(cfg model.Config, candidate model.Candidate, row contract.SelectorSpec, key string) (contract.SelectorSpec, error) {
	identity := candidate.Fields[key]
	if candidate.Index < 0 || strings.TrimSpace(identity) == "" {
		return contract.SelectorSpec{}, fmt.Errorf("这位候选人的原行位置或稳定编号没读完整，先跳过，避免认错人")
	}
	field, ok := cfg.CandidateFields[key]
	if !ok || len(field.Target.Selectors) == 0 {
		return contract.SelectorSpec{}, fmt.Errorf("平台 %s 缺少候选人稳定编号字段配置", cfg.ID)
	}
	for _, variant := range field.Variants() {
		if strings.TrimSpace(variant.ReadAttribute) == "" {
			return contract.SelectorSpec{}, fmt.Errorf("平台 %s 的候选人稳定编号没有属性读取配置", cfg.ID)
		}
	}
	check, err := RequiredSelector(cfg, "candidate.identity_check")
	if err != nil {
		return contract.SelectorSpec{}, err
	}
	row = row.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		index := candidate.Index
		variant.Target.Index = &index
		return variant
	})
	field = field.MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		attributes := make(map[string]string, len(variant.Target.Attributes)+1)
		for name, value := range variant.Target.Attributes {
			attributes[name] = value
		}
		attributes[variant.ReadAttribute] = identity
		variant.Target.Attributes = attributes
		variant.Target.Index = nil
		return variant
	})
	return check.WithinSelector(field.WithinSelector(row)).MapVariants(func(variant contract.SelectorSpec) contract.SelectorSpec {
		variant.Description = firstNonEmpty(candidate.Name, variant.Description)
		return variant
	}), nil
}
