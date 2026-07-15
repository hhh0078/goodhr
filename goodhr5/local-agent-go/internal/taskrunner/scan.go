// Package taskrunner 文件作用：按职责承载本地任务运行流程的拆分实现。
package taskrunner

import (
	"context"
	"errors"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localdb"
	"goodhr5/local-agent-go/internal/platformcore"
	"goodhr5/local-agent-go/internal/platforms"
	"path/filepath"
	"strings"
	"time"
)

// scanOnce 执行一轮候选人扫描并保存到本地数据库。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置。
func (r *Runner) scanOnce(ctx context.Context, task localdb.Task, platformConfig cloudapi.PlatformConfig, options StartOptions) (map[string]any, error) {
	if r.worker == nil {
		return nil, fmt.Errorf("浏览器 Worker 未配置")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	platformRuntime, err := platforms.RuntimeFor(task.PlatformID)
	if err != nil {
		return nil, err
	}
	exec := platformExecutor{runner: r, taskID: task.ID}
	entryURL := platformEntryURL(platformConfig)
	if entryURL == "" {
		return nil, fmt.Errorf("云端平台配置缺少入口页面地址")
	}
	// 1. 准备平台运行时和浏览器。
	r.taskLog(task.ID, "info", "页面准备：正在打开招聘平台页面")
	workerStatus, err := r.worker.Start(ctx)
	if err != nil {
		return nil, err
	}
	r.taskLog(task.ID, "info", fmt.Sprintf("页面准备：浏览器 Worker 已启动，running=%v，base_url=%s", workerStatus.Running, workerStatus.BaseURL))
	profileName := taskProfileName(task)
	userDataDir := filepath.Join(r.profilesDir, profileName)
	r.taskLog(task.ID, "info", "页面准备：正在启动浏览器账号目录="+profileName)
	viewportWidth, viewportHeight := taskBrowserViewport()
	if _, err := r.worker.Call(ctx, "/api/v1/browser/start", map[string]any{
		"humanize":        true,
		"user_data_dir":   userDataDir,
		"downloads_path":  r.browserDownloadDir(),
		"viewport_width":  viewportWidth,
		"viewport_height": viewportHeight,
	}); err != nil {
		return nil, err
	}
	r.taskLog(task.ID, "info", "页面准备：浏览器启动完成，准备确认当前页面")
	onEntryPage, err := platformRuntime.IsTaskEntryPage(ctx, exec, platformConfig)
	if err != nil {
		r.taskLog(task.ID, "warning", "页面准备：读取当前页面地址失败，将打开入口页面，错误="+err.Error())
	}
	if onEntryPage {
		r.taskLog(task.ID, "info", "页面准备：当前页面已命中入口地址，跳过入口页跳转")
	} else {
		r.taskLog(task.ID, "info", "页面准备：当前页面未命中入口地址，准备打开入口页面")
		if err := platformRuntime.OpenEntryPage(ctx, exec, platformConfig, entryURL); err != nil {
			return nil, err
		}
		r.taskLog(task.ID, "info", "页面准备：招聘平台页面打开完成")
	}
	seen := map[string]struct{}{}
	queue := make([]map[string]any, 0)
	totalSaved := 0
	totalSkipped := 0
	totalGreeted := 0
	totalFailed := 0
	processedCount := 0
	emptyLoads := 0
	positionSearchPrepared := false
	emptyLimit := emptyLoadLimit(options)
	maxItems := maxItemsPerLoad(options)
scanLoop:
	for emptyLoads < emptyLimit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := r.ensureCloudSessionActive(ctx, task, options); err != nil {
			return nil, err
		}
		if len(queue) == 0 {
			skipPositionSelection := shouldSkipPositionSelection(platformRuntime)
			forcePositionSelection := shouldSelectPositionDirectly(platformRuntime)
			// 2. 确认当前网页已经进入任务入口，并切到任务对应岗位。
			r.updateProgress(task.ID, Progress{Stage: "page_ready", Message: "正在确认页面和岗位"})
			r.taskLog(task.ID, "info", "页面准备：正在确认当前页面和岗位")
			if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
				return nil, err
			}
			r.prepareEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig)
			if !positionSearchPrepared {
				if preparer, ok := platformRuntime.(platformcore.PositionSearchPreparer); ok {
					r.taskLog(task.ID, "info", "候选人搜索：正在应用岗位搜索条件")
					if err := preparer.PreparePositionSearch(ctx, exec, platformConfig, task.PositionSnapshot); err != nil {
						return nil, fmt.Errorf("应用岗位搜索条件失败：%w", err)
					}
					r.taskLog(task.ID, "info", "候选人搜索：岗位搜索条件已应用")
					forcePositionSelection = !skipPositionSelection
				}
				positionSearchPrepared = true
			}
			if skipPositionSelection {
				r.taskLog(task.ID, "info", "页面准备：平台无需读取或切换页面岗位，继续候选人流程")
			} else {
				positionName := taskPositionName(task)
				if strings.TrimSpace(positionName) == "" {
					return nil, fmt.Errorf("任务岗位名称为空，无法确认页面岗位")
				}
				if forcePositionSelection {
					r.taskLog(task.ID, "info", "页面准备：平台要求每次选择任务岗位，准备直接切换")
					if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
						return nil, fmt.Errorf("切换页面岗位失败：%w", err)
					}
					r.taskLog(task.ID, "info", "页面准备：任务岗位已选择="+positionName)
				} else {
					currentName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
					if err != nil {
						return nil, fmt.Errorf("获取页面当前岗位失败：%w", err)
					}
					r.taskLog(task.ID, "info", fmt.Sprintf("页面准备：当前岗位=%s，任务岗位=%s", currentName, positionName))
					if strings.Contains(normalizeTaskPositionName(currentName), normalizeTaskPositionName(positionName)) {
						r.taskLog(task.ID, "info", "页面准备：岗位匹配成功")
					} else {
						r.taskLog(task.ID, "warning", "页面准备：岗位不一致，准备切换岗位")
						if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
							return nil, fmt.Errorf("切换页面岗位失败：%w", err)
						}
						confirmedName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
						if err != nil {
							return nil, fmt.Errorf("切换后确认页面岗位失败：%w", err)
						}
						if !strings.Contains(normalizeTaskPositionName(confirmedName), normalizeTaskPositionName(positionName)) {
							return nil, fmt.Errorf("页面切换岗位失败，请手动操作后再点击开始。当前页面岗位=%s，任务岗位=%s", confirmedName, positionName)
						}
						r.taskLog(task.ID, "info", "页面准备：岗位切换完成，当前岗位="+confirmedName)
					}
				}
			}
			delay := pageReadyDelay(options)
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取前等待页面稳定：%s", delay.String()))
			if err := sleepWithContext(ctx, delay); err != nil {
				return nil, err
			}
			// 3. 读取当前屏幕可见候选人，并追加到待处理队列。
			r.updateProgress(task.ID, Progress{Stage: "extracting", Message: "正在提取候选人"})
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取：正在读取当前页面候选人，最多=%d", maxItems))
			platformCandidates, err := platformRuntime.ListVisibleCandidates(ctx, exec, platformConfig, maxItems)
			if err != nil {
				return nil, err
			}
			candidates, duplicateCount := freshCandidates(candidateMaps(platformCandidates), seen)
			if len(candidates) == 0 {
				emptyLoads++
				r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取：本轮没有新候选人，重复=%d，连续空轮次=%d/%d", duplicateCount, emptyLoads, emptyLimit))
				if emptyLoads >= emptyLimit {
					r.taskLog(task.ID, "info", "候选人提取：达到连续空轮次上限，停止继续滚动")
					break
				}
				if err := r.scrollForMoreCandidates(ctx, task.ID, platformRuntime, exec, platformConfig, options); err != nil {
					return nil, err
				}
				continue
			}
			emptyLoads = 0
			queue = append(queue, candidates...)
			r.syncProcessedResumeCount(ctx, task, len(candidates), options)
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取：读取完成，本次新增=%d，重复=%d，待处理=%d，已处理=%d", len(candidates), duplicateCount, len(queue), processedCount))
		}
		candidates := queue
		queue = nil
		filtered, skipped := r.prepareCandidatesForFirstStage(task, candidates)
		totalSkipped += skipped
		r.taskLog(task.ID, "info", fmt.Sprintf("列表过滤：完成，保留=%d，跳过=%d", len(filtered), skipped))
		if skipped > 0 {
			r.taskLog(task.ID, "info", fmt.Sprintf("列表过滤：有 %d 个候选人已跳过", skipped))
		}
		if len(filtered) > 0 {
			r.updateProgress(task.ID, Progress{Stage: "pipeline", Message: fmt.Sprintf("正在处理候选人队列，待处理 %d 个", len(filtered))})
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人处理：队列开始，数量=%d", len(filtered)))

			// 4. 并发做“是否值得看详情”的预评分，但主流程仍按页面顺序消费候选人。
			batchResult := batchProcessResult{}
			aiClient, err := r.pipelineAIClient(task, options)
			if err != nil {
				return nil, err
			}
			precheckCh := make(chan candidatePipelineResult, len(filtered))
			aiJobs := make(chan candidatePipelineResult, len(filtered))
			needsAI := taskMode(task) == "ai"
			if needsAI {
				workerCount := candidatePipelineConcurrency(len(filtered))
				r.taskLog(task.ID, "info", fmt.Sprintf("AI 预判断：开始并发分析，数量=%d，并发数=%d", len(filtered), workerCount))
				r.startCandidateDetailWorkers(ctx, task, exec, aiClient, aiJobs, precheckCh, workerCount)
			}
			go r.feedCandidatePipeline(ctx, task, filtered, needsAI, aiJobs, precheckCh)

			pending := map[int]candidatePipelineResult{}
			nextIndex := 0
			for nextIndex < len(filtered) {
				if r.isUserStopped(task.ID) {
					break
				}
				if reachedRunGreetLimit(task, totalGreeted+batchResult.Greeted) {
					r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取：达到本次打招呼上限，停止继续处理，上限=%d", task.MatchLimit))
					break
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				item, ok := pending[nextIndex]
				if !ok {
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case received := <-precheckCh:
						pending[received.Index] = received
						continue
					case <-time.After(150 * time.Millisecond):
						continue
					}
				}
				delete(pending, nextIndex)
				nextIndex++
				if item.Err != nil {
					r.taskLog(task.ID, "error", fmt.Sprintf("候选人处理：失败，序号=%d，错误=%v", item.Index+1, item.Err))
					if errors.Is(item.Err, context.Canceled) || isBrowserClosedTaskError(item.Err) {
						return nil, item.Err
					}
					item.Candidate["status"] = "failed"
					item.Candidate["error"] = item.Err.Error()
					batchResult.Failed++
					continue
				}

				candidate := item.Candidate
				processedCount++
				candidateName := candidateLogName(candidate)
				candidateCtx, candidateCancel := context.WithTimeout(ctx, candidateTotalTimeout)
				var detailSession *candidateDetailSession
				r.taskLog(task.ID, "info", fmt.Sprintf("候选人处理：开始处理，本页序号=%d/%d，累计处理=%d，姓名=%s，状态=%s，超时=%s", item.Index+1, len(filtered), processedCount, candidateName, stringFromMap(candidate, "status"), candidateTotalTimeout.Round(time.Second)))
				batchResult.Skipped += item.Skipped
				r.ensureCandidateVisibleBeforeDecision(candidateCtx, task.ID, platformRuntime, exec, platformConfig, platformcore.Candidate(candidate))

				// 5. 如果预评分通过，再打开详情；详情模式由任务配置决定：DOM、OCR 或 AI 图片。
				if item.DetailDecision != nil {
					decision := item.DetailDecision
					candidate["ai_detail_score"] = decision.Score
					candidate["ai_detail_reason"] = decision.Reason
					candidate["ai_detail_threshold"] = decision.Threshold
					candidate["ai_detail_usage"] = decision.Usage
					candidate["ai_detail_elapsed_ms"] = decision.ElapsedMS
					if !decision.ShouldOpenDetail {
						candidate["status"] = "skipped"
						candidate["skip_reason"] = fmt.Sprintf("详情评分低于阈值：%.1f/%.1f，%s", decision.Score, decision.Threshold, decision.Reason)
						batchResult.Skipped++
						r.taskLog(task.ID, "info", fmt.Sprintf("AI 预判断：跳过候选人，候选人=%s，分数=%.1f，阈值=%.1f，原因=%s", candidateLogName(candidate), decision.Score, decision.Threshold, decision.Reason))
					} else {
						r.taskLog(task.ID, "info", fmt.Sprintf("AI 预判断：完成，候选人=%s，分数=%.1f，阈值=%.1f，是否看详情=是", candidateLogName(candidate), decision.Score, decision.Threshold))
						itemSkipped, nextDetailSession, err := r.enrichCandidateWithDetail(candidateCtx, task, platformRuntime, exec, platformConfig, candidate, aiClient, options)
						detailSession = nextDetailSession
						batchResult.Skipped += itemSkipped
						if err != nil {
							candidateCancel()
							return nil, err
						}
					}
				}

				// 6. 非 AI 主模式下，如果任务要求看详情，也按配置读取详情。
				if !needsAI && shouldFetchDetail(task) && canContinueCandidate(stringFromMap(candidate, "status")) {
					if taskMode(task) == "keyword" && !shouldOpenDetailByProbability(options) {
						candidate["status"] = "skipped"
						candidate["skip_reason"] = fmt.Sprintf("未命中打开详情概率：%d%%", detailOpenProbability(options))
						batchResult.Skipped++
						r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：候选人已跳过，候选人=%s，原因=未命中打开详情概率%d%%", candidateLogName(candidate), detailOpenProbability(options)))
					} else {
						r.taskLog(task.ID, "info", fmt.Sprintf("详情读取：准备打开详情，候选人=%s，模式=%s", candidateLogName(candidate), detailModeLabel(detailMode(task))))
						itemSkipped, nextDetailSession, err := r.enrichCandidateWithDetail(candidateCtx, task, platformRuntime, exec, platformConfig, candidate, aiClient, options)
						detailSession = nextDetailSession
						batchResult.Skipped += itemSkipped
						if err != nil {
							candidateCancel()
							return nil, err
						}
					}
				}

				// 7. 第二次详情分析：详情 AI 已经一次性评分时跳过；否则按任务模式做最终判断。
				if _, supportsDetailScrolling := platformRuntime.(platformcore.DetailAnalysisScroller); detailSession != nil && !supportsDetailScrolling {
					_ = detailSession.Close(context.WithoutCancel(candidateCtx))
					detailSession = nil
				}
				shouldFinalizeWithAI := canContinueCandidate(stringFromMap(candidate, "status")) && !boolFromMap(candidate, "ai_greet_scored")
				stopDetailScrolling := func() {}
				if detailSession != nil && shouldFinalizeWithAI && taskMode(task) != "keyword" {
					stopDetailScrolling = r.startCandidateDetailScrolling(candidateCtx, task.ID, platformRuntime, exec, platformConfig, candidate)
				}
				if shouldFinalizeWithAI {
					itemSkipped, err := r.finalizeCandidateGreetDecision(candidateCtx, task, exec, candidate, aiClient)
					batchResult.Skipped += itemSkipped
					if err != nil {
						candidate["status"] = "failed"
						candidate["error"] = err.Error()
						batchResult.Failed++
						r.taskLog(task.ID, "warning", fmt.Sprintf("打招呼判断：失败，候选人=%s，错误=%s", candidateLogName(candidate), err.Error()))
					}
				}
				stopDetailScrolling()
				if detailSession != nil {
					_ = detailSession.Close(context.WithoutCancel(candidateCtx))
					detailSession = nil
				}

				// 8. 评分通过后执行打招呼，然后保存候选人结果。
				if options.EnableGreet {
					greeted, failed, itemSkipped, err := r.consumeCandidateForGreet(candidateCtx, task, platformRuntime, exec, platformConfig, candidate, totalGreeted+batchResult.Greeted, options)
					if err != nil {
						candidateCancel()
						return nil, err
					}
					batchResult.Greeted += greeted
					batchResult.Failed += failed
					batchResult.Skipped += itemSkipped
					if greeted > 0 {
						r.incrementRunGreeted(task.ID, greeted)
					}
				}

				status := stringFromMap(candidate, "status")
				if shouldSaveCandidateResult(status) {
					r.taskLog(task.ID, "info", fmt.Sprintf("结果保存：准备保存候选人，候选人=%s，状态=%s", candidateName, status))
					r.saveCandidateResult(ctx, task, candidate, options)
					r.taskLog(task.ID, "info", fmt.Sprintf("候选人处理：候选人处理完成，姓名=%s，结果=%s", candidateLogName(candidate), status))
					batchResult.Saved++
				}
				if errors.Is(candidateCtx.Err(), context.DeadlineExceeded) {
					candidate["status"] = "failed"
					candidate["error"] = fmt.Sprintf("候选人处理总超时：超过%s", candidateTotalTimeout.Round(time.Second))
					batchResult.Failed++
					r.taskLog(task.ID, "error", fmt.Sprintf("候选人处理：超时，姓名=%s，超过=%s", candidateName, candidateTotalTimeout.Round(time.Second)))
				}
				candidateCancel()
				if err := r.maybeRestAfterCandidate(ctx, task.ID, exec, options); err != nil {
					return nil, err
				}
				if r.isUserStopped(task.ID) {
					r.taskLog(task.ID, "info", "任务停止：当前候选人处理完成，按停止请求结束任务")
					break
				}
			}

			totalSaved += batchResult.Saved
			totalSkipped += batchResult.Skipped
			totalGreeted += batchResult.Greeted
			totalFailed += batchResult.Failed
			r.taskLog(task.ID, "info", fmt.Sprintf("候选人处理：队列完成，保存=%d，跳过=%d，打招呼=%d，失败=%d", batchResult.Saved, batchResult.Skipped, batchResult.Greeted, batchResult.Failed))
			if r.isUserStopped(task.ID) {
				break scanLoop
			}
			if reachedRunGreetLimit(task, totalGreeted) {
				break scanLoop
			}
		}
		if err := r.scrollForMoreCandidates(ctx, task.ID, platformRuntime, exec, platformConfig, options); err != nil {
			return nil, err
		}
	}
	if totalSaved > 0 || totalSkipped > 0 {
		updatedTask, err := r.db.IncrementTaskCounts(task.ID, totalSaved, totalGreeted, totalSkipped, totalFailed)
		if err == nil {
			r.syncTaskCounts(ctx, updatedTask, options)
		}
		r.taskLog(task.ID, "info", fmt.Sprintf("候选人提取：本次扫描结束，保存=%d，跳过=%d，打招呼=%d，失败=%d", totalSaved, totalSkipped, totalGreeted, totalFailed))
	} else {
		r.taskLog(task.ID, "warning", "候选人提取：当前页面未提取到可见候选人，请确认账号已登录且页面在推荐列表")
	}
	return map[string]any{
		"candidates_count": totalSaved,
		"skipped_count":    totalSkipped,
		"greeted_count":    totalGreeted,
		"failed_count":     totalFailed,
		"processed_count":  processedCount,
		"entry_url":        entryURL,
	}, nil
}

