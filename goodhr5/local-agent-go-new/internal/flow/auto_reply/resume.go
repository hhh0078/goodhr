// Package auto_reply 本文件负责自动回复会话的简历索要、收集、正式入库和附件上传门槛。
package auto_reply

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ensureResume 在进入 AI 回复前确保已经索要简历，或收集并保存页面已有简历。
// handled 为 true 表示本轮已经发送索要简历话术，不应继续生成第二条回复。
func (f *Flow) ensureResume(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, position cloud.AutoReplyPositionSnapshot, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, basedOnMessageKey string, stats *shared.Stats) (*model.AutoReplyResumeBundle, bool, error) {
	if state.HasResumeAttachment {
		return nil, false, nil
	}
	if !snapshot.ResumeCardAvailable {
		requestMessage := firstNonEmpty(position.Config.ResumeRequestMessage, "你好，能发一份简历吗？")
		unchanged, err := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, snapshot, basedOnMessageKey)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if !unchanged {
			stats.Skipped++
			f.reportChangedMessage(prepared.Request.TaskID, snapshot.CandidateName)
			return nil, true, nil
		}
		duplicate, err := f.replyAlreadyRecorded(ctx, prepared, *conversation, basedOnMessageKey, requestMessage)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if duplicate {
			stats.Skipped++
			return nil, true, nil
		}
		if err = runtime.RequestAutoReplyResume(ctx, f.Browser, prepared.Platform, snapshot); err != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("索要候选人简历失败：%w", err)
		}
		sent, err := f.sendVerifiedMessage(ctx, prepared, runtime, *conversation, snapshot, basedOnMessageKey, requestMessage, false)
		if err != nil {
			stats.Failed++
			return nil, false, err
		}
		if sent {
			stats.Succeeded++
		} else {
			stats.Skipped++
		}
		shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
			Kind: "auto_reply", Phase: "result", Stage: "resume_requested", Terminal: true,
			CandidateName: snapshot.CandidateName, Accepted: boolPointer(sent),
			Reason: "还缺一份简历，我已经礼貌问候了", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return nil, true, nil
	}

	bundle, err := runtime.CollectAutoReplyResume(ctx, f.Browser, prepared.Platform, snapshot)
	if err != nil {
		stats.Failed++
		return nil, false, fmt.Errorf("读取候选人简历失败：%w", err)
	}
	if strings.TrimSpace(bundle.OnlineResumeText) == "" && len(bundle.AttachmentPaths) == 0 {
		stats.Failed++
		return nil, false, fmt.Errorf("候选人简历卡片已经出现，但没有读到在线简历或附件")
	}
	candidateID := ""
	if state.Candidate != nil {
		candidateID = state.Candidate.ID
	}
	if candidateID == "" && strings.TrimSpace(bundle.Phone) != "" {
		result, saveErr := f.Cloud.SaveAutoReplyCandidate(ctx, credentials(prepared), cloud.AutoReplyCandidateInput{
			StructuredCandidate: cloud.StructuredCandidate{
				CandidateName: firstNonEmpty(bundle.CandidateName, snapshot.CandidateName),
				BirthYM:       bundle.BirthYM, Phone: bundle.Phone, Email: bundle.Email,
				RawText: bundle.OnlineResumeText,
			},
			PositionID: position.Position.ID, PlatformID: prepared.Platform.ID,
			PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID,
			Gender: firstNonEmpty(bundle.Gender, snapshot.Gender), BirthYMPrecision: bundle.BirthYMPrecision,
			BasicInfo: bundle.OnlineResumeText,
		})
		if saveErr != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("保存候选人正式简历失败：%w", saveErr)
		}
		candidateID = result.CandidateID
		conversation.CandidateID = candidateID
		if result.PlatformIdentity.ID != "" {
			conversation.PlatformIdentityID = result.PlatformIdentity.ID
		}
		saved, saveErr := f.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), *conversation)
		if saveErr != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("关联正式简历和候选人会话失败：%w", saveErr)
		}
		*conversation = saved
	}
	for _, path := range bundle.AttachmentPaths {
		if _, err = f.Cloud.UploadAutoReplyAttachment(ctx, credentials(prepared), cloud.AutoReplyAttachmentUpload{
			FilePath: path, CandidateID: candidateID, ConversationID: conversation.ID,
			SourceMessageID: firstNonEmpty(bundle.ResumeSourceMessageID, snapshot.ResumeSourceMessageID),
			PlatformID:      prepared.Platform.ID, ExtractedText: bundle.OnlineResumeText,
		}); err != nil {
			stats.Failed++
			return nil, false, fmt.Errorf("上传候选人简历附件失败：%w", err)
		}
	}
	return &bundle, false, nil
}
