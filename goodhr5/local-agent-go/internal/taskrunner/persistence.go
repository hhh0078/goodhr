// Package taskrunner 文件作用：按职责承载本地任务运行流程的拆分实现。
package taskrunner

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/localai"
	"goodhr5/local-agent-go/internal/localdb"
	"strings"
	"time"
)

// syncProcessedResumeCount 将去重后的新增候选人数量同步给云端公开统计。
// ctx 为请求上下文，task 为任务记录，count 为新增候选人数量，options 为任务启动参数。
func (r *Runner) syncProcessedResumeCount(ctx context.Context, task localdb.Task, count int, options StartOptions) {
	if count <= 0 || strings.TrimSpace(options.Token) == "" {
		return
	}
	err := r.withOperationTimeout(ctx, task.ID, task.Name, "同步已处理简历数", cloudStatsSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).AddProcessedResumes(syncCtx, options.Token, task.ID, count)
	})
	if err != nil {
		r.taskLog(task.ID, "warning", "同步已处理简历数失败："+err.Error())
	}
}

// syncTaskCounts 将本地任务累计统计同步给云端任务列表。
// ctx 为请求上下文，task 为本地任务记录，options 为任务启动参数。
func (r *Runner) syncTaskCounts(ctx context.Context, task localdb.Task, options StartOptions) {
	if strings.TrimSpace(task.ID) == "" || strings.TrimSpace(options.Token) == "" {
		return
	}
	counts := map[string]any{
		"scanned_count": task.ScannedCount,
		"greeted_count": task.GreetedCount,
		"skipped_count": task.SkippedCount,
		"failed_count":  task.FailedCount,
	}
	r.taskLog(task.ID, "info", fmt.Sprintf("统计同步：准备同步任务统计，扫描=%d，打招呼=%d，跳过=%d，失败=%d", task.ScannedCount, task.GreetedCount, task.SkippedCount, task.FailedCount))
	err := r.withOperationTimeout(ctx, task.ID, task.Name, "同步任务统计", cloudStatsSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).SyncTaskCounts(syncCtx, options.Token, task.ID, counts)
	})
	if err != nil {
		r.taskLog(task.ID, "warning", "统计同步：同步失败，错误="+err.Error())
	}
}

// saveCandidateResult 将候选人结果同步到云端简历库。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) saveCandidateResult(ctx context.Context, task localdb.Task, candidate map[string]any, options StartOptions) {
	if strings.TrimSpace(options.Token) == "" {
		r.taskLog(task.ID, "warning", "结果保存：云端同步失败，错误=缺少登录 token")
		return
	}
	if r.savePendingAIVisionCandidateAsync(ctx, task, candidate, options) {
		return
	}
	payload := cloneCandidateForCloud(task, candidate)
	r.saveCandidatePayload(ctx, task, payload, options)
}

// cloneCandidateForCloud 生成候选人云端入库 JSON。
// task 为任务记录，candidate 为本地候选人结果。
func cloneCandidateForCloud(task localdb.Task, candidate map[string]any) map[string]any {
	payload := make(map[string]any, len(candidate)+5)
	for key, value := range candidate {
		if strings.HasPrefix(key, "_pending_") {
			continue
		}
		payload[key] = value
	}
	payload["task_id"] = task.ID
	payload["platform_id"] = task.PlatformID
	payload["position_id"] = task.PositionID
	payload["platform_account_id"] = task.PlatformAccountID
	payload["ai"] = candidateAIResult(payload)
	if _, ok := payload["candidate_name"]; !ok {
		payload["candidate_name"] = candidateLogName(candidate)
	}
	return payload
}

// candidateAIResult 将两次真实 AI 分析结果组装成展示用嵌套结构。
// payload 为即将同步云端的候选人数据，返回 ai.detail 和 ai.greet。
func candidateAIResult(payload map[string]any) map[string]any {
	return map[string]any{
		"detail": map[string]any{
			"score":  payload["ai_detail_score"],
			"reason": payload["ai_detail_reason"],
		},
		"greet": map[string]any{
			"score":  payload["ai_greet_score"],
			"reason": payload["ai_greet_reason"],
		},
	}
}