// ensureCloudSessionActive 确认当前账号登录态仍然有效。
// ctx 为任务上下文，task 为本地任务，options 为启动参数。
func (r *Runner) ensureCloudSessionActive(ctx context.Context, task localdb.Task, options StartOptions) error {
	token := strings.TrimSpace(options.Token)
	if token == "" {
		return nil
	}
	baseURL := strings.TrimSpace(options.CloudAPIBase)
	if baseURL == "" {
		baseURL = strings.TrimSpace(r.cloudAPIBase)
	}
	if baseURL == "" {
		baseURL = "https://goodhr5.58it.cn"
	}
	err := cloudapi.New(baseURL).ValidateSession(ctx, token)
	if err == nil {
		return nil
	}
	var authErr cloudapi.AuthExpiredError
	if !errors.As(err, &authErr) {
		r.taskLog(task.ID, "warning", "账号验证暂时失败，先继续任务："+err.Error())
		return nil
	}
	message := "账号已在其他地方登录，当前任务已停止。请重新登录后再启动任务。"
	r.taskLog(task.ID, "warning", message)
	r.updateProgress(task.ID, Progress{Stage: "stopped", Message: message})
	_, _ = r.db.UpdateTaskStatus(task.ID, "stopped")
	r.sendTaskFailNotification(context.Background(), task.ID, message, options)
	return authErr
}

