// Package greeting 文件作用：在主流程提前使用 AI 评分后，后台等待完整结果并同步结构化简历。
package greeting

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

const candidateCloudSyncTimeout = 60 * time.Second

// finishCandidateEvaluationAsync 后台等待 AI 完整响应，并按岗位开关同步结构化简历。
func (f *Flow) finishCandidateEvaluationAsync(
	prepared shared.PreparedTask,
	candidate model.Candidate,
	status string,
	evaluation ai.Evaluation,
	cancel context.CancelFunc,
) {
	candidate = cloneCandidate(candidate)
	go func() {
		if cancel != nil {
			defer cancel()
		}
		result := ai.EvaluationResult{Decision: evaluation.Decision}
		if evaluation.Final != nil {
			result = <-evaluation.Final
		}
		if result.Err != nil {
			f.log(
				prepared.Request.TaskID,
				"complete_ai_output",
				"warning",
				time.Now(),
				fmt.Errorf("AI 完整输出没有接收完：%w", result.Err),
			)
			return
		}
		if !prepared.Position.CommonConfig.OutputStructuredResume {
			return
		}
		if f.Cloud == nil {
			f.log(
				prepared.Request.TaskID,
				"sync_structured_candidate",
				"warning",
				time.Now(),
				fmt.Errorf("云端客户端还没准备好，结构化简历这次没有同步"),
			)
			return
		}
		upload := candidateUpload(prepared, candidate, status, result.Decision)
		syncCtx, syncCancel := context.WithTimeout(context.Background(), candidateCloudSyncTimeout)
		defer syncCancel()
		startedAt := time.Now()
		f.log(prepared.Request.TaskID, "sync_structured_candidate", "start", startedAt, nil)
		if err := f.Cloud.SavePositionCandidate(
			syncCtx,
			prepared.Request.Token,
			prepared.Position.ID,
			upload,
		); err != nil {
			f.log(prepared.Request.TaskID, "sync_structured_candidate", "warning", startedAt, err)
			return
		}
		f.log(prepared.Request.TaskID, "sync_structured_candidate", "success", startedAt, nil)
	}()
}

// candidateUpload 合并页面基础摘要、AI 最终评分和可选结构化简历。
func candidateUpload(
	prepared shared.PreparedTask,
	candidate model.Candidate,
	status string,
	decision ai.Decision,
) cloud.CandidateUpload {
	structured := cloud.StructuredCandidate{}
	if decision.Resume != nil {
		structured = *decision.Resume
	}
	if strings.TrimSpace(structured.CandidateName) == "" {
		structured.CandidateName = strings.TrimSpace(candidate.Name)
	}
	if strings.TrimSpace(structured.RawText) == "" {
		structured.RawText = strings.TrimSpace(candidate.Summary)
	}
	score := decision.Score
	return cloud.CandidateUpload{
		StructuredCandidate: structured,
		PlatformID:          prepared.Position.PlatformID,
		PlatformCandidateID: strings.TrimSpace(candidate.Fields["platform_candidate_id"]),
		BasicInfo:           strings.TrimSpace(candidate.Summary),
		Status:              strings.TrimSpace(status),
		AIGreetReason:       decision.Reason,
		AIGreetScore:        &score,
	}
}

// cloneCandidate 复制候选人及其字段，避免后台同步读取到后续页面修改。
func cloneCandidate(candidate model.Candidate) model.Candidate {
	fields := make(map[string]string, len(candidate.Fields))
	for key, value := range candidate.Fields {
		fields[key] = value
	}
	candidate.Fields = fields
	candidate.IdentityTexts = append([]string(nil), candidate.IdentityTexts...)
	return candidate
}
