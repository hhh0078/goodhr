// Package positionrunner 文件作用：按职责承载本地岗位运行运行流程的拆分实现。
package positionrunner

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
// ctx 为请求上下文，position 为岗位运行记录，count 为新增候选人数量，options 为岗位运行启动参数。
func (r *Runner) syncProcessedResumeCount(ctx context.Context, position localdb.Position, count int, options StartOptions) {
	if count <= 0 || strings.TrimSpace(options.Token) == "" {
		return
	}
	err := r.withOperationTimeout(ctx, position.ID, position.Name, "同步已处理简历数", cloudStatsSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).AddProcessedResumes(syncCtx, options.Token, position.ID, count)
	})
	if err != nil {
		r.positionLog(position.ID, "warning", "同步已处理简历数失败："+err.Error())
	}
}

// syncPositionCounts 将本地岗位运行累计统计同步给云端岗位运行列表。
// ctx 为请求上下文，position 为本地岗位运行记录，options 为岗位运行启动参数。
func (r *Runner) syncPositionCounts(ctx context.Context, position localdb.Position, options StartOptions) {
	if strings.TrimSpace(position.ID) == "" || strings.TrimSpace(options.Token) == "" {
		return
	}
	counts := map[string]any{
		"scanned_count": position.ScannedCount,
		"greeted_count": position.GreetedCount,
		"skipped_count": position.SkippedCount,
		"failed_count":  position.FailedCount,
	}
	r.positionLog(position.ID, "info", fmt.Sprintf("统计同步：准备同步岗位运行统计，扫描=%d，打招呼=%d，跳过=%d，失败=%d", position.ScannedCount, position.GreetedCount, position.SkippedCount, position.FailedCount))
	err := r.withOperationTimeout(ctx, position.ID, position.Name, "同步岗位运行统计", cloudStatsSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).SyncPositionCounts(syncCtx, options.Token, position.ID, counts)
	})
	if err != nil {
		r.positionLog(position.ID, "warning", "统计同步：同步失败，错误="+err.Error())
	}
}

// persistPositionCountProgress 将本次扫描尚未落库的统计增量写入本地，并把最新累计值同步到云端。
// ctx 为同步上下文，position 为本地岗位运行，current 为本次扫描当前统计，persisted 为已经落库的统计，options 为启动参数。
func (r *Runner) persistPositionCountProgress(ctx context.Context, position localdb.Position, current batchProcessResult, persisted batchProcessResult, options StartOptions) (batchProcessResult, error) {
	delta := current.deltaFrom(persisted)
	if delta.empty() {
		if current.empty() {
			return persisted, nil
		}
		latestPosition, err := r.db.GetPosition(position.ID)
		if err != nil {
			return persisted, err
		}
		r.syncPositionCounts(ctx, latestPosition, options)
		return persisted, nil
	}
	updatedPosition, err := r.db.IncrementPositionCounts(position.ID, delta.Scanned, delta.Greeted, delta.Skipped, delta.Failed)
	if err != nil {
		return persisted, err
	}
	r.syncPositionCounts(ctx, updatedPosition, options)
	return current, nil
}

// saveCandidateResult 将候选人结果同步到云端简历库。
// ctx 为请求上下文，position 为岗位运行记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) saveCandidateResult(ctx context.Context, position localdb.Position, candidate map[string]any, options StartOptions) {
	if strings.TrimSpace(options.Token) == "" {
		r.positionLog(position.ID, "warning", "结果保存：云端同步失败，错误=缺少登录 token")
		return
	}
	if r.savePendingAIVisionCandidateAsync(ctx, position, candidate, options) {
		return
	}
	payload := cloneCandidateForCloud(position, candidate)
	r.saveCandidatePayload(ctx, position, payload, options)
}

