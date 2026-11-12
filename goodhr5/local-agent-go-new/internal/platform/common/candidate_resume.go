// Package common 文件作用：为没有独立列表编号解析器的平台提供保守断点，以小批只读身份摘要核对唯一性和前缀顺序。
package common

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// CandidateResumeTracker 由平台运行时内嵌；只保存当前任务的编号摘要，不接触数据库或业务发送。
type CandidateResumeTracker struct {
	enabled bool
	keys    []string
	points  map[int]model.CandidateResumePoint
}

// RestoreCandidateList 分批核对唯一身份；启用推荐续跑的平台允许前缀换序，并在尚未加载时沿原链路继续寻找。
// 完整前缀模式保持原有保护；任何读取、滚动或取消错误都原样返回，不冒充找不到并清除断点。
func (r *CandidateResumeTracker) RestoreCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, point model.CandidateResumePoint, cursor *model.CandidateListCursor) (bool, error) {
	r.enabled, r.keys, r.points = true, nil, nil
	if cursor == nil {
		return false, fmt.Errorf("候选人读取位置还没准备好")
	}
	*cursor = model.CandidateListCursor{}
	if point.CandidateID == "" || point.PrefixHash == "" {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	loadNext := cfg.Behavior.CandidateResumeByIdentity
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		keys, identities, err := readCandidateResumeSnapshot(ctx, browser, cfg)
		if err != nil {
			return false, err
		}
		matchIndex, matches := -1, 0
		for index, key := range keys {
			if key == point.CandidateID {
				matches++
				if cfg.Behavior.CandidateResumeByIdentity || HashText(strings.Join(keys[:index+1], "\n")) == point.PrefixHash {
					matchIndex = index
				}
			}
		}
		if matches == 1 && matchIndex >= 0 {
			r.keys = keys
			cursor.StartIndex = matchIndex
			// 主流程重叠读取锚点，必须恢复前面同名同龄者的计数，避免按默认第一个人定位。
			if cfg.Behavior.CandidateResumeByIdentity {
				cursor.IdentityOccurrences = make(map[string]int)
				for _, identity := range identities[:matchIndex] {
					cursor.IdentityOccurrences[identity]++
				}
			}
			return true, nil
		}
		if matches > 0 || !loadNext {
			r.keys = nil
			return false, nil
		}
		loadNext, err = LoadMoreResumeCandidates(ctx, browser, cfg)
		if err != nil {
			return false, err
		}
		// 没有新增时仍再核对一次，避免等待期间锚点刚出现却直接从头。
	}
}

// readCandidateResumeSnapshot 每批最多读取20人的最小身份，同时还原正常读取使用的姓名年龄定位键。
func readCandidateResumeSnapshot(ctx context.Context, browser model.Browser, cfg model.Config) ([]string, []string, error) {
	selector, err := RequiredSelector(cfg, "candidate.item")
	if err != nil {
		return nil, nil, err
	}
	selector.TimeoutMS = 1
	fields := candidateResumeFields(cfg)
	if len(fields) == 0 {
		return nil, nil, nil
	}
	var keys, identities []string
	for start := 0; ; start += 20 {
		items, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: selector, StartIndex: start, MaxItems: 20,
			Fields: fields, ReadConcurrency: 5, ExpectedMissing: true})
		if IsElementMissing(err) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			if item.Index != len(keys) {
				return nil, nil, fmt.Errorf("候选人名单正在刷新，断点暂不改动，请稍后再开始")
			}
			keys = append(keys, candidateResumeIdentity(cfg, item.Fields))
			identities = append(identities, strings.Join(CandidateIdentityTexts(item.Fields["name"], item.Fields, item.Text), "\x00"))
		}
		if len(items) < 20 {
			break
		}
	}
	return keys, identities, nil
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