// scrollForMoreCandidates 滚动候选人列表以加载更多候选人。
// ctx 为请求上下文，taskID 为任务 ID，platformRuntime 为平台执行器，exec 为 Worker 执行器，platformConfig 为平台配置，options 为任务启动参数。
func (r *Runner) scrollForMoreCandidates(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, options StartOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.updateProgress(taskID, Progress{Stage: "scrolling", Message: "正在加载更多候选人"})
	scrollDistance := randomScrollDistance(options)
	r.taskLog(taskID, "info", fmt.Sprintf("候选人提取：准备滚动加载更多候选人，距离=%dpx", scrollDistance))
	if err := platformRuntime.ScrollCandidateList(ctx, exec, platformConfig, scrollDistance); err != nil {
		r.taskLog(taskID, "warning", "候选人提取：滚动失败，错误="+err.Error())
		return nil
	}
	r.taskLog(taskID, "info", fmt.Sprintf("候选人提取：滚动完成，距离=%dpx", scrollDistance))
	return nil
}

// ensureCandidateVisibleBeforeDecision 在查看候选人分数前先滚动到对应卡片，确保页面位置随候选人顺序推进。
// ctx 为候选人处理上下文，taskID 为任务 ID，platformRuntime 为平台运行时，exec 为 Worker 执行器，platformConfig 为平台配置，candidate 为候选人。
func (r *Runner) ensureCandidateVisibleBeforeDecision(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig, candidate platformcore.Candidate) {
	visibleRuntime, ok := platformRuntime.(candidateVisibleRuntime)
	if !ok {
		return
	}
	name := candidateLogName(map[string]any(candidate))
	r.taskLog(taskID, "info", fmt.Sprintf("候选人处理：查看分数前确认候选人可见，姓名=%s", name))
	if err := visibleRuntime.EnsureCandidateVisible(ctx, exec, platformConfig, candidate); err != nil {
		r.taskLog(taskID, "warning", fmt.Sprintf("候选人处理：查看分数前滚动到位失败，姓名=%s，错误=%s", name, err.Error()))
		return
	}
	r.taskLog(taskID, "info", fmt.Sprintf("候选人处理：候选人已在可见范围，姓名=%s", name))
}