// cloneCandidateForCloud 生成候选人云端入库 JSON。
// position 为岗位运行记录，candidate 为本地候选人结果。
func cloneCandidateForCloud(position localdb.Position, candidate map[string]any) map[string]any {
	payload := make(map[string]any, len(candidate)+5)
	for key, value := range candidate {
		if strings.HasPrefix(key, "_pending_") {
			continue
		}
		payload[key] = value
	}
	payload["position_id"] = position.ID
	payload["platform_id"] = position.PlatformID
	payload["platform_account_id"] = position.PlatformAccountID
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
// ctx 为请求上下文，position 为岗位运行记录，candidate 为候选人结果，options 为启动参数。
func (r *Runner) savePendingAIVisionCandidateAsync(ctx context.Context, position localdb.Position, candidate map[string]any, options StartOptions) bool {
	raw := candidate[pendingAIVisionDecisionKey]
	resultCh, ok := raw.(<-chan pendingAIDecisionResult)
	if !ok || resultCh == nil {
		return false
	}
	delete(candidate, pendingAIVisionDecisionKey)
	payload := cloneCandidateForCloud(position, candidate)
	name := candidateLogName(candidate)
	r.positionLog(position.ID, "info", "AI 完整详情输出将后台同步简历："+name)
	go func() {
		timer := time.NewTimer(pendingAIVisionOutputTimeout)
		defer timer.Stop()
		select {
		case result := <-resultCh:
			if result.Err != nil {
				r.positionLog(position.ID, "warning", "AI 完整详情输出失败："+result.Err.Error())
				return
			}
			mergeVisionDecisionIntoCandidate(payload, result.Decision)
			r.positionLog(position.ID, "info", "AI 完整详情输出已合并："+name)
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			defer cancel()
			r.saveCandidatePayload(saveCtx, position, payload, options)
		case <-ctx.Done():
			r.positionLog(position.ID, "warning", "等待 AI 完整详情输出被中断："+ctx.Err().Error())
		case <-timer.C:
			r.positionLog(position.ID, "warning", fmt.Sprintf("AI图片详情：超时，候选人=%s，超过=%s", name, pendingAIVisionOutputTimeout.Round(time.Second)))
		}
	}()
	return true
}

// saveCandidatePayload 将候选人入库 payload 同步到云端。
// ctx 为请求上下文，position 为岗位运行记录，payload 为候选人 JSON，options 为启动参数。
func (r *Runner) saveCandidatePayload(ctx context.Context, position localdb.Position, payload map[string]any, options StartOptions) {
	name := candidateLogName(payload)
	r.positionLog(position.ID, "info", "结果保存：准备同步云端，候选人="+name)
	err := r.withOperationTimeout(ctx, position.ID, name, "同步候选人到云端", cloudCandidateSyncTimeout, func(syncCtx context.Context) error {
		return cloudapi.New(options.CloudAPIBase).SavePositionCandidate(syncCtx, options.Token, position.ID, payload)
	})
	if err != nil {
		r.positionLog(position.ID, "warning", fmt.Sprintf("结果保存：云端同步失败，候选人=%s，错误=%s", name, err.Error()))
		return
	}
	r.positionLog(position.ID, "info", "结果保存：云端同步完成，候选人="+name)
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

// buildPositionRuntimeSnapshot 在岗位运行启动时集中读取云端运行配置。
// ctx 为请求上下文，client 为云端 API 客户端，position 为岗位运行快照，options 为启动参数，totalRounds 为进度显示总轮次。
func (r *Runner) buildPositionRuntimeSnapshot(ctx context.Context, client *cloudapi.Client, position localdb.Position, options StartOptions, totalRounds int) (PositionRuntimeSnapshot, error) {
	positionID := position.ID
	if client == nil {
		return PositionRuntimeSnapshot{}, fmt.Errorf("云端客户端未初始化")
	}
	requiresAI := positionRequiresAI(position)
	r.positionLog(positionID, "info", "岗位运行启动：正在校验会员状态")
	subscription, err := client.FetchSubscription(ctx, options.Token)
	if err != nil {
		return PositionRuntimeSnapshot{}, fmt.Errorf("会员校验失败：%w", err)
	}
	if !boolFromMap(subscription, "active") {
		if requiresAI {
			return PositionRuntimeSnapshot{}, fmt.Errorf("会员已到期，当前岗位运行使用了 AI 筛选或 AI 详情识别，请先订阅后再开始岗位运行")
		}
		r.positionLog(positionID, "info", "岗位运行启动：当前为免费版，岗位运行未使用会员功能，允许启动")
	} else {
		r.positionLog(positionID, "info", fmt.Sprintf("岗位运行启动：会员校验通过，类型=%s，到期=%s", stringFromMap(subscription, "member_type"), stringFromMap(subscription, "expires_at")))
	}
	if len(position.PositionSnapshot) == 0 {
		return PositionRuntimeSnapshot{}, fmt.Errorf("云端岗位模板为空，岗位运行无法启动")
	}

	r.updateProgress(positionID, Progress{Stage: "preferences", Message: "正在读取云端个人配置", TotalRounds: totalRounds})
	r.positionLog(positionID, "info", "岗位运行启动：正在读取个人偏好配置")
	preferences, err := client.FetchUserPreferences(ctx, options.Token)
	if err != nil {
		return PositionRuntimeSnapshot{}, fmt.Errorf("读取云端个人配置失败：%w", err)
	}
	options = applyCloudPreferences(options, preferences)
	r.positionLog(positionID, "info", "岗位运行启动：个人偏好配置读取完成")

	if requiresAI {
		r.updateProgress(positionID, Progress{Stage: "ai_config", Message: "正在读取云端 AI 配置", TotalRounds: totalRounds})
		r.positionLog(positionID, "info", "岗位运行启动：正在读取 AI 配置")
		aiConfig, err := client.FetchEffectiveAIConfig(ctx, options.Token)
		if err != nil {
			return PositionRuntimeSnapshot{}, fmt.Errorf("读取云端 AI 配置失败：%w", err)
		}
		options.AIConfig = aiConfigFromCloud(aiConfig)
		if err := validateAIConfig(options.AIConfig); err != nil {
			return PositionRuntimeSnapshot{}, err
		}
		r.positionLog(positionID, "info", fmt.Sprintf("岗位运行启动：AI 配置读取完成，模型=%s", options.AIConfig.Model))
	}

	r.updateProgress(positionID, Progress{Stage: "platform_config", Message: "正在读取平台配置", TotalRounds: totalRounds})
	platformID := strings.ToLower(strings.TrimSpace(position.PlatformID))
	if platformID == "" {
		platformID = "boss"
	}
	r.positionLog(positionID, "info", "岗位运行启动：正在读取平台配置，平台="+platformID)
	platformConfig, err := client.FetchPlatformConfig(ctx, platformID)
	if err != nil {
		return PositionRuntimeSnapshot{}, fmt.Errorf("读取云端平台配置失败：%w", err)
	}
	if len(platformConfig) == 0 {
		return PositionRuntimeSnapshot{}, fmt.Errorf("云端平台配置为空，岗位运行无法启动")
	}
	r.positionLog(positionID, "info", "岗位运行启动：平台配置读取完成，平台="+platformID)

	return PositionRuntimeSnapshot{
		Position:       position,
		Options:        options,
		PlatformConfig: platformConfig,
		Preferences:    preferences,
		AIConfig:       options.AIConfig,
	}, nil
}

// localPositionSnapshotFromCloud 将云端岗位运行转换为本地运行快照。
// position 为云端岗位运行响应对象，返回可写入本地轻量岗位运行表的字段。
func localPositionSnapshotFromCloud(position map[string]any) map[string]any {
	snapshot := position
	commonConfig := mapValue(snapshot["common_config"])
	return map[string]any{
		"id":                stringFromMap(snapshot, "id"),
		"name":              stringFromMap(snapshot, "name"),
		"platform_id":       stringFromMap(snapshot, "platform_id"),
		"mode":              stringFromMap(commonConfig, "mode_default"),
		"match_limit":       intFromMap(snapshot, "match_limit"),
		"enable_sound":      boolFromMap(snapshot, "enable_sound"),
		"enable_thinking":   boolFromMap(snapshot, "enable_thinking"),
		"position_snapshot": snapshot,
	}
}

// localPositionStatusMap 将本地岗位运行记录转换为状态接口返回 map。
// position 为本地岗位运行记录。
func localPositionStatusMap(position localdb.Position) map[string]any {
	return map[string]any{
		"id":                  position.ID,
		"name":                position.Name,
		"platform_id":         position.PlatformID,
		"platform_account_id": position.PlatformAccountID,
		"mode":                position.Mode,
		"match_limit":         position.MatchLimit,
		"status":              position.Status,
		"scanned_count":       position.ScannedCount,
		"greeted_count":       position.GreetedCount,
		"skipped_count":       position.SkippedCount,
		"failed_count":        position.FailedCount,
		"enable_sound":        position.EnableSound,
		"enable_thinking":     position.EnableThinking,
		"position_snapshot":   position.PositionSnapshot,
		"created_at":          position.CreatedAt,
		"updated_at":          position.UpdatedAt,
	}
}

// applyCloudPreferences 使用云端个人配置覆盖岗位运行启动参数。
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

// validateAIConfig 校验岗位运行运行需要的 AI 配置是否完整。
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

// positionRequiresAI 判断岗位运行是否需要 AI 配置。
// position 为本地运行岗位运行，AI 筛选或 AI 详情识别时返回 true。
func positionRequiresAI(position localdb.Position) bool {
	return positionMode(position) == "ai" || detailMode(position) == "ai"
}

// positionProfileName 返回岗位运行对应的本机浏览器目录名。
// position 为本地岗位运行；开始岗位运行统一复用默认浏览器目录，不再按平台或账号拆分目录。
func positionProfileName(_ localdb.Position) string {
	return "default"
}
