// Package liepin 文件作用：承载 candidate.go 对应的平台职责实现。
package liepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
	"time"
)

// ListVisibleCandidates 提取当前可见猎聘企业端候选人。
func (r *Runtime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	startedAt := time.Now()
	item := platformElement(cfg, "card", "item")
	if item == nil {
		return nil, fmt.Errorf("平台配置中无候选人卡片选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{"element": item, "visible_only": true, "fields": cardFieldRequests(cfg), "max_items": maxItems})
	if err != nil {
		return nil, err
	}
	rawItems := mapList(workerData(result, "items"))
	candidates := make([]platformcore.Candidate, 0, len(rawItems))
	for _, item := range rawItems {
		fields := mapFromAny(item["fields"])
		rawText := firstNonEmpty(stringFromMap(item, "text"), candidateRawText(fields))
		name := firstNonEmpty(stringFromMap(fields, "name"), fmt.Sprintf("候选人%d", intFromMap(item, "index")+1))
		candidates = append(candidates, platformcore.Candidate{
			"name":           name,
			"candidate_name": name,
			"status":         "scanned",
			"raw_text":       rawText,
			"filter_text":    rawText,
			"platform_id":    r.platformID,
			"card_index":     intFromMap(item, "index"),
			"element_ref":    stringFromMap(item, "ref"),
			"fields":         fields,
		})
	}
	exec.Log("info", fmt.Sprintf("候选人提取完成：count=%d elapsed=%s", len(candidates), formatElapsedMS(int(time.Since(startedAt).Milliseconds()))))
	return candidates, nil
}

// ScrollCandidateList 滚动猎聘企业端候选人列表。
func (r *Runtime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	scroll := platformElement(cfg, "card", "scroll")
	if scroll != nil {
		_, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"element": scroll, "distance": distance})
		return err
	}
	_, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": distance})
	return err
}

// CandidateFilterText 返回猎聘企业端候选人筛选文本。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text")))
}

// CandidateFingerprint 返回猎聘企业端候选人去重指纹。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	name := firstNonEmpty(stringFromMap(candidate, "candidate_name"), stringFromMap(candidate, "name"), stringFromMap(fields, "name"))
	age := candidateAge(candidate)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(age) == "" {
		return ""
	}
	return r.platformID + "_" + normalizeCandidateIDPart(name) + "_" + normalizeCandidateIDPart(age)
}
