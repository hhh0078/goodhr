// Package auto_reply 本文件从业务事务收据恢复推荐资格检查，覆盖提交后尚未入队就崩溃的空隙。
package auto_reply

import (
	"context"
	"errors"
	"net/http"
	"time"

	"goodhr5/local-agent-go-new/internal/integration/cloud"
)

// recoverCommittedRecommendations 只处理当前账号已提交的收据，陈旧消息或岗位版本直接结束，不恢复旧轮副作用。
func (r *AIResponder) recoverCommittedRecommendations(ctx context.Context, credentials cloud.AgentCredentials) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	receipts, err := r.Cloud.PendingAutoReplyProposals(ctx, credentials)
	if err != nil {
		return err
	}
	var firstErr error
	for _, receipt := range receipts {
		if err = ctx.Err(); err != nil {
			return err
		}
		itemCtx, itemCancel := context.WithTimeout(ctx, 5*time.Second)
		err = r.recoverCommittedRecommendation(itemCtx, credentials, receipt)
		itemCancel()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			failureErr := r.Cloud.FailAutoReplyProposal(finishCtx, credentials, receipt.ID, "推荐资格暂未读完整，稍后重试")
			finishCancel()
			if failureErr != nil {
				return failureErr
			}
		}
	}
	return firstErr
}

// recoverCommittedRecommendation 独立恢复一份收据，已删除岗位结束收据，临时失败只保留本项重试。
func (r *AIResponder) recoverCommittedRecommendation(ctx context.Context, credentials cloud.AgentCredentials, receipt cloud.AutoReplyProposalReceipt) error {
	snapshot, readErr := r.Cloud.AutoReplySnapshot(ctx, credentials, receipt.Conversation.PositionID)
	if readErr != nil {
		var apiErr *cloud.APIError
		if errors.As(readErr, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return r.Cloud.CompleteAutoReplyProposal(ctx, credentials, receipt.ID)
		}
		return readErr
	}
	if snapshot.Config.Enabled /* 会员限制暂停：&& snapshot.Subscription.Active && snapshot.Subscription.AllowAutoReply */ && snapshot.Config.Version == receipt.ConfigVersion {
		messages, readErr := r.Cloud.AutoReplyMessages(ctx, credentials, receipt.ConversationID)
		if readErr != nil {
			return readErr
		}
		if latestRecommendationCandidateKey(messages) == receipt.BasedOnMessageKey {
			state, readErr := r.Cloud.AutoReplyCandidateState(ctx, credentials, cloud.AutoReplyCandidateLookup{PlatformID: receipt.Conversation.PlatformID, PlatformAccountID: receipt.Conversation.PlatformAccountID, PlatformThreadID: receipt.Conversation.PlatformThreadID})
			if readErr != nil {
				return readErr
			}
			if state.Candidate != nil && state.Candidate.ID == receipt.Conversation.CandidateID {
				if err := r.QueueRecommendation(ctx, ReplyContext{Credentials: credentials, Position: snapshot, Conversation: receipt.Conversation, CandidateState: state, Messages: messages, BasedOnMessageKey: receipt.BasedOnMessageKey}); err != nil {
					return err
				}
			}
		}
	}
	// 入队按来源幂等；这里崩溃留下收据也只会再次检查，不会重复生成。
	return r.Cloud.CompleteAutoReplyProposal(ctx, credentials, receipt.ID)
}

// latestRecommendationCandidateKey 统一比较最新已采集候选人消息，避免恢复或发布时忽略更新的入站批次。
func latestRecommendationCandidateKey(messages []cloud.AutoReplyMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Direction == "candidate" {
			return firstNonEmpty(messages[index].PlatformMessageID, messages[index].Fingerprint, messages[index].ID)
		}
	}
	return ""
}