// ensureTaskPageReady 确认当前页面和岗位与任务匹配。
// ctx 为请求上下文，task 为任务记录，platformConfig 为云端平台配置。
func (r *Runner) ensureTaskPageReady(ctx context.Context, task localdb.Task, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.waitTaskEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig); err != nil {
		return err
	}
	r.prepareEntryPage(ctx, task.ID, platformRuntime, exec, platformConfig)
	positionName := taskPositionName(task)
	if strings.TrimSpace(positionName) == "" {
		return fmt.Errorf("任务岗位名称为空，无法确认页面岗位")
	}
	if shouldSkipPositionSelection(platformRuntime) {
		r.taskLog(task.ID, "info", "页面准备：平台无需读取或切换页面岗位，继续候选人流程")
		return nil
	}
	if shouldSelectPositionDirectly(platformRuntime) {
		r.taskLog(task.ID, "info", "页面准备：平台无需读取当前岗位，准备直接切换任务岗位")
		if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
			return fmt.Errorf("切换页面岗位失败：%w", err)
		}
		r.taskLog(task.ID, "info", "页面准备：任务岗位已选择="+positionName)
		return nil
	}
	currentName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
	if err != nil {
		return fmt.Errorf("获取页面当前岗位失败：%w", err)
	}
	if strings.Contains(normalizeTaskPositionName(currentName), normalizeTaskPositionName(positionName)) {
		r.taskLog(task.ID, "info", "页面岗位匹配："+currentName)
		return nil
	}
	r.taskLog(task.ID, "warning", fmt.Sprintf("页面岗位与任务岗位不一致，准备切换：页面=%s，任务=%s", currentName, positionName))
	if err := platformRuntime.SelectPosition(ctx, exec, platformConfig, positionName); err != nil {
		return fmt.Errorf("切换页面岗位失败：%w", err)
	}
	confirmedName, err := r.waitCurrentPositionName(ctx, task.ID, platformRuntime, exec, platformConfig)
	if err != nil {
		return fmt.Errorf("切换后确认页面岗位失败：%w", err)
	}
	if strings.Contains(normalizeTaskPositionName(confirmedName), normalizeTaskPositionName(positionName)) {
		r.taskLog(task.ID, "info", "页面岗位已切换为："+confirmedName)
		return nil
	}
	return fmt.Errorf("页面切换岗位失败，请手动操作后再点击开始。当前页面岗位=%s，任务岗位=%s", confirmedName, positionName)
}

