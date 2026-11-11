// Package common 文件作用：为没有独立列表编号解析器的平台提供保守断点，以小批只读身份摘要核对唯一性和前缀顺序。
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// CandidateResumeTracker 由平台运行时内嵌；只保存当前任务的编号摘要，不接触数据库或业务发送。
type CandidateResumeTracker struct {
	enabled bool
	keys    []string
	points  map[int]model.CandidateResumePoint
}

// RestoreCandidateList 分批核对当前已加载名单，唯一身份和完整前缀均一致才恢复，读取失败不能冒充找不到。
// 缺少平台编号时使用姓名、基本信息与经历的联合摘要；同摘要重复或字段变化均从头，不用姓名年龄猜位置。
func (r *CandidateResumeTracker) RestoreCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, point model.CandidateResumePoint, cursor *model.CandidateListCursor) (bool, error) {
	r.enabled, r.keys, r.points = true, nil, nil
	if cursor == nil {
		return false, fmt.Errorf("候选人读取位置还没准备好")
	}
	*cursor = model.CandidateListCursor{}
	if point.CandidateID == "" || point.PrefixHash == "" {
		return false, nil
	}
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return false, err
	}
	selector.TimeoutMS = 1 // 显式短探测；Go的零值会省略字段并变成Worker默认等待。
	fields := candidateResumeFields(cfg)
	if len(fields) == 0 {
		return false, nil
	}
	matchIndex, matches := -1, 0
	for start := 0; ; start += 20 {
		items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, StartIndex: start, MaxItems: 20,
			Fields: fields, ReadConcurrency: 5, ExpectedMissing: true})
		if IsElementMissing(err) {
			break
		}
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if item.Index != len(r.keys) {
				return false, nil // 标准读取发现名单变化或漏行时，不拼接不同版本的名单。
			}
			key := candidateResumeIdentity(cfg, item.Fields)
			r.keys = append(r.keys, key)
			if key == point.CandidateID {
				matches++
				if HashText(strings.Join(r.keys, "\n")) == point.PrefixHash {
					matchIndex = item.Index
				}
			}
		}
		if len(items) < 20 {
			break
		}
	}
	if matches != 1 || matchIndex < 0 {
		r.keys = nil
		return false, nil
	}
	// 重叠一张完成者供主流程去重，并保留分页和列表到底判断所需的真实尾部卡片。
	cursor.StartIndex = matchIndex
	return true, nil
}

// RememberCandidateResume 记住本批所有原始行的身份摘要，已过滤的推广行也占真实序号，不因此错位。
func (r *CandidateResumeTracker) RememberCandidateResume(cfg model.Config, candidates []model.Candidate) {
	r.points = nil
	if !r.enabled || len(candidates) == 0 {
		return
	}
	start := candidates[0].Index
	if start < 0 || start > len(r.keys) {
		return
	}
	r.keys = r.keys[:start]
	r.points = make(map[int]model.CandidateResumePoint, len(candidates))
	for _, candidate := range candidates {
		if candidate.Index != len(r.keys) {
			r.points = nil
			return
		}
		key := candidateResumeIdentity(cfg, candidate.Fields)
		r.keys = append(r.keys, key)
		if key != "" {
			r.points[candidate.Index] = model.CandidateResumePoint{CandidateID: key, PrefixHash: HashText(strings.Join(r.keys, "\n"))}
		}
	}
}

// CandidateResumePoint 交付本批已核对的只读断点，主流程处理成功后才会持久化。
func (r *CandidateResumeTracker) CandidateResumePoint(candidate model.Candidate) (model.CandidateResumePoint, bool) {
	point, ok := r.points[candidate.Index]
	return point, ok
}

// candidateResumeFields 优先使用平台已有稳定编号；否则只读取联合身份字段，排除按钮、活跃标记和整张简历正文。
func candidateResumeFields(cfg model.Config) map[string]contract.SelectorSpec {
	if id, ok := cfg.CandidateFields["platform_candidate_id"]; ok {
		return map[string]contract.SelectorSpec{"platform_candidate_id": id}
	}
	fields := make(map[string]contract.SelectorSpec)
	keys := cfg.CandidateResumeFields
	if len(keys) == 0 {
		keys = []string{"name", "basic_info", "education"}
	}
	for _, key := range keys {
		if spec, ok := cfg.CandidateFields[key]; ok {
			fields[key] = spec
		}
	}
	return fields
}

// candidateResumeIdentity 对固定顺序的身份字段取摘要，缺编号或姓名时不建立断点，不持久化个人信息。
func candidateResumeIdentity(cfg model.Config, values map[string]string) string {
	fields := candidateResumeFields(cfg)
	if _, id := fields["platform_candidate_id"]; id {
		if strings.TrimSpace(values["platform_candidate_id"]) == "" {
			return ""
		}
	} else if strings.TrimSpace(values["name"]) == "" {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, strings.Join(strings.Fields(values[key]), " "))
	}
	data, _ := json.Marshal(parts) // 字符串切片不会产生JSON编码错误。
	return HashText(string(data))
}