// savePendingAIVisionCandidateAsync 在后台等待图片详情 AI 完整输出并入库。
// ctx 为请求上下文，task 为任务记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) savePendingAIVisionCandidateAsync(ctx context.Context, task localdb.Task, candidate map[string]any, options StartOptions) bool {
	raw := candidate[pendingAIVisionDecisionKey]
	resultCh, ok := raw.(<-chan pendingAIDecisionResult)
	if !ok || resultCh == nil {
		return false
	}
	delete(candidate, pendingAIVisionDecisionKey)
	payload := cloneCandidateForCloud(task, candidate)
	name := candidateLogName(candidate)
	r.taskLog(task.ID, "info", "AI 完整详情输出将后台同步简历："+name)
	go func() {
		timer := time.NewTimer(pendingAIVisionOutputTimeout)
		defer timer.Stop()
		select {
		case result := <-resultCh:
			if result.Err != nil {
				r.taskLog(task.ID, "warning", "AI 完整详情输出失败："+result.Err.Error())
				return
			}
			mergeVisionDecisionIntoCandidate(payload, result.Decision)
			r.taskLog(task.ID, "info", "AI 完整详情输出已合并："+name)
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			defer cancel()
			r.saveCandidatePayload(saveCtx, task, payload, options)
		case <-ctx.Done():
			r.taskLog(task.ID, "warning", "等待 AI 完整详情输出被中断："+ctx.Err().Error())
		case <-timer.C:
			r.taskLog(task.ID, "warning", fmt.Sprintf("AI图片详情：超时，候选人=%s，超过=%s", name, pendingAIVisionOutputTimeout.Round(time.Second)))
		}
	}()
	return true
}

// saveCandidatePayload 将候选人入库 payload 同步到云端。
// ctx 为请求上下文，task 为任务记录，payload 为候选人 JSON，options 为启动参数。
func (r *Runner) saveCandidatePayload(ctx context.Context, task localdb.Task, payload map[string]any, options StartOptions) {
	name := candidateLogName(payload)
	r.taskLog(task.ID, "info", "结果保存：准备同步云端，候选人="+name)
	err := r.withOperationTimeout(ctx, task.ID, name, "同步候选人到云端", cloudCandidateSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).SaveTaskCandidate(syncCtx, options.Token, task.ID, payload)
	})
	if err != nil {
		r.taskLog(task.ID, "warning", fmt.Sprintf("结果保存：云端同步失败，候选人=%s，错误=%s", name, err.Error()))
		return
	}
	r.taskLog(task.ID, "info", "结果保存：云端同步完成，候选人="+name)
}

// mergeVisionDecisionIntoCandidate 合并图片详情 AI 的最终输出。
// candidate 为候选人结果，decision 为完整 AI 决策。
func mergeVisionDecisionIntoCandidate(candidate map[string]any, decision localai.Decision) {
	if text := strings.TrimSpace(decision.DetailText); text != "" {
		candidate["ai_vision_text"] = text
	}
	if len(decision.Usage) > 0 {
		candidate["ai_usage"] = decision.Usage
	}
	if decision.ElapsedMS > 0 {
		candidate["ai_elapsed_ms"] = decision.ElapsedMS
	}
	if decision.ResumeData != nil && len(decision.ResumeData) > 0 {
		for key, value := range decision.ResumeData {
			if isAIScoreField(key) {
				continue
			}
			if value != nil {
				candidate[key] = value
			}
		}
	}
}

// isAIScoreField 判断字段是否为两次 AI 分析的真实结果字段。
// key 为候选人字段名，返回 true 表示不允许结构化简历覆盖。
func isAIScoreField(key string) bool {
	switch key {
	case "ai_detail_score", "ai_detail_reason", "ai_greet_score", "ai_greet_reason":
		return true
	default:
		return false
	}
}