// shouldSkipPositionSelection 判断平台是否应跳过全部页面岗位处理。
// platformRuntime 为当前平台运行时。
func shouldSkipPositionSelection(platformRuntime platformcore.Runtime) bool {
	skipper, ok := platformRuntime.(platformcore.PositionSelectionSkipper)
	return ok && skipper.ShouldSkipPositionSelection()
}

// shouldSelectPositionDirectly 判断平台是否要求跳过当前岗位读取并直接切换。
// platformRuntime 为当前平台运行时。
func shouldSelectPositionDirectly(platformRuntime platformcore.Runtime) bool {
	selector, ok := platformRuntime.(platformcore.DirectPositionSelector)
	return ok && selector.ShouldSelectPositionDirectly()
}

// prepareEntryPage 调用平台入口页准备动作，失败时只记录日志不中断主流程。
// taskID 为任务 ID，platformRuntime 为平台实现，exec 为浏览器执行器，platformConfig 为云端平台配置。
func (r *Runner) prepareEntryPage(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) {
	if err := ctx.Err(); err != nil {
		return
	}
	r.taskLog(taskID, "info", "正在执行平台入口页准备动作")
	if err := platformRuntime.PrepareEntryPage(ctx, exec, platformConfig); err != nil {
		r.taskLog(taskID, "warning", "平台入口页准备动作失败，继续主流程："+err.Error())
		return
	}
	r.taskLog(taskID, "info", "平台入口页准备动作完成")
}

