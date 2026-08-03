// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

import (
	"context"
	"errors"
	"fmt"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"strings"
	"time"
)

// withOperationTimeout 给单个候选人的关键操作加超时、异常捕获和耗时日志。
func (r *Runner) withOperationTimeout(ctx context.Context, positionID string, candidateName string, operation string, timeout time.Duration, fn func(context.Context) error) (err error) {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "未命名操作"
	}
	candidateName = strings.TrimSpace(candidateName)
	if candidateName == "" {
		candidateName = "未知候选人"
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	startedAt := time.Now()
	r.positionLog(positionID, "info", fmt.Sprintf("%s：开始，候选人=%s，超时=%s", operation, candidateName, timeout.Round(time.Second)))
	defer func() {
		elapsed := time.Since(startedAt).Round(time.Millisecond)
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s异常：%v", operation, recovered)
		}
		if errors.Is(opCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("%s超时：超过%s", operation, timeout.Round(time.Second))
		}
		if err != nil {
			if errors.Is(opCtx.Err(), context.DeadlineExceeded) {
				r.positionLog(positionID, "error", fmt.Sprintf("%s：超时，候选人=%s，超过=%s，耗时=%s，错误=%s", operation, candidateName, timeout.Round(time.Second), elapsed, err.Error()))
				return
			}
			r.positionLog(positionID, "warning", fmt.Sprintf("%s：失败，候选人=%s，耗时=%s，错误=%s", operation, candidateName, elapsed, err.Error()))
			return
		}
		r.positionLog(positionID, "info", fmt.Sprintf("%s：完成，候选人=%s，耗时=%s", operation, candidateName, elapsed))
	}()
	return fn(opCtx)
}

// startCandidateDetailWorkers 启动候选人看详情评分并发处理池。
// workerCount 为并发数量，aiJobs 为待评分候选人队列，resultCh 为完成结果队列。
func (r *Runner) startCandidateDetailWorkers(ctx context.Context, position localdb.Position, exec platformExecutor, aiClient *localai.Client, aiJobs <-chan candidatePipelineResult, resultCh chan<- candidatePipelineResult, workerCount int) {
	if workerCount <= 0 {
		workerCount = 1
	}
	for i := 0; i < workerCount; i++ {
		go func() {
			for item := range aiJobs {
				if err := ctx.Err(); err != nil {
					item.Err = err
					resultCh <- item
					continue
				}
				showOverlay := item.Index == 0
				title := ""
				subtitle := ""
				if showOverlay {
					title = "AI 正在预分析"
					subtitle = candidateLogName(item.Candidate)
				}
				visibleClient, cleanup := r.aiClientForCall(ctx, exec, aiClient, title, subtitle, "正在判断是否值得打开详情")
				var decision localai.Decision
				err := r.withOperationTimeout(ctx, position.ID, candidateLogName(item.Candidate), "AI基础预评分", aiPrecheckTimeout, func(opCtx context.Context) error {
					nextDecision, scoreErr := r.scoreCandidateForDetail(opCtx, position, item.Candidate, visibleClient)
					decision = nextDecision
					return scoreErr
				})
				cleanup()
				if err == nil {
					item.DetailDecision = &decision
					if showOverlay {
						r.showAIReply(ctx, exec, title, subtitle, formatDetailDecisionReply(decision))
					}
				}
				item.Err = err
				resultCh <- item
			}
		}()
	}
}

// feedCandidatePipeline 按页面顺序把候选人送入看详情评分队列。
// needsAI 表示是否需要 AI 评分，aiJobs 为 AI 队列，resultCh 为最终结果队列。
func (r *Runner) feedCandidatePipeline(ctx context.Context, position localdb.Position, candidates []map[string]any, needsAI bool, aiJobs chan<- candidatePipelineResult, resultCh chan<- candidatePipelineResult) {
	if needsAI {
		defer close(aiJobs)
	}
	for index, candidate := range candidates {
		item := candidatePipelineResult{Index: index, Candidate: candidate}
		if err := ctx.Err(); err != nil {
			item.Err = err
			resultCh <- item
			return
		}
		if item.Err != nil || !needsAI || !canContinueCandidate(stringFromMap(candidate, "status")) {
			resultCh <- item
			continue
		}
		select {
		case aiJobs <- item:
		case <-ctx.Done():
			item.Err = ctx.Err()
			resultCh <- item
			return
		}
	}
}

// pipelineAIClient 创建流水线使用的 AI 客户端。
// position 为岗位运行记录，options 为岗位运行启动参数，只有 AI 模式或 AI 详情模式时才读取配置。
func (r *Runner) pipelineAIClient(position localdb.Position, options StartOptions) (*localai.Client, error) {
	if positionMode(position) != "ai" && detailMode(position) != "ai" {
		return nil, nil
	}
	config := options.AIConfig
	if err := validateAIConfig(config); err != nil {
		return nil, err
	}
	client := localai.New(config)
	client.EnableThinking = position.EnableThinking
	return client, nil
}

// candidatePipelineConcurrency 返回候选人后台处理并发数。
// total 为本批候选人数量。
func candidatePipelineConcurrency(total int) int {
	if total <= 0 {
		return 1
	}
	if total < defaultCandidatePipelineConcurrency {
		return total
	}
	return defaultCandidatePipelineConcurrency
}

// reachedRunGreetLimit 判断本次运行是否已经达到打招呼上限。
// position 为岗位运行配置，greeted 为本次运行已成功打招呼数量。
func reachedRunGreetLimit(position localdb.Position, greeted int) bool {
	return position.MatchLimit > 0 && greeted >= position.MatchLimit
}