// buildTaskRuntimeSnapshot 在任务启动时集中读取云端运行配置。
// ctx 为请求上下文，client 为云端 API 客户端，task 为任务快照，options 为启动参数，totalRounds 为进度显示总轮次。
func (r *Runner) buildTaskRuntimeSnapshot(ctx context.Context, client *cloudapi.Client, task localdb.Task, options StartOptions, totalRounds int) (TaskRuntimeSnapshot, error) {
	taskID := task.ID
	if client == nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端客户端未初始化")
	}
	requiresAI := taskRequiresAI(task)
	r.taskLog(taskID, "info", "任务启动：正在校验会员状态")
	subscription, err := client.FetchSubscription(ctx, options.Token)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("会员校验失败：%w", err)
	}
	if !boolFromMap(subscription, "active") {
		if requiresAI {
			return TaskRuntimeSnapshot{}, fmt.Errorf("会员已到期，当前任务使用了 AI 筛选或 AI 详情识别，请先订阅后再开始任务")
		}
		r.taskLog(taskID, "info", "任务启动：当前为免费版，任务未使用会员功能，允许启动")
	} else {
		r.taskLog(taskID, "info", fmt.Sprintf("任务启动：会员校验通过，类型=%s，到期=%s", stringFromMap(subscription, "member_type"), stringFromMap(subscription, "expires_at")))
	}
	if strings.TrimSpace(task.PositionID) != "" && len(task.PositionSnapshot) == 0 {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端岗位模板为空，任务无法启动")
	}

	r.updateProgress(taskID, Progress{Stage: "preferences", Message: "正在读取云端个人配置", TotalRounds: totalRounds})
	r.taskLog(taskID, "info", "任务启动：正在读取个人偏好配置")
	preferences, err := client.FetchUserPreferences(ctx, options.Token)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端个人配置失败：%w", err)
	}
	options = applyCloudPreferences(options, preferences)
	r.taskLog(taskID, "info", "任务启动：个人偏好配置读取完成")

	if requiresAI {
		r.updateProgress(taskID, Progress{Stage: "ai_config", Message: "正在读取云端 AI 配置", TotalRounds: totalRounds})
		r.taskLog(taskID, "info", "任务启动：正在读取 AI 配置")
		aiConfig, err := client.FetchEffectiveAIConfig(ctx, options.Token)
		if err != nil {
			return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端 AI 配置失败：%w", err)
		}
		options.AIConfig = aiConfigFromCloud(aiConfig)
		if err := validateAIConfig(options.AIConfig); err != nil {
			return TaskRuntimeSnapshot{}, err
		}
		r.taskLog(taskID, "info", fmt.Sprintf("任务启动：AI 配置读取完成，模型=%s", options.AIConfig.Model))
	}

	r.updateProgress(taskID, Progress{Stage: "platform_config", Message: "正在读取平台配置", TotalRounds: totalRounds})
	platformID := strings.ToLower(strings.TrimSpace(task.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	r.taskLog(taskID, "info", "任务启动：正在读取平台配置，平台="+platformID)
	platformConfig, err := client.FetchPlatformConfig(ctx, platformID)
	if err != nil {
		return TaskRuntimeSnapshot{}, fmt.Errorf("读取云端平台配置失败：%w", err)
	}
	if len(platformConfig) == 0 {
		return TaskRuntimeSnapshot{}, fmt.Errorf("云端平台配置为空，任务无法启动")
	}
	r.taskLog(taskID, "info", "任务启动：平台配置读取完成，平台="+platformID)

	return TaskRuntimeSnapshot{
		Task:           task,
		Options:        options,
		PlatformConfig: platformConfig,
		Preferences:    preferences,
		AIConfig:       options.AIConfig,
	}, nil
}

// localTaskSnapshotFromCloud 将云端任务转换为本地运行快照。
// task 为云端任务响应对象，返回可写入本地轻量任务表的字段。
func localTaskSnapshotFromCloud(task map[string]any) map[string]any {
	position := mapValue(task["position"])
	if len(position) == 0 {
		position = mapValue(task["position_snapshot"])
	}
	return map[string]any{
		"id":                  stringFromMap(task, "id"),
		"name":                stringFromMap(task, "name"),
		"platform_id":         stringFromMap(task, "platform_id"),
		"platform_account_id": stringFromMap(task, "platform_account_id"),
		"position_id":         stringFromMap(task, "position_id"),
		"mode":                stringFromMap(task, "mode"),
		"match_limit":         intFromMap(task, "match_limit"),
		"enable_sound":        boolFromMap(task, "enable_sound"),
		"enable_thinking":     boolFromMap(task, "enable_thinking"),
		"position_snapshot":   position,
	}
}

// localTaskStatusMap 将本地任务记录转换为状态接口返回 map。
// task 为本地任务记录。
func localTaskStatusMap(task localdb.Task) map[string]any {
	return map[string]any{
		"id":                  task.ID,
		"name":                task.Name,
		"platform_id":         task.PlatformID,
		"platform_account_id": task.PlatformAccountID,
		"position_id":         task.PositionID,
		"mode":                task.Mode,
		"match_limit":         task.MatchLimit,
		"status":              task.Status,
		"scanned_count":       task.ScannedCount,
		"greeted_count":       task.GreetedCount,
		"skipped_count":       task.SkippedCount,
		"failed_count":        task.FailedCount,
		"enable_sound":        task.EnableSound,
		"enable_thinking":     task.EnableThinking,
		"position_snapshot":   task.PositionSnapshot,
		"created_at":          task.CreatedAt,
		"updated_at":          task.UpdatedAt,
	}
}

// applyCloudPreferences 使用云端个人配置覆盖任务启动参数。
// options 为当前启动参数，preferences 为云端 /api/config/user-preferences 返回的配置。
func applyCloudPreferences(options StartOptions, preferences map[string]any) StartOptions {
	if len(preferences) == 0 {
		return options
	}
	options.ScrollDelayMin = intFromMapOr(preferences, "scroll_delay_min", options.ScrollDelayMin)
	options.ScrollDelayMax = intFromMapOr(preferences, "scroll_delay_max", options.ScrollDelayMax)
	options.ListViewDelayMin = floatFromMapOr(preferences, "list_view_delay_min", options.ListViewDelayMin)
	options.ListViewDelayMax = floatFromMapOr(preferences, "list_view_delay_max", options.ListViewDelayMax)
	options.DetailViewDelayMin = floatFromMapOr(preferences, "detail_view_delay_min", options.DetailViewDelayMin)
	options.DetailViewDelayMax = floatFromMapOr(preferences, "detail_view_delay_max", options.DetailViewDelayMax)
	if _, ok := preferences["detail_open_probability"]; ok {
		options.DetailOpenProbability = intFromMapOr(preferences, "detail_open_probability", options.DetailOpenProbability)
		options.detailOpenProbabilitySet = true
	}
	options.DetailOpenDelayMin = floatFromMapOr(preferences, "detail_open_delay_min", options.DetailOpenDelayMin)
	options.DetailOpenDelayMax = floatFromMapOr(preferences, "detail_open_delay_max", options.DetailOpenDelayMax)
	options.DetailCloseDelayMin = floatFromMapOr(preferences, "detail_close_delay_min", options.DetailCloseDelayMin)
	options.DetailCloseDelayMax = floatFromMapOr(preferences, "detail_close_delay_max", options.DetailCloseDelayMax)
	options.GreetBeforeDelayMin = floatFromMapOr(preferences, "greet_before_delay_min", options.GreetBeforeDelayMin)
	options.GreetBeforeDelayMax = floatFromMapOr(preferences, "greet_before_delay_max", options.GreetBeforeDelayMax)
	options.RestAfterCandidatesMin = intFromMapOr(preferences, "rest_after_candidates_min", options.RestAfterCandidatesMin)
	options.RestAfterCandidatesMax = intFromMapOr(preferences, "rest_after_candidates_max", options.RestAfterCandidatesMax)
	options.RestTimesMin = intFromMapOr(preferences, "rest_times_min", options.RestTimesMin)
	options.RestTimesMax = intFromMapOr(preferences, "rest_times_max", options.RestTimesMax)
	options.RestDurationMin = floatFromMapOr(preferences, "rest_duration_min", options.RestDurationMin)
	options.RestDurationMax = floatFromMapOr(preferences, "rest_duration_max", options.RestDurationMax)
	return options
}

// aiConfigFromCloud 将云端 AI 配置转换为本地 AI 客户端配置。
// config 为云端 /api/config/effective-ai 返回的配置。
func aiConfigFromCloud(config map[string]any) localdb.AIConfig {
	return localdb.AIConfig{
		ID:          "cloud",
		BaseURL:     stringFromMap(config, "base_url"),
		APIKey:      stringFromMap(config, "api_key"),
		Model:       stringFromMap(config, "model"),
		Temperature: floatFromMapOr(config, "temperature", 0.2),
		Timeout:     intFromMapOr(config, "timeout", 120),
		Extra:       mapValue(config["extra"]),
	}
}

// validateAIConfig 校验任务运行需要的 AI 配置是否完整。
// config 为云端下发的 AI 配置。
func validateAIConfig(config localdb.AIConfig) error {
	if strings.TrimSpace(config.BaseURL) == "" {
		return fmt.Errorf("请先在个人配置里填写云端 AI 接口地址")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("请先在个人配置里填写云端 AI Key")
	}
	if strings.TrimSpace(config.Model) == "" {
		return fmt.Errorf("请先在个人配置里填写 AI 模型")
	}
	return nil
}

// taskRequiresAI 判断任务是否需要 AI 配置。
// task 为本地运行任务，AI 筛选或 AI 详情识别时返回 true。
func taskRequiresAI(task localdb.Task) bool {
	return taskMode(task) == "ai" || detailMode(task) == "ai"
}

// taskProfileName 返回任务对应的本机浏览器目录名。
// task 为本地任务；开始任务统一复用默认浏览器目录，不再按平台或账号拆分目录。
func taskProfileName(_ localdb.Task) string {
	return "default"
}
