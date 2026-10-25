// Package greeting 文件作用：并发完成候选人基础信息 AI 预判断，并按页面原顺序返回处理结果。
package greeting

import (
	"context"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const candidatePreviewConcurrency = 5
const candidatePreviewPollInterval = 150 * time.Millisecond

// candidatePreviewResult 保存候选人基础信息预判断结果和原页面序号。
type candidatePreviewResult struct {
	Index     int
	Candidate model.Candidate
	Decision  *ai.Decision
	Err       error
	AttemptID string
}

// candidatePreviews 并发执行 AI 基础预判断，并通过结果通道严格按页面顺序输出。
func (f *Flow) candidatePreviews(
	ctx context.Context,
	prepared shared.PreparedTask,
	candidates []model.Candidate,
) <-chan candidatePreviewResult {
	ordered, _ := f.candidatePreviewsWithDone(ctx, prepared, candidates)
	return ordered
}

// candidatePreviewsWithDone 在调用现场先落盘，再交回有序通道；done供任务结束前等候已调用判定的持久化收尾。
func (f *Flow) candidatePreviewsWithDone(ctx context.Context, prepared shared.PreparedTask, candidates []model.Candidate) (<-chan candidatePreviewResult, <-chan struct{}) {
	ordered := make(chan candidatePreviewResult, len(candidates))
	done := make(chan struct{})
	if !usesCandidatePreview(prepared) {
		for index, candidate := range candidates {
			ordered <- candidatePreviewResult{Index: index, Candidate: candidate}
		}
		close(ordered)
		close(done)
		return ordered, done
	}

	startedAt := time.Now()
	reportAILoading(
		f.Logger,
		prepared.Request.TaskID,
		"本页候选人",
		"preview",
		"AI 正在根据候选人基础信息预分析",
	)
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
				if ctx.Err() != nil {
					return
				}
				// 平台确认无需处理的推荐卡片不调用 AI，保持原顺序交给主流程正常跳过。
				if strings.TrimSpace(item.Candidate.SkipReason) == "" {
					var started time.Time
					item.AttemptID, item.Err = f.beginScreeningAttempt(ctx, prepared, item.Candidate)
					if item.Err == nil {
						started, item.Err = f.startScreeningRound(ctx, item.AttemptID, 1, "ai")
					}
					if item.Err == nil {
						evaluationCtx, cancelEvaluation := context.WithTimeout(context.WithoutCancel(ctx), candidateTimeout)
						stopOnCancel := context.AfterFunc(ctx, cancelEvaluation)
						evaluation, err := f.AI.EvaluateCandidatePreviewEarly(
							evaluationCtx,
							prepared.Position.AI,
							prepared.Position,
							item.Candidate,
						)
						stopOnCancel()
						decision := evaluation.Decision
						item.Err = err
						var result *ai.Decision
						if err == nil {
							item.Decision = &decision
							result = &decision
						}
						if saveErr := f.finishScreeningRound(ctx, prepared, item.Candidate, item.AttemptID, 1, "ai", started, result, previewScoreThreshold(prepared.Position), err); saveErr != nil {
							item.Err = saveErr
							item.Decision = nil
						}
						if err != nil || evaluation.Final == nil {
							cancelEvaluation()
						} else {
							go f.finishPreviewOutput(prepared.Request.TaskID, evaluation.Final, cancelEvaluation)
						}
					}
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
		close(done)
	}()
	go orderCandidatePreviews(ctx, results, ordered)
	return ordered, done
}

// finishPreviewOutput 收完已经产生评分的第一轮尾部，释放请求预算，尾部失败不伪造或覆盖已记录评分。
func (f *Flow) finishPreviewOutput(taskID string, final <-chan ai.EvaluationResult, cancel context.CancelFunc) {
	defer cancel()
	if result := <-final; result.Err != nil {
		f.log(taskID, "complete_ai_preview_output", "warning", time.Now(), result.Err)
	}
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

// waitCandidatePreview 每 150 毫秒醒来检查一次，直到下一位候选人的预判断完成。
func waitCandidatePreview(
	ctx context.Context,
	stop <-chan struct{},
	ordered <-chan candidatePreviewResult,
) (candidatePreviewResult, bool, error) {
	ticker := time.NewTicker(candidatePreviewPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return candidatePreviewResult{}, false, ctx.Err()
		case <-stop:
			return candidatePreviewResult{}, false, nil
		case item, ok := <-ordered:
			return item, ok, nil
		case <-ticker.C:
		}
	}
}

// usesCandidatePreview 判断当前岗位是否需要在打开详情前执行 AI 基础预判断。
func usesCandidatePreview(prepared shared.PreparedTask) bool {
	return prepared.Position.RequiresAI && !isKeywordMode(prepared.Position)
}
