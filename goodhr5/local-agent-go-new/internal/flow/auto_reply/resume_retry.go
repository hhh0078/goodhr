// Package auto_reply 本文件恢复失败的简历步骤；复用会话待办与原 AI 循环，已回答的消息绝不再次发送。
package auto_reply

import (
	"context"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// autoReplyResumeRetryDue 判断服务器简历待办是否到期；三次自动尝试耗尽后保留错误供人工检查。
func autoReplyResumeRetryDue(retry *cloud.AutoReplyResumeRetry, now time.Time) bool {
	return retry != nil && retry.Attempts < 3 && !retry.NextAttemptAt.After(now)
}

// beginResumeRetry 在浏览器采集和 AI 付费调用之前记录次数及退避时间，进程中断也不会每轮立即重试。
func (f *Flow) beginResumeRetry(ctx context.Context, prepared shared.PreparedTask, conversation *cloud.AutoReplyConversation, sourceID string) (bool, error) {
	saved, claimed, err := f.Cloud.ClaimAutoReplyResumeRetry(ctx, credentials(prepared), conversation.ID, sourceID)
	if err != nil {
		return false, fmt.Errorf("保存简历待办失败，尚未继续采集：%w", err)
	}
	*conversation = saved
	return claimed, nil
}

// recordResumeRetryFailure 保存真实原因并显示下一步，不把可延期的简历错误计作问答失败。
func (f *Flow) recordResumeRetryFailure(ctx context.Context, prepared shared.PreparedTask, conversation *cloud.AutoReplyConversation, snapshot model.AutoReplyConversationSnapshot, cause error) error {
	if conversation.ResumeRetry == nil {
		return fmt.Errorf("简历失败记录缺少持久待办：%w", cause)
	}
	retry := *conversation.ResumeRetry
	retry.LastError = cause.Error()
	conversation.ResumeRetry = &retry
	err := f.Cloud.FinishAutoReplyResumeRetry(ctx, credentials(prepared), conversation.ID, retry.SourceMessageID, false, cause.Error())
	if err != nil {
		return fmt.Errorf("保存简历失败原因失败：%w", err)
	}
	f.log(prepared.Request.TaskID, "resume_deferred", "warning", time.Now(), cause)
	reason := "简历暂时没读好，已留待办稍后重试；有依据的岗位问题继续回答"
	if retry.Attempts >= 3 {
		reason = "简历已尝试三次，请先检查附件；岗位问答继续，推荐暂不生成"
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		PositionID: conversation.PositionID, Kind: "auto_reply", Phase: "result", Stage: "resume_deferred",
		CandidateName: snapshot.CandidateName, Reason: reason, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	return nil
}

// reviewRecoveredResume 在新简历证据完成后维护目标；不补答历史，必要时登记并送达一次未问目标。
func (f *Flow) reviewRecoveredResume(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, input ReplyContext) error {
	if input.CandidateState.Candidate == nil || !input.CandidateState.Candidate.ResumeStructured {
		return f.recordResumeRetryFailure(ctx, prepared, &input.Conversation, input.PageSnapshot, fmt.Errorf("简历还没有成功结构化，暂不复核推荐条件"))
	}
	if err := f.verifyCurrentReplyConfiguration(ctx, prepared, input.Position); err != nil {
		return err
	}
	before, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, input.PageSnapshot)
	if err != nil {
		return err
	}
	beforeKey := firstNonEmpty(before.PlatformMessageID, before.Key, messageFingerprint(before, 0))
	if before.Direction == "candidate" && beforeKey != input.BasedOnMessageKey {
		return nil
	}
	decision, err := f.Responder.Reply(ctx, input)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	validFollowup := input.AllowGoalFollowup && len(decision.Messages) == 1 && decision.Messages[0].Type == "confirmation" && decision.Messages[0].ConfirmationItemID != ""
	if err == nil && (len(decision.Messages) != 0 && !validFollowup || decision.ManualReason != "" || len(decision.Suggestions) != 0) {
		err = fmt.Errorf("本轮仅补审条件，模型没有返回有效的内部复核结果")
	}
	if err != nil {
		return f.recordResumeRetryFailure(ctx, prepared, &input.Conversation, input.PageSnapshot, err)
	}
	after, err := runtime.ReadLatestAutoReplyMessage(ctx, f.Browser, prepared.Platform, input.PageSnapshot)
	if err != nil {
		return err
	}
	if after.Direction != before.Direction || firstNonEmpty(after.PlatformMessageID, after.Key, messageFingerprint(after, 0)) != beforeKey {
		return nil
	}
	if err = f.verifyCurrentReplyConfiguration(ctx, prepared, input.Position); err != nil {
		return err
	}
	var followup []cloud.AutoReplyGoalFollowup
	if validFollowup {
		followup = append(followup, cloud.AutoReplyGoalFollowup{EventKind: "resume", ContextRevision: cloud.AutoReplyGoalContextRevision(input.Position), ConfirmationItemID: decision.Messages[0].ConfirmationItemID, Content: common.NormalizeAutoReplyOutgoingText(prepared.Platform, decision.Messages[0].Content), ExpectedContentKey: decision.Messages[0].GoalContentKey})
	}
	if input.Conversation.ResumeRetry == nil {
		return fmt.Errorf("新简历复核缺少已领取的证据事件")
	}
	if err = f.Cloud.CommitAutoReplyResumeProposal(ctx, input.Credentials, input.Conversation.ID, input.BasedOnMessageKey, input.Conversation.ResumeRetry.SourceMessageID, cloud.AutoReplyResumeRevision(*input.CandidateState.Candidate), input.Position.Config.Version, decision.ConfirmationUpdates, followup...); err != nil {
		return err
	}
	// 新目标追问和补审结果同时落账，发送前再核对原页；重启也能恢复这一次事件。
	return f.deliverGoalFollowup(ctx, prepared, runtime, input.Position, input.Conversation, input.PageSnapshot)
}
