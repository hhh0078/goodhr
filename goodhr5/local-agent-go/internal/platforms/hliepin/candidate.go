// Package hliepin 文件作用：承载 candidate.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
	"time"
)

// ListVisibleCandidates 提取当前可见猎聘猎头端候选人。
func (r *Runtime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	startedAt := time.Now()
	item := platformElement(cfg, "card", "item")
	if item == nil {
		item = hliepinCandidateRowElement()
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{"element": item, "visible_only": true, "fields": cardFieldRequests(cfg), "max_items": maxItems})
	if err != nil {
		return nil, err
	}
	rawItems := mapList(workerData(result, "items"))
	// 2026 版猎聘已取消旧 .no-hover-tr class，云端旧选择器无结果时回退到候选人表格行。
	if len(rawItems) == 0 {
		item = hliepinCandidateRowElement()
		result, err = exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{"element": item, "visible_only": true, "max_items": maxItems})
		if err != nil {
			return nil, err
		}
		rawItems = mapList(workerData(result, "items"))
	}
	if len(rawItems) == 0 {
		pageURL := ""
		if pageResult, pageErr := exec.Post(ctx, "/api/v1/page/url", map[string]any{}); pageErr == nil {
			pageURL = stringFromMap(workerDataMap(pageResult), "url")
		}
		exec.Log("warning", fmt.Sprintf("猎聘候选人提取为空：url=%s selector=%s max_items=%d", pageURL, stringFromMap(item, "selector"), maxItems))
	}
	candidates := make([]platformcore.Candidate, 0, len(rawItems))
	for _, found := range rawItems {
		fields := mapFromAny(found["fields"])
		rawText := firstNonEmpty(stringFromMap(found, "text"), candidateRawText(fields))
		if !strings.Contains(rawText, "立即沟通") {
			continue
		}
		name := firstNonEmpty(stringFromMap(fields, "name"), hliepinCandidateName(rawText), fmt.Sprintf("候选人%d", intFromMap(found, "index")+1))
		candidate := platformcore.Candidate{
			"name":           name,
			"candidate_name": name,
			"status":         "scanned",
			"raw_text":       rawText,
			"filter_text":    rawText,
			"platform_id":    r.platformID,
			"card_index":     intFromMap(found, "index"),
			"element_ref":    stringFromMap(found, "ref"),
			"card_item":      item,
			"fields":         fields,
		}
		if id := r.CandidateFingerprint(candidate); id != "" {
			candidate["id"] = id
		}
		candidates = append(candidates, candidate)
	}
	exec.Log("info", fmt.Sprintf("候选人提取完成：count=%d elapsed=%s", len(candidates), formatElapsedMS(int(time.Since(startedAt).Milliseconds()))))
	return candidates, nil
}

// ScrollCandidateList 滚动猎聘猎头端候选人列表。
func (r *Runtime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	behavior := platformSection(cfg, "behavior")
	nextSelector := firstNonEmpty(stringFromMap(behavior, "nextPageBtn"), ".ant-pagination-next")
	disabledClass := firstNonEmpty(stringFromMap(behavior, "nextPageDisabledClass"), "ant-pagination-disabled")
	result, err := exec.Post(ctx, "/api/v1/page/scroll-or-click-next", map[string]any{
		"distance":       distance,
		"next_element":   map[string]any{"selector": nextSelector},
		"disabled_class": disabledClass,
		"next_wait_ms":   1800,
	})
	if err == nil {
		data := workerDataMap(result)
		exec.Log("info", fmt.Sprintf("猎聘候选人列表推进：action=%s reason=%s", stringFromMap(data, "action"), stringFromMap(data, "reason")))
	}
	return err
}

// CandidateFilterText 返回猎聘猎头端候选人筛选文本。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text")))
}

// CandidateFingerprint 返回猎聘猎头端候选人去重指纹。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	name := firstNonEmpty(stringFromMap(candidate, "candidate_name"), stringFromMap(candidate, "name"), stringFromMap(fields, "name"))
	age := candidateAge(candidate)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(age) == "" {
		return ""
	}
	raw := normalizeCandidateIDPart(firstNonEmpty(stringFromMap(candidate, "raw_text"), stringFromMap(candidate, "filter_text")))
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%s_%s_%s_%x", r.platformID, normalizeCandidateIDPart(name), normalizeCandidateIDPart(age), sum[:8])
}

// hliepinCandidateRowElement 返回新版猎聘候选人行的安全回退选择器。
func hliepinCandidateRowElement() map[string]any {
	return map[string]any{"selector": "tbody tr"}
}

// candidateItemElement 返回列表抓取时实际使用的候选人行选择器。
func candidateItemElement(candidate platformcore.Candidate, cfg cloudapi.PlatformConfig) map[string]any {
	if item := mapFromAny(candidate["card_item"]); len(item) > 0 {
		return item
	}
	if item := platformElement(cfg, "card", "item"); item != nil {
		return item
	}
	return hliepinCandidateRowElement()
}

// hliepinCandidateName 从新版候选人行文本中提取姓名。
func hliepinCandidateName(text string) string {
	ignored := map[string]bool{
		"隐藏": true, "活跃状态": true, "在线": true, "今天活跃": true,
		"3天内活跃": true, "30天内活跃": true,
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || ignored[line] || strings.Contains(line, "岁") || strings.Contains(line, "求职期望") {
			continue
		}
		if len([]rune(line)) <= 12 {
			return line
		}
	}
	return ""
}