// waitTaskEntryPage 等待当前页面加载到任务入口页。
// ctx 为请求上下文，taskID 为任务 ID，platformConfig 为平台配置。
func (r *Runner) waitTaskEntryPage(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) error {
	attempts := pageEntryCheckAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		r.taskLog(taskID, "info", fmt.Sprintf("正在等待页面加载，第 %d/%d 次", attempt, attempts))
		if err := sleepWithContext(ctx, pageEntryCheckDelay); err != nil {
			return err
		}
		ok, err := platformRuntime.IsTaskEntryPage(ctx, exec, platformConfig)
		if err != nil {
			lastErr = err
			r.taskLog(taskID, "warning", fmt.Sprintf("检查当前页面失败，第 %d/%d 次：%s", attempt, attempts, err.Error()))
			continue
		}
		if ok {
			r.taskLog(taskID, "info", fmt.Sprintf("当前页面已确认，第 %d/%d 次检查成功", attempt, attempts))
			return nil
		}
		lastErr = fmt.Errorf("网页还没有加载到任务入口页")
	}
	if lastErr != nil {
		return fmt.Errorf("检查当前页面失败：%w", lastErr)
	}
	return fmt.Errorf("检查当前页面失败")
}

// waitCurrentPositionName 等待页面当前岗位名称可读取。
// ctx 为请求上下文，taskID 为任务 ID，platformConfig 为平台配置。
func (r *Runner) waitCurrentPositionName(ctx context.Context, taskID string, platformRuntime platformcore.Runtime, exec platformExecutor, platformConfig cloudapi.PlatformConfig) (string, error) {
	attempts := currentPositionCheckAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		r.taskLog(taskID, "info", fmt.Sprintf("正在读取页面当前岗位，第 %d/%d 次", attempt, attempts))
		if err := sleepWithContext(ctx, currentPositionCheckDelay); err != nil {
			return "", err
		}
		name, err := platformRuntime.CurrentPositionName(ctx, exec, platformConfig)
		if err == nil {
			return name, nil
		}
		lastErr = err
		r.taskLog(taskID, "warning", fmt.Sprintf("读取页面当前岗位失败，第 %d/%d 次：%s", attempt, attempts, err.Error()))
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("页面当前岗位为空")
}

