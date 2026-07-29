// Package greeting 文件作用：并发完成候选人基础信息 AI 预判断，并按页面原顺序返回处理结果。
package greeting

import (
	"context"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const candidatePreviewConcurrency = 5

// candidatePreviewResult 保存候选人基础信息预判断结果和原页面序号。
type candidatePreviewResult struct {
	Index     int
	Candidate model.Candidate
	Decision  *ai.Decision
	Err       error
}

// candidatePreviews 并发执行 AI 基础预判断，并通过结果通道严格按页面顺序输出。
func (f *Flow) candidatePreviews(
	ctx context.Context,
	prepared shared.PreparedTask,
	candidates []model.Candidate,
) <-chan candidatePreviewResult {
	ordered := make(chan candidatePreviewResult, len(candidates))
	if !usesCandidatePreview(prepared) {
		for index, candidate := range candidates {
			ordered <- candidatePreviewResult{Index: index, Candidate: candidate}
		}
		close(ordered)
		return ordered
	}

	startedAt := time.Now()
	f.log(prepared.Request.TaskID, "candidate_preview", "start", startedAt, nil)
	jobs := make(chan candidatePreviewResult)
	results := make(chan candidatePreviewResult, len(candidates))
	workerCount := min(candidatePreviewConcurrency, max(1, len(candidates)))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for item := range jobs {
				decision, err := f.AI.EvaluateCandidatePreview(
					ctx,
					prepared.Position.AI,
					prepared.Position,
					item.Candidate,
				)
				item.Err = err
				if err == nil {
					item.Decision = &decision
				}
				select {
				case results <- item:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index, candidate := range candidates {
			select {
			case jobs <- candidatePreviewResult{Index: index, Candidate: candidate}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		if err := ctx.Err(); err != nil {
			f.log(prepared.Request.TaskID, "candidate_preview", "warning", startedAt, err)
		} else {
			f.log(prepared.Request.TaskID, "candidate_preview", "success", startedAt, nil)
		}
		close(results)
	}()
	go orderCandidatePreviews(ctx, results, ordered)
	return ordered
}

// orderCandidatePreviews 缓存乱序完成的 AI 结果，并按连续页面序号输出。
func orderCandidatePreviews(
	ctx context.Context,
	results <-chan candidatePreviewResult,
	ordered chan<- candidatePreviewResult,
) {
	defer close(ordered)
	pending := make(map[int]candidatePreviewResult)
	nextIndex := 0
	for result := range results {
		pending[result.Index] = result
		for {
			next, exists := pending[nextIndex]
			if !exists {
				break
			}
			delete(pending, nextIndex)
			select {
			case ordered <- next:
				nextIndex++
			case <-ctx.Done():
				return
			}
		}
	}
}

// usesCandidatePreview 判断当前岗位是否需要在打开详情前执行 AI 基础预判断。
func usesCandidatePreview(prepared shared.PreparedTask) bool {
	return prepared.Position.RequiresAI && !isKeywordMode(prepared.Position)
}
