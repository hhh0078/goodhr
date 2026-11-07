// Package lifecycle 本文件将设备登录后的简历删除通知分派给已运行的本地数据管理器。
package lifecycle

import (
	"context"
	"errors"

	downloadflow "goodhr5/local-agent-go-new/internal/flow/resume_download"
	importflow "goodhr5/local-agent-go-new/internal/flow/resume_import"
	summaryflow "goodhr5/local-agent-go-new/internal/flow/resume_summary"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// ConfigureCandidatePurge 在启动后台循环前组装清理步骤，数据库、草稿管理器或文件出错均保留云端事件供重试。
func (r *Runner) ConfigureCandidatePurge(imports *importflow.Manager, summaries *summaryflow.Manager, downloads *downloadflow.Manager) {
	if r == nil || r.autoReplyBackground == nil {
		return
	}
	r.autoReplyBackground.SetCandidatePurge(func(ctx context.Context, owner string, credentials cloud.AgentCredentials) error {
		return r.cloud.SyncCandidateDeletions(ctx, credentials, func(ctx context.Context, event cloud.CandidateDeletion) error {
			replyErr := r.cloud.PurgeLocalAutoReplyCandidate(ctx, credentials, event.CandidateID, event.SourceHashes)
			screenErr := r.store.PurgeCandidateScreenings(ctx, owner, event.SourceHashes)
			summaryErr := summaries.PurgeCandidate(ctx, owner, event.CandidateID)
			importErr := imports.PurgeCandidate(ctx, owner, event.CandidateID, event.RequestHashes, func(ctx context.Context, owner, task string) error {
				return downloads.PurgeImportTask(ctx, owner, task, r.store.HidePurgedDownload)
			})
			return errors.Join(replyErr, screenErr, summaryErr, importErr)
		})
	})
}
