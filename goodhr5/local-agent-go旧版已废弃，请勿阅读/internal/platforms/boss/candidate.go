// Package boss 文件作用：承载 candidate.go 对应的平台职责实现。
package boss

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
	"time"
)

// ListVisibleCandidates 提取当前可见 Boss 候选人。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，maxItems 为最多数量。
func (r *Runtime) ListVisibleCandidates(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, maxItems int) ([]platformcore.Candidate, error) {
	startedAt := time.Now()
	exec.Log("info", fmt.Sprintf("候选人提取请求已发送：max_items=%d", maxItems))
	result, err := exec.Post(ctx, "/api/v1/boss/candidates/extract", map[string]any{
		"platform_config": cfg,
		"max_items":       maxItems,
	})
	if err != nil {
		exec.Log("warning", fmt.Sprintf("候选人提取请求失败：elapsed=%s err=%s", time.Since(startedAt).Round(time.Millisecond), err.Error()))
		return nil, err
	}
	data := workerDataMap(result)
	items := mapList(data["candidates"])
	exec.Log("info", fmt.Sprintf("候选人卡片提取返回：found=%d candidates=%d worker_find=%s worker_convert=%s total=%s",
		intFromMap(data, "found_count"),
		len(items),
		formatElapsedMS(intFromMap(data, "find_elapsed_ms")),
		formatElapsedMS(intFromMap(data, "convert_elapsed_ms")),
		formatElapsedMS(intFromMap(data, "elapsed_ms")),
	))
	convertStartedAt := time.Now()
	candidates := make([]platformcore.Candidate, 0, len(items))
	for index, item := range items {
		if index == 0 || (index+1)%20 == 0 || index+1 == len(items) {
			exec.Log("info", fmt.Sprintf("正在整理候选人：%d/%d", index+1, len(items)))
		}
		candidate := platformcore.Candidate(item)
		if id := r.CandidateFingerprint(candidate); id != "" {
			candidate["id"] = id
		}
		candidates = append(candidates, candidate)
	}
	exec.Log("info", fmt.Sprintf("候选人整理完成：有效=%d 整理耗时=%s 总耗时=%s", len(candidates), time.Since(convertStartedAt).Round(time.Millisecond), time.Since(startedAt).Round(time.Millisecond)))
	return candidates, nil
}

// ScrollCandidateList 滚动 Boss 候选人列表。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，distance 为滚动距离。
func (r *Runtime) ScrollCandidateList(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, distance int) error {
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/scroll", map[string]any{
		"platform_config": cfg,
		"distance":        distance,
	})
	return err
}

// EnsureCandidateVisible 使用小步滚轮滚动到指定 Boss 候选人卡片。
// ctx 为运行上下文，exec 为执行器，cfg 为平台配置，candidate 为候选人。
func (r *Runtime) EnsureCandidateVisible(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	payload := bossCandidateVisiblePayload(cfg, candidate)
	payload["debug_stage"] = "decision-before"
	_, err := exec.Post(ctx, "/api/v1/boss/candidates/visible", payload)
	return err
}

// CandidateFilterText 返回 Boss 候选人筛选文本。
// candidate 为候选人。
func (r *Runtime) CandidateFilterText(candidate platformcore.Candidate) string {
	return strings.TrimSpace(firstNonEmpty(stringFromMap(candidate, "filter_text"), stringFromMap(candidate, "raw_text")))
}

// CandidateFingerprint 返回 Boss 候选人去重指纹。
// candidate 为候选人，仅使用姓名和年龄生成稳定 ID。
func (r *Runtime) CandidateFingerprint(candidate platformcore.Candidate) string {
	fields := mapFromAny(candidate["fields"])
	name := firstNonEmpty(stringFromMap(candidate, "candidate_name"), stringFromMap(candidate, "name"), stringFromMap(fields, "name"))
	age := candidateAge(candidate)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(age) == "" {
		return ""
	}
	return "boss_" + normalizeCandidateIDPart(name) + "_" + normalizeCandidateIDPart(age)
}