// browserDownloadDir 返回任务运行时使用的下载目录。
// 优先读取本地设置，没有设置时使用默认下载目录。
func (r *Runner) browserDownloadDir() string {
	settings, err := r.db.GetSettings()
	if err == nil {
		if value := stringFromMap(settings, "browser_download_dir"); value != "" {
			return value
		}
		if value := stringFromMap(settings, "downloads_dir"); value != "" {
			return value
		}
	}
	return r.downloadsDir
}

// platformEntryURL 读取平台推荐页入口。
// platformConfig 为云端平台配置。
func platformEntryURL(platformConfig cloudapi.PlatformConfig) string {
	if url := stringFromMap(platformEntryPage(platformConfig), "url"); url != "" {
		return url
	}
	if url := stringFromMap(platformConfig, "url"); url != "" {
		return url
	}
	return pageEntryURL(platformConfig)
}

// platformEntryPage 读取平台任务入口页配置。
// platformConfig 为云端平台配置。
func platformEntryPage(platformConfig cloudapi.PlatformConfig) map[string]any {
	if page := entryPageFromAny(mapFromAny(platformConfig["auth"])); len(page) > 0 {
		return page
	}
	if page := entryPageFromAny(platformConfig); len(page) > 0 {
		return page
	}
	if url := stringFromMap(platformConfig, "url"); url != "" {
		return map[string]any{"url": url}
	}
	return nil
}

