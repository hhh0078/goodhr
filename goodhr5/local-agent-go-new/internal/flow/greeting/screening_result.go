// Package greeting 文件作用：仅用真实终态筛选结果记录教程完成事实，不上传候选人资料。
package greeting

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// recordScreeningResult 后台同步一次真实筛选终态；不等待网络，失败仅记日志并让后续真实结果重试。
func (f *Flow) recordScreeningResult(ctx context.Context, prepared shared.PreparedTask, stats *shared.Stats, result shared.AnalysisStatus) {
	if stats == nil || f.Cloud == nil {
		return
	}
	if result.Phase != "result" || !result.Terminal || (result.Kind != "ai" && result.Kind != "keyword") {
		return
	}
	if stats.ScreeningResultSync == nil {
		stats.ScreeningResultSync = &shared.ScreeningResultSyncState{}
	}
	state := stats.ScreeningResultSync
	if !state.TryStart() {
		return
	}
	evidence := cloud.ScreeningResultEvidence{Kind: result.Kind, Phase: result.Phase, Terminal: result.Terminal}
	client, logger := f.Cloud, f.Logger
	token, positionID, taskID := prepared.Request.Token, prepared.Position.ID, prepared.Request.TaskID
	baseCtx := context.WithoutCancel(ctx)
	go func() {
		success := false
		defer func() {
			state.Finish(success)
			if recovered := recover(); recovered != nil && logger != nil {
				logger.Step(taskID, "greeting", "sync_screening_result", "warning", time.Now(), fmt.Errorf("教程结果同步没有完成：%v", recovered))
			}
		}()
		syncCtx, cancel := context.WithTimeout(baseCtx, 3*time.Second)
		defer cancel()
		if err := client.ReportScreeningResult(syncCtx, token, positionID, evidence); err != nil {
			if logger != nil {
				logger.Step(taskID, "greeting", "sync_screening_result", "warning", time.Now(), err)
			}
			return
		}
		success = true
	}()
}
