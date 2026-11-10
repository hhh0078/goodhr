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
			hashes, err := r.cloud.CandidateDeletionDocumentHashes(ctx, credentials, event)
			if err != nil {
				return err
			}
			replyErr := r.cloud.ApplyAutoReplyCandidateDeletion(ctx, credentials, event)
			screenErr := r.store.PurgeCandidateScreenings(ctx, owner, event.SourceHashes)
			summaryErr := summaries.PurgeCandidate(ctx, owner, event.CandidateID)
			purgeDownload := func(ctx context.Context, owner, task string) error {
				return downloads.PurgeImportTask(ctx, owner, task, r.store.HidePurgedDownload)
			}
			var importErr error
			// 旧事件只知道升级时间，不能把升级前重新导入的未入库草稿推认为旧资料。
			if !event.CutoffFromMigration {
				importErr = imports.PurgeCandidateCopies(ctx, owner, event.CandidateID, event.DeletedAt, hashes, event.RequestHashes, purgeDownload)
			}
			if importErr == nil {
				importErr = imports.PurgeCandidate(ctx, owner, event.CandidateID, event.RequestHashes, purgeDownload)
			}
			return errors.Join(replyErr, screenErr, summaryErr, importErr)
		})
	})
}