// entryPageFromAny 从包含 pages 的对象中读取入口页。
// value 为配置对象。
func entryPageFromAny(value any) map[string]any {
	pages := pageList(value)
	if len(pages) == 0 {
		return nil
	}
	for _, page := range pages {
		if boolFromMap(page, "entry") && stringFromMap(page, "url") != "" {
			return page
		}
	}
	for _, page := range pages {
		if stringFromMap(page, "url") != "" {
			return page
		}
	}
	return nil
}

// pageEntryURL 从页面配置中读取入口地址。
// value 为包含 pages 的配置对象或 pages 数组，优先返回 entry=true 的页面。
func pageEntryURL(value any) string {
	pages := pageList(value)
	if len(pages) == 0 {
		return ""
	}
	for _, page := range pages {
		if boolFromMap(page, "entry") {
			if url := stringFromMap(page, "url"); url != "" {
				return url
			}
		}
	}
	for _, page := range pages {
		if url := stringFromMap(page, "url"); url != "" {
			return url
		}
	}
	return ""
}

// pageList 从平台配置对象或数组中读取 pages 列表。
// value 为配置对象或 pages 数组。
func pageList(value any) []map[string]any {
	if value == nil {
		return nil
	}
	if section, ok := value.(cloudapi.PlatformConfig); ok {
		value = section["pages"]
	}
	if section, ok := value.(map[string]any); ok {
		value = section["pages"]
	}
	if typedPages, ok := value.([]map[string]any); ok {
		return typedPages
	}
	pages, ok := value.([]any)
	if !ok || len(pages) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(pages))
	for _, item := range pages {
		if page, ok := item.(map[string]any); ok {
			result = append(result, page)
		}
	}
	return result
}

// taskPositionName 返回任务岗位名称。
// task 为任务记录。
func taskPositionName(task localdb.Task) string {
	return stringFromMap(task.PositionSnapshot, "name")
}

// normalizeTaskPositionName 规范化岗位名称用于比较。
// value 为原始岗位名称。
func normalizeTaskPositionName(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), "")
}
