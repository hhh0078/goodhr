// Package auto_reply 本文件负责单个会话的身份、岗位、聊天、简历和发送闭环。
package auto_reply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// ScopedFailure 仅携带已确认的岗位与会话，供外层释放当前会话后仍可准确记录故障。
type ScopedFailure struct {
	Cause                  error
	Position, Conversation string
}

// Error 保留已有业务报错文字。
func (e *ScopedFailure) Error() string { return e.Cause.Error() }

// Unwrap 保留下层错误的来源和超时证据。
func (e *ScopedFailure) Unwrap() error { return e.Cause }

// scopedFailure 只在已核验岗位并获得结构化故障时携带归属，不猜测全局扫描所属岗位。
func scopedFailure(err error, position, conversation string) error {
	if position == "" || len(failurediag.Details(err)) == 0 {
		return err
	}
	return &ScopedFailure{Cause: err, Position: position, Conversation: conversation}
}

// captureIntentForConversation 保存当前候选人的恢复线索，供点击打开或暂时离开已读聊天前登记。
func captureIntentForConversation(prepared shared.PreparedTask, conversation model.Conversation) cloud.AutoReplyCaptureIntent {
	return cloud.AutoReplyCaptureIntent{ReplyAfter: conversation.ReplyAfter, WitnessDigest: conversation.WitnessDigest, PositionID: firstNonEmpty(prepared.Position.ID, prepared.Request.PositionID), PlatformThreadID: conversation.PlatformThreadID, PlatformCandidateID: conversation.PlatformCandidateID, CandidateName: conversation.Name, PagePositionText: conversation.CommunicationPosition}
}

// processConversation 平铺执行页面读取、岗位归属、云端同步、简历门槛、AI决定和安全发送。
func (f *Flow) processConversation(ctx context.Context, prepared shared.PreparedTask, runtime model.AutoReplyRuntime, positions []cloud.AutoReplyPositionSnapshot, conversation model.Conversation, openSnapshot *model.AutoReplyConversationSnapshot, stats *shared.Stats) (resultErr error) {
	if !targetConversationAllowed(prepared.Request.AutoReplyTarget, conversation) {
		return fmt.Errorf("这个会话不在本次自动回复范围内，已跳过")
	}
	stats.Processed++
	if progress, ok := f.Logger.(interface{ UpdateReplyStats(shared.Stats) }); ok {
		progress.UpdateReplyStats(*stats)
		defer func() { progress.UpdateReplyStats(*stats) }()
	}
	if prepared.Request.PlatformAccountID != "" {
		if conversation.PlatformAccountID != "" && conversation.PlatformAccountID != prepared.Request.PlatformAccountID {
			return fmt.Errorf("招聘账号绑定发生变化，请停止后重新确认账号")
		}
		conversation.PlatformAccountID = prepared.Request.PlatformAccountID
	}
	lookup := cloud.AutoReplyCandidateLookup{
		PlatformID: prepared.Platform.ID, PlatformAccountID: conversation.PlatformAccountID,
		PlatformCandidateID: conversation.PlatformCandidateID,
		PlatformThreadID:    persistentConversationThreadID(conversation.PlatformThreadID, conversation.Key),
	}
	initialState, err := f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), lookup)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取候选人差量游标失败：%w", err)
	}
	knownMessageKeys := knownMessageKeysFromState(initialState)
	messageIdentityVersion := 0
	if f.Cloud.LocalAutoReplyEnabled() {
		messageIdentityVersion = prepared.Platform.Behavior.AutoReplyMessageIdentityVersion
		if messageIdentityVersion > 0 && (initialState.Conversation == nil || initialState.Conversation.MessageIdentityVersion < messageIdentityVersion) {
			// 旧算法即使曾经读到顶部也可能合并过重复消息；本次不使用增量锚点提前结束。
			knownMessageKeys = nil
		}
	}
	intent := captureIntentForConversation(prepared, conversation)
	var boundary *cloud.AutoReplyDeletionBoundary
	var inspected *model.AutoReplyConversationSnapshot
	if f.Cloud.LocalAutoReplyEnabled() {
		boundary, err = f.Cloud.AutoReplyDeletionBoundary(ctx, credentials(prepared), lookup)
		if err != nil {
			return err
		}
		if (boundary == nil || boundary.Sequence == 0) && initialState.Conversation != nil && initialState.Conversation.DeletionBoundary != nil {
			boundary = initialState.Conversation.DeletionBoundary
		}
		// 离线后来加入的设备可能只收到来源摘要，没有旧 thread 标记。先核身再判定是否允许完整采集。
		if boundary == nil && prepared.Platform.Behavior.SupportsPostDeletionHistory && lookup.PlatformCandidateID == "" && initialState.Conversation == nil {
			anyDeleted, lookupErr := f.Cloud.HasAutoReplyDeletionBoundaries(ctx, credentials(prepared))
			if lookupErr != nil {
				return lookupErr
			}
			if anyDeleted {
				header, headErr := f.inspectUnknownContact(ctx, prepared, runtime, conversation, lookup, intent)
				if headErr != nil {
					return headErr
				}
				inspected = &header
				lookup.PlatformCandidateID = header.PlatformCandidateID
				intent.PlatformCandidateID = header.PlatformCandidateID
				boundary, err = f.Cloud.AutoReplyDeletionBoundary(ctx, credentials(prepared), lookup)
				if err != nil {
					return err
				}
			}
		}
		if boundary == nil {
			if err = f.Cloud.BeginAutoReplyCapture(ctx, credentials(prepared), prepared.Platform.ID, conversation.PlatformAccountID, lookup.PlatformThreadID, intent); err != nil {
				if !errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
					stats.Failed++
				}
				return fmt.Errorf("保存候选人会话采集进度失败，尚未打开聊天：%w", err)
			}
			if inspected != nil {
				if err = f.Cloud.FinishAutoReplyContactProbe(ctx, credentials(prepared), lookup); err != nil {
					return err
				}
			}
		}
	}
	pageSnapshot := model.AutoReplyConversationSnapshot{}
	if boundary != nil {
		pageSnapshot, err = f.readPostDeletionConversation(ctx, prepared, runtime, conversation, openSnapshot, inspected, lookup, intent, *boundary, initialState, knownMessageKeys)
	} else if openSnapshot != nil && openSnapshot.CandidateName != "" {
		pageSnapshot = *openSnapshot
		pageSnapshot.Messages, pageSnapshot.HistoryComplete, err = runtime.ReadAutoReplyMessages(ctx, f.Browser, prepared.Platform, pageSnapshot, knownMessageKeys, cloud.AutoReplyMaxHistoryMessages)
	} else {
		pageSnapshot, err = runtime.OpenAutoReplyConversation(ctx, f.Browser, prepared.Platform, conversation, knownMessageKeys, cloud.AutoReplyMaxHistoryMessages)
	}
	if err != nil {
		if errors.Is(err, storage.ErrAutoReplyCandidateDeleted) {
			return err
		}
		stats.Failed++
		return fmt.Errorf("打开并读取候选人会话失败：%w", err)
	}
	if messageIdentityVersion > 0 && (initialState.Conversation == nil || initialState.Conversation.MessageIdentityVersion < messageIdentityVersion) && !pageSnapshot.HistoryComplete &&
		(len(pageSnapshot.Messages) > 0 || (initialState.Conversation != nil && initialState.Conversation.LastSyncedMessageKey != "")) {
		return fmt.Errorf("聊天历史还没有完整读到顶部，先保留采集待办；旧消息不会自动补发")
	}
	pageSnapshot = mergePageResumeCard(pageSnapshot, pageSnapshot.Messages)
	pageSnapshot = normalizePageSnapshot(conversation, pageSnapshot)
	if openSnapshot != nil {
		*openSnapshot = pageSnapshot
	}
	messages, latestCandidateKey, err := convertMessages(pageSnapshot.Messages)
	if err != nil {
		stats.Failed++
		return err
	}
	position, positionErr := resolvePosition(positions, pageSnapshot.CommunicationPosition)
	if positionErr == nil {
		prepared.Position.ID = position.Position.ID
		if binder, ok := f.Logger.(interface{ SetTaskPosition(string, string) }); ok {
			binder.SetTaskPosition(prepared.Request.TaskID, position.Position.ID)
		}
	}
	state := initialState
	if boundary != nil || persistentConversationThreadID(pageSnapshot.PlatformThreadID, pageSnapshot.Conversation.Key) != persistentConversationThreadID(conversation.PlatformThreadID, conversation.Key) ||
		pageSnapshot.PlatformCandidateID != conversation.PlatformCandidateID ||
		pageSnapshot.PlatformAccountID != conversation.PlatformAccountID || pageSnapshot.Phone != "" {
		state, err = f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
			PlatformID: prepared.Platform.ID, PlatformAccountID: pageSnapshot.PlatformAccountID,
			PlatformCandidateID: pageSnapshot.PlatformCandidateID,
			PlatformThreadID:    persistentConversationThreadID(pageSnapshot.PlatformThreadID, pageSnapshot.Conversation.Key),
			Phone:               pageSnapshot.Phone,
		})
		if err != nil {
			stats.Failed++
			return fmt.Errorf("读取候选人云端状态失败：%w", err)
		}
	}
	identity, err := f.savePlatformIdentity(ctx, prepared, pageSnapshot, state)
	if err != nil {
		stats.Failed++
		return err
	}
	cloudConversation, err := f.saveCloudConversation(ctx, prepared, position, positionErr, pageSnapshot, state, identity)
	if err != nil {
		stats.Failed++
		return err
	}
	if logger, ok := f.Logger.(interface{ BindAutoReplyConversation(string, string) }); ok {
		logger.BindAutoReplyConversation(cloudConversation.ID, position.Position.ID)
		defer func() {
			resultErr = scopedFailure(resultErr, position.Position.ID, cloudConversation.ID)
			logger.BindAutoReplyConversation("", "")
		}()
	}
	if _, err = f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
		ConversationID: cloudConversation.ID, HistoryComplete: pageSnapshot.HistoryComplete, Messages: messages, CaptureCompleted: true,
		MessageIdentityVersion: messageIdentityVersion, ReplyAfter: conversation.ReplyAfter, WitnessDigest: conversation.WitnessDigest,
	}); err != nil {
		stats.Failed++
		return fmt.Errorf("同步候选人聊天记录失败：%w", err)
	}
	pending, err := f.Cloud.PendingAutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
	if err != nil {
		return fmt.Errorf("读取待回复消息失败：%w", err)
	}
	resumeOnly := len(pending) == 0
	if resumeOnly && positionErr == nil && len(f.serverAnswers[cloudConversation.ID]) > 0 {
		return f.deliverAnswer(ctx, prepared, runtime, position, cloudConversation, pageSnapshot, state, f.serverAnswers[cloudConversation.ID][0], stats)
	}
	if resumeOnly && positionErr == nil && cloudConversation.GoalFollowup != nil {
		return f.deliverGoalFollowup(ctx, prepared, runtime, position, cloudConversation, pageSnapshot)
	}
	if resumeOnly && !autoReplyResumeRetryDue(cloudConversation.ResumeRetry, time.Now()) {
		stats.Skipped++
		if positionErr == nil && cloudConversation.ResumeRetry == nil {
			return f.reuseStoredStructuredResume(ctx, prepared, position, cloudConversation, pageSnapshot, state, false)
		}
		return nil
	}
	latestCandidateKey = cloudConversation.LastCandidateMessageKey
	if !resumeOnly {
		latestCandidateKey = firstNonEmpty(pending[len(pending)-1].PlatformMessageID, pending[len(pending)-1].Fingerprint)
	}
	if positionErr != nil {
		stats.Skipped++
		if err = f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, positionErr.Error(), "position_unresolved", latestCandidateKey); err != nil {
			return err
		}
		return f.Cloud.CompleteAutoReplyMessage(ctx, credentials(prepared), cloudConversation.ID, latestCandidateKey, "manual")
	}
	if position.Config.PhrasesVersion != "" || position.Config.Prompts != (cloud.AutoReplyPrompts{}) {
		fresh, fetchErr := f.Cloud.AutoReplySnapshot(ctx, credentials(prepared), position.Position.ID)
		if fetchErr != nil {
			return fmt.Errorf("读取本轮岗位话术和提示词失败：%w", fetchErr)
		}
		if !fresh.Config.Enabled || !fresh.Subscription.Active || !fresh.Subscription.AllowAutoReply || fresh.Position.ID != position.Position.ID {
			return fmt.Errorf("岗位自动回复已关闭或权限已变化，已保留本轮消息")
		}
		position = fresh
	}
	shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
		PositionID: position.Position.ID, Kind: "auto_reply", Phase: "loading", Stage: "sync", CandidateName: pageSnapshot.CandidateName,
		Reason: "聊天记录已经同步，正在核对简历", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})

	messages, err = f.Cloud.AutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取完整聊天记录失败：%w", err)
	}
	pageSnapshot = mergeCloudResumeCard(pageSnapshot, messages)
	if retry := cloudConversation.ResumeRetry; retry != nil && retry.SourceMessageID != "" && pageSnapshot.ResumeSourceMessageID == "" {
		pageSnapshot.ResumeCardAvailable = true
		pageSnapshot.ResumeSourceMessageID = retry.SourceMessageID
	}
	if openSnapshot != nil {
		*openSnapshot = pageSnapshot
	}
	if state.Candidate == nil {
		candidate, candidateErr := f.Cloud.EnsureLocalAutoReplyCandidate(ctx, credentials(prepared), cloudConversation)
		if candidateErr != nil {
			return candidateErr
		}
		state.Candidate = &candidate
		cloudConversation.CandidateID = candidate.ID
	}
	avatarHandled := f.syncCandidateAvatarBestEffort(ctx, prepared, pageSnapshot, state.Candidate, runtime)
	var resume *model.AutoReplyResumeBundle
	needsResume := pageSnapshot.ResumeCardAvailable || state.HasResumeAttachment
	if !resumeOnly && needsResume && (state.Candidate == nil || !state.Candidate.ResumeStructured || cloudConversation.ResumeRetry != nil ||
		(pageSnapshot.ResumeSourceMessageID != "" && state.Candidate.ResumeSourceMessageID != pageSnapshot.ResumeSourceMessageID)) {
		// 新问题先答；这里只登记待办，不点击附件、不读历史来源、不调用简历AI，也不消耗重试次数。
		cloudConversation, err = f.Cloud.EnqueueAutoReplyResumeRetry(ctx, credentials(prepared), cloudConversation.ID, pageSnapshot.ResumeSourceMessageID)
		if err != nil {
			return fmt.Errorf("登记简历待办失败：%w", err)
		}
	}
	if resumeOnly {
		claimed, claimErr := f.beginResumeRetry(ctx, prepared, &cloudConversation, pageSnapshot.ResumeSourceMessageID)
		if claimErr != nil || !claimed {
			return claimErr
		}
		resume, _, err = f.ensureResume(ctx, prepared, runtime, position, &cloudConversation, pageSnapshot, state, latestCandidateKey, stats)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, errAutoReplyResumeDeferred) {
				return err
			}
			return f.recordResumeRetryFailure(ctx, prepared, &cloudConversation, pageSnapshot, err)
		}
	}
	state, err = f.Cloud.AutoReplyCandidateState(ctx, credentials(prepared), cloud.AutoReplyCandidateLookup{
		PlatformID: prepared.Platform.ID, PlatformAccountID: pageSnapshot.PlatformAccountID,
		PlatformCandidateID: pageSnapshot.PlatformCandidateID,
		PlatformThreadID:    persistentConversationThreadID(pageSnapshot.PlatformThreadID, pageSnapshot.Conversation.Key),
		Phone:               firstNonEmpty(pageSnapshot.Phone, resumePhone(resume)),
	})
	if err != nil {
		stats.Failed++
		return fmt.Errorf("刷新正式简历状态失败：%w", err)
	}
	if cloudConversation.ResumeRetry != nil && !resumeOnly && state.Candidate != nil {
		candidate := *state.Candidate
		candidate.ResumeStructured = false
		state.Candidate = &candidate
	}
	if !avatarHandled {
		f.syncCandidateAvatarBestEffort(ctx, prepared, pageSnapshot, state.Candidate, runtime)
	}
	confirmations, err := f.Cloud.AutoReplyConfirmationItems(ctx, credentials(prepared), cloudConversation.ID)
	if err != nil {
		stats.Failed++
		return fmt.Errorf("读取候选人确认项失败：%w", err)
	}
	confirmations, err = f.syncPositionConfirmationItems(ctx, credentials(prepared), position, cloudConversation, confirmations)
	if err != nil {
		stats.Failed++
		return err
	}
	if f.Responder == nil {
		stats.Failed++
		return fmt.Errorf("自动回复 AI 处理器没有准备完整")
	}
	if resumeOnly {
		return f.reviewRecoveredResume(ctx, prepared, runtime, ReplyContext{
			TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), Position: position,
			AIConfig: prepared.Position.AI, EnableThinking: prepared.Position.EnableThinking,
			Conversation: cloudConversation, CandidateState: state, Messages: messages,
			ConfirmationItems: confirmations, PageSnapshot: pageSnapshot, Resume: resume,
			BasedOnMessageKey: latestCandidateKey, ConditionsOnly: true, AllowGoalFollowup: true,
		})
	}
	for refreshes := 0; ; refreshes++ {

		replyInput := ReplyContext{
			TaskID: prepared.Request.TaskID, Credentials: credentials(prepared), Position: position,
			AIConfig: prepared.Position.AI, EnableThinking: prepared.Position.EnableThinking,
			Conversation: cloudConversation, CandidateState: state, Messages: messages, PendingMessages: pending,
			ConfirmationItems: confirmations, PageSnapshot: pageSnapshot, Resume: resume,
			BasedOnMessageKey: latestCandidateKey,
		}
		var decision ReplyDecision
		changed := false
		fixedID := fixedReceiptID(replyInput)
		if fixedID != "" {
			if body := fixedReplyPhrase(position.Config, fixedID); body != "" && (fixedID != "receive_phone" || !previouslyReceivedPhone(replyInput)) {
				decision.Messages = []ReplyMessage{{Type: "answer", Content: body}}
			}
			if fixedID == "receive_phone" {
				// 手机号是新证据；固定签收之外仍让 AI 推进目标，避免收完联系方式就停住。
				reviewInput := replyInput
				reviewInput.ConditionsOnly, reviewInput.AllowGoalFollowup, reviewInput.ReceiptFollowup = true, true, true
				review, messageChanged, reviewErr := f.replyWhileMonitoring(ctx, prepared, runtime, pageSnapshot, reviewInput)
				changed = messageChanged
				if reviewErr != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					f.log(prepared.Request.TaskID, "phone_goal_review", "warning", time.Now(), reviewErr)
				} else if review.ManualReason == "" && len(review.Suggestions) == 0 && (len(review.Messages) == 0 || len(review.Messages) == 1 && review.Messages[0].Type == "confirmation") {
					decision.ConfirmationUpdates = review.ConfirmationUpdates
					decision.Messages = append(decision.Messages, review.Messages...)
				}
			}
		} else {
			shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
				PositionID: position.Position.ID, Kind: "auto_reply", Phase: "loading", Stage: "ai", CandidateName: pageSnapshot.CandidateName,
				Reason: "AI 正在认真看消息", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			})
			if err = f.loadKnownQuestions(ctx, &replyInput); err != nil {
				return fmt.Errorf("读取岗位待确认问题失败：%w", err)
			}
			var replyErr error
			decision, changed, replyErr = f.replyWhileMonitoring(ctx, prepared, runtime, pageSnapshot, replyInput)
			if replyErr != nil {
				stats.Failed++
				return fmt.Errorf("AI 自动回复判断失败：%w", replyErr)
			}
		}
		if !changed {
			unchanged, checkErr := f.latestCandidateMessageUnchanged(ctx, runtime, prepared, pageSnapshot, latestCandidateKey)
			if checkErr != nil {
				stats.Failed++
				return checkErr
			}
			changed = !unchanged
		}
		if changed {
			f.reportChangedMessage(prepared.Request.TaskID, pageSnapshot.CandidateName)
			if refreshes >= maxAutoReplyMessageRefreshes {
				stats.Skipped++
				return f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot,
					"候选人连续发来新消息，我没敢抢着插话，请人工看一下", "candidate_message_keeps_changing", latestCandidateKey)
			}
			pageMessages, historyComplete, readErr := runtime.ReadAutoReplyMessages(
				ctx, f.Browser, prepared.Platform, pageSnapshot, recentAutoReplyMessageKeys(messages), cloud.AutoReplyMaxHistoryMessages,
			)
			if readErr != nil {
				stats.Failed++
				return fmt.Errorf("候选人发来新消息后差量读取失败：%w", readErr)
			}
			pageSnapshot.Messages = pageMessages
			pageSnapshot.HistoryComplete = historyComplete
			delta, nextCandidateKey, convertErr := convertMessages(pageMessages)
			if convertErr != nil {
				stats.Failed++
				return convertErr
			}
			if len(delta) == 0 {
				stats.Skipped++
				return nil
			}
			if _, syncErr := f.Cloud.SyncAutoReplyMessages(ctx, credentials(prepared), cloud.AutoReplyMessageSyncRequest{
				ConversationID: cloudConversation.ID, HistoryComplete: historyComplete, Messages: delta,
			}); syncErr != nil {
				stats.Failed++
				return fmt.Errorf("同步候选人新消息失败：%w", syncErr)
			}
			if nextCandidateKey == "" {
				stats.Skipped++
				return nil
			}
			messages, err = f.Cloud.AutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
			if err != nil {
				stats.Failed++
				return fmt.Errorf("刷新完整聊天记录失败：%w", err)
			}
			confirmations, err = f.Cloud.AutoReplyConfirmationItems(ctx, credentials(prepared), cloudConversation.ID)
			if err != nil {
				stats.Failed++
				return fmt.Errorf("刷新候选人确认项失败：%w", err)
			}
			pending, err = f.Cloud.PendingAutoReplyMessages(ctx, credentials(prepared), cloudConversation.ID)
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				return nil
			}
			latestCandidateKey = nextCandidateKey
			continue
		}
		if err = f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
			return err
		}
		if err = f.Cloud.CommitAutoReplyProposal(ctx, credentials(prepared), cloudConversation.ID, latestCandidateKey, position.Config.Version, decision.ConfirmationUpdates, decision.Suggestions); err != nil {
			return fmt.Errorf("保存本轮已复核结果失败：%w", err)
		}
		confirmations, err = f.Cloud.AutoReplyConfirmationItems(ctx, credentials(prepared), cloudConversation.ID)
		if err != nil {
			return err
		}
		if fixedID == "receive_phone" && state.Candidate != nil {
			phone := phoneDigits(mainlandChatMobile.FindString(pending[0].TextContent))
			if err = f.Cloud.SaveAutoReplyChatPhone(ctx, credentials(prepared), cloudConversation.ID, state.Candidate.ID, latestCandidateKey, phone); err != nil {
				if noticeErr := f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, err.Error(), "phone_record_failed", latestCandidateKey); noticeErr != nil {
					return noticeErr
				}
				return f.completeReplyBatch(ctx, prepared, replyInput, "manual")
			}
			state.Candidate.Phone = phone
		}
		if fixedID != "" && len(decision.Messages) == 0 {
			return f.completeReplyBatch(ctx, prepared, replyInput, "completed")
		}
		outcome := "sent"
		if strings.TrimSpace(decision.ManualReason) != "" {
			outcome = "manual"
			reasonKey := firstNonEmpty(decision.ReasonKey, "ai_manual_handoff")
			if reasonKey == "missing_recruitment_info" && position.Config.PhrasesVersion != "" {
				if len(decision.HandoffQuestions) == 0 {
					return fmt.Errorf("本轮缺少可核实的原话，已保留待处理消息")
				}
				for _, q := range decision.HandoffQuestions {
					if _, err = f.Cloud.RecordAutoReplyQuestion(ctx, credentials(prepared), position.Position.ID, q.ExistingQuestionID, q.Question, cloudConversation.ID, q.sourceKey); err != nil {
						return fmt.Errorf("登记待确认问题失败：%w", err)
					}
				}
			}
			if err = f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, decision.ManualReason, reasonKey, latestCandidateKey); err != nil {
				return err
			}
			if reasonKey == "missing_recruitment_info" {
				decision.Messages = handoffReplyMessages(decision.KnownAnswer, fixedReplyPhrase(position.Config, "manual_followup"))
				if len(decision.Messages) > 0 {
					// 等 HR 补资料不等于 HR 已接管，后续简历证据仍可推进候选人的独立目标。
					outcome = "awaiting_hr"
				}
				if len(decision.Messages) == 1 && decision.HandoffFollowup != nil {
					decision.Messages = append(decision.Messages, *decision.HandoffFollowup)
				}
			}
			if len(decision.Messages) == 0 {
				stats.Skipped++
				return f.completeReplyBatch(ctx, prepared, replyInput, "manual")
			}
		}
		if len(decision.Messages) == 0 || len(decision.Messages) > 2 {
			stats.Failed++
			return fmt.Errorf("AI 需要准备1到2条候选人消息")
		}
		sentCount := 0
		previousContent := ""
		for index, reply := range decision.Messages {
			if index > 0 {
				ready, lastKey, checkErr := f.latestMessageAllowsFollowup(ctx, runtime, prepared, pageSnapshot, previousContent)
				if checkErr != nil {
					stats.Failed++
					return checkErr
				}
				pageSnapshot.ExpectedSendMessageKey = lastKey
				if !ready {
					f.reportChangedMessage(prepared.Request.TaskID, pageSnapshot.CandidateName)
					break
				}
			}
			if err = f.verifyCurrentReplyConfiguration(ctx, prepared, position); err != nil {
				return err
			}
			content := strings.TrimSpace(reply.Content)
			if !oneOf(reply.Type, "answer", "confirmation") || content == "" || len([]rune(content)) > maxAutoReplyMessageRunes {
				stats.Failed++
				return fmt.Errorf("AI 第%d条消息类型不正确、内容为空或超过200字", index+1)
			}
			if err = checkPhoneReply(inspectPhoneContact(replyInput), position.Config, content); err != nil {
				return err
			}
			var goalBinding *cloud.AutoReplyGoalSend
			if reply.Type == "confirmation" {
				for _, goal := range confirmations {
					if goal.ID == reply.ConfirmationItemID && cloud.AutoReplyGoalCanAsk(goal) && (reply.GoalContentKey == "" || reply.GoalContentKey == cloud.AutoReplyGoalContentKey(goal)) {
						goalBinding = &cloud.AutoReplyGoalSend{ConfirmationItemID: goal.ID, ExpectedVersion: cloud.AutoReplyConfirmationRevision(goal), BasedOnMessageKey: latestCandidateKey, ExpectedLatestKey: pageSnapshot.ExpectedSendMessageKey}
						break
					}
				}
				if goalBinding == nil || candidateEndedContact(pending) {
					break
				}
			}
			sent, sendErr := f.sendVerifiedMessageWithGoal(ctx, prepared, runtime, cloudConversation, pageSnapshot, latestCandidateKey, content, index == 0, goalBinding, index+1)
			if errors.Is(sendErr, model.ErrAutoReplySupersededBeforeSend) {
				f.reportChangedMessage(prepared.Request.TaskID, pageSnapshot.CandidateName)
				break
			}
			if errors.Is(sendErr, errAutoReplySendNeedsReview) {
				if err = f.notifyUnresolved(ctx, prepared, position, cloudConversation, pageSnapshot, sendErr.Error(), "send_unknown", latestCandidateKey); err != nil {
					return err
				}
				return f.completeReplyBatch(ctx, prepared, replyInput, "manual")
			}
			if sendErr != nil {
				stats.Failed++
				return sendErr
			}
			previousContent = content
			if !sent {
				continue
			}
			sentCount++
		}
		if sentCount == 0 {
			stats.Skipped++
			return nil
		}
		if sentCount == 1 && len(decision.Messages) == 1 && (outcome == "sent" || outcome == "awaiting_hr") && fixedID == "" && !candidateEndedContact(pending) {
			goalPlanned := false
			reviewPlan := decision
			if outcome == "awaiting_hr" {
				reviewPlan.ManualReason = ""
			}
			if cloudConversation.ResumeRetry == nil && needsMessageGoalReview(reviewPlan, confirmations) {
				var goalSent bool
				goalPlanned, goalSent, err = f.reviewGoalsAfterAnswer(ctx, prepared, runtime, replyInput, previousContent)
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					f.log(prepared.Request.TaskID, "message_goal_review", "warning", time.Now(), err)
				}
				if goalSent {
					sentCount++
				}
			}
			if outcome == "sent" && !goalPlanned {
				err = f.requestResumeAfterAnswer(ctx, prepared, runtime, position, &cloudConversation, pageSnapshot, state, latestCandidateKey, previousContent, decision.Messages[0].RequestsResume)
			}
			if err != nil && !goalPlanned && outcome == "sent" {
				return err
			}
		}
		changedAfterSend, syncErr := f.syncReplyChangesBeforeCompletion(ctx, prepared, runtime, cloudConversation, pageSnapshot, latestCandidateKey, messages)
		if syncErr != nil {
			return syncErr
		}
		if changedAfterSend {
			return f.Cloud.CompleteAutoReplyMessage(ctx, credentials(prepared), cloudConversation.ID, latestCandidateKey, outcome)
		}
		if err = f.completeReplyBatch(ctx, prepared, replyInput, outcome); err != nil {
			return err
		}
		stats.Succeeded++
		shared.ReportAnalysis(f.Logger, prepared.Request.TaskID, shared.AnalysisStatus{
			PositionID: position.Position.ID, Kind: "auto_reply", Phase: "result", Stage: "sent", Terminal: true,
			CandidateName: pageSnapshot.CandidateName, Accepted: boolPointer(true),
			Reason: fmt.Sprintf("候选人消息已经回复，共%d条", sentCount), UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		})
		return nil
	}
}

// knownMessageKeysFromState 优先返回会话权威的最后同步游标，缺失时再兼容云端最近消息列表。
func knownMessageKeysFromState(state cloud.AutoReplyCandidateState) []string {
	if state.Conversation != nil {
		if keys := cleanMessageKeys([]string{state.Conversation.LastSyncedMessageKey}); len(keys) > 0 {
			return keys
		}
	}
	return cleanMessageKeys(state.RecentMessageKeys)
}

// recentAutoReplyMessageKeys 返回聊天记录最后两条可用的稳定消息编号。
func recentAutoReplyMessageKeys(messages []cloud.AutoReplyMessage) []string {
	start := len(messages) - 2
	if start < 0 {
		start = 0
	}
	keys := make([]string, 0, 2)
	for _, message := range messages[start:] {
		keys = append(keys, firstNonEmpty(message.PlatformMessageID, message.Fingerprint))
	}
	return cleanMessageKeys(keys)
}

// cleanMessageKeys 清理空白消息编号并只保留最后两条。
func cleanMessageKeys(values []string) []string {
	result := make([]string, 0, 2)
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			value = strings.TrimPrefix(strings.TrimPrefix(value, "id:"), "fp:")
			result = append(result, value)
		}
	}
	if len(result) > 2 {
		result = result[len(result)-2:]
	}
	return result
}

// normalizePageSnapshot 用未读列表字段补齐打开会话后可能为空的身份字段。
func normalizePageSnapshot(conversation model.Conversation, snapshot model.AutoReplyConversationSnapshot) model.AutoReplyConversationSnapshot {
	snapshot.Conversation = conversation
	snapshot.CandidateName = firstNonEmpty(snapshot.CandidateName, conversation.Name)
	if snapshot.HistoryAfter == nil {
		snapshot.AvatarURL = firstNonEmpty(snapshot.AvatarURL, conversation.AvatarURL)
		snapshot.Gender = firstNonEmpty(snapshot.Gender, conversation.Gender)
	} else {
		// 新代仅将列表作为定位依据，旧头像、摘要或其他未知字段不进入分析上下文。
		snapshot.AvatarURL, snapshot.Gender, snapshot.Phone = "", "", ""
		snapshot.Conversation.AvatarURL, snapshot.Conversation.Gender, snapshot.Conversation.Summary = "", "", ""
		snapshot.Conversation.Fields = nil
	}
	snapshot.PlatformThreadID = firstNonEmpty(snapshot.PlatformThreadID, conversation.PlatformThreadID)
	snapshot.PlatformCandidateID = firstNonEmpty(snapshot.PlatformCandidateID, conversation.PlatformCandidateID)
	snapshot.PlatformAccountID = firstNonEmpty(snapshot.PlatformAccountID, conversation.PlatformAccountID)
	snapshot.CommunicationPosition = firstNonEmpty(snapshot.CommunicationPosition, conversation.CommunicationPosition)
	return snapshot
}

// savePlatformIdentity 优先保存平台候选人编号，获取不到时由手机号和会话编号后备。
func (f *Flow) savePlatformIdentity(ctx context.Context, prepared shared.PreparedTask, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState) (cloud.CandidatePlatformIdentity, error) {
	if strings.TrimSpace(snapshot.PlatformCandidateID) == "" {
		if state.Identity != nil {
			return *state.Identity, nil
		}
		return cloud.CandidatePlatformIdentity{}, nil
	}
	candidateID := ""
	if state.Candidate != nil {
		candidateID = state.Candidate.ID
	}
	identityID := ""
	if state.Identity != nil {
		identityID = state.Identity.ID
	}
	identity, err := f.Cloud.SaveAutoReplyIdentity(ctx, credentials(prepared), cloud.CandidatePlatformIdentity{
		ID: identityID, CandidateID: candidateID, PlatformID: prepared.Platform.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformCandidateID: snapshot.PlatformCandidateID,
		CandidateName: snapshot.CandidateName, Gender: snapshot.Gender, NormalizedPhone: snapshot.Phone,
	})
	if err != nil {
		return cloud.CandidatePlatformIdentity{}, fmt.Errorf("保存平台候选人身份失败：%w", err)
	}
	return identity, nil
}

// saveCloudConversation 保存正式简历存在前也可使用的临时会话和岗位归属结果。
func (f *Flow) saveCloudConversation(ctx context.Context, prepared shared.PreparedTask, position cloud.AutoReplyPositionSnapshot, positionErr error, snapshot model.AutoReplyConversationSnapshot, state cloud.AutoReplyCandidateState, identity cloud.CandidatePlatformIdentity) (cloud.AutoReplyConversation, error) {
	item := cloud.AutoReplyConversation{
		PositionID: position.Position.ID, PlatformIdentityID: identity.ID,
		PlatformAccountID: snapshot.PlatformAccountID, PlatformID: prepared.Platform.ID,
		PlatformThreadID: persistentConversationThreadID(snapshot.PlatformThreadID, snapshot.Conversation.Key), CandidateName: snapshot.CandidateName,
		Gender: snapshot.Gender, PagePositionText: snapshot.CommunicationPosition,
		Status: "active", HistoryComplete: snapshot.HistoryComplete,
	}
	if state.Candidate != nil {
		item.CandidateID = state.Candidate.ID
	}
	if state.Conversation != nil {
		item.DeletionBoundary = state.Conversation.DeletionBoundary
		item.ID = state.Conversation.ID
		item.EngagementID = state.Conversation.EngagementID
		item.LastSyncedMessageKey = state.Conversation.LastSyncedMessageKey
		item.LastCandidateMessageKey = state.Conversation.LastCandidateMessageKey
		item.ResumeRetry = state.Conversation.ResumeRetry
		if status := preservedAutoReplyConversationStatus(state.Conversation.Status); status != "" {
			item.Status = status
		}
	}
	if positionErr != nil {
		item.PositionID = ""
		item.UnresolvedReason = positionErr.Error()
	}
	now := time.Now().UTC()
	item.LastCheckedAt = &now
	saved, err := f.Cloud.SaveAutoReplyConversation(ctx, credentials(prepared), item)
	if err != nil {
		return cloud.AutoReplyConversation{}, fmt.Errorf("保存候选人会话失败：%w", err)
	}
	return saved, nil
}

// preservedAutoReplyConversationStatus 只保留云端数据库允许的已有会话状态，避免每轮检查覆盖等待简历状态。
func preservedAutoReplyConversationStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "active", "waiting_resume", "ready", "unresolved", "ended":
		return value
	default:
		return ""
	}
}

// convertMessages 校验平台消息方向并生成云端幂等指纹和最新候选人消息键。
func convertMessages(items []model.ConversationMessage) ([]cloud.AutoReplyMessage, string, error) {
	result := make([]cloud.AutoReplyMessage, 0, len(items))
	latestCandidateKey := ""
	for index, item := range items {
		direction := strings.ToLower(strings.TrimSpace(item.Direction))
		if direction != "candidate" && direction != "self" && direction != "system" {
			return nil, "", fmt.Errorf("第%d条聊天消息方向无法确认", index+1)
		}
		card := item.CardContent
		if len(strings.TrimSpace(string(card))) == 0 {
			card = json.RawMessage(`{}`)
		}
		fingerprint := firstNonEmpty(item.Key, messageFingerprint(item, index))
		message := cloud.AutoReplyMessage{
			PlatformMessageID: strings.TrimSpace(item.PlatformMessageID), Fingerprint: fingerprint,
			Direction: direction, MessageType: firstNonEmpty(item.MessageType, "text"),
			TextContent: item.TextContent, CardContent: card, SenderName: item.SenderName,
			PlatformSentAt: item.SentAt, IngestedAt: time.Now().UTC(),
		}
		result = append(result, message)
		if direction == "candidate" {
			latestCandidateKey = firstNonEmpty(message.PlatformMessageID, message.Fingerprint)
		} else if direction == "self" {
			latestCandidateKey = ""
		}
	}
	return result, latestCandidateKey, nil
}

// messageFingerprint 为没有平台消息编号的消息生成稳定本地指纹。
func messageFingerprint(item model.ConversationMessage, index int) string {
	sentAt := ""
	if item.SentAt != nil {
		sentAt = item.SentAt.UTC().Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		item.Direction, item.MessageType, item.TextContent, item.SenderName, sentAt, fmt.Sprint(index),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

// firstNonEmpty 返回第一段非空文字。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// persistentConversationThreadID 返回云端持久化会话使用的稳定编号。
// 页面有原生编号时原样使用；没有时使用明确带 local 前缀的本地键，不参与页面身份核对。
func persistentConversationThreadID(platformThreadID string, conversationKey string) string {
	if platformThreadID = strings.TrimSpace(platformThreadID); platformThreadID != "" {
		return platformThreadID
	}
	conversationKey = strings.TrimSpace(conversationKey)
	if conversationKey == "" || strings.HasPrefix(conversationKey, "local:") {
		return conversationKey
	}
	return "local:" + conversationKey
}

// mergePageResumeCard 从本轮页面消息补齐简历卡片状态，并保留之前已经确认的卡片。
func mergePageResumeCard(snapshot model.AutoReplyConversationSnapshot, messages []model.ConversationMessage) model.AutoReplyConversationSnapshot {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if strings.EqualFold(strings.TrimSpace(message.Direction), "candidate") && strings.EqualFold(strings.TrimSpace(message.MessageType), "resume") {
			snapshot.ResumeCardAvailable = true
			snapshot.ResumeSourceMessageID = firstNonEmpty(message.PlatformMessageID, message.Key, snapshot.ResumeSourceMessageID)
			return snapshot
		}
	}
	return snapshot
}

// mergeCloudResumeCard 从云端完整聊天记录恢复旧简历卡片状态，避免下载失败后再次索要。
func mergeCloudResumeCard(snapshot model.AutoReplyConversationSnapshot, messages []cloud.AutoReplyMessage) model.AutoReplyConversationSnapshot {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if strings.EqualFold(strings.TrimSpace(message.Direction), "candidate") && strings.EqualFold(strings.TrimSpace(message.MessageType), "resume") {
			snapshot.ResumeCardAvailable = true
			snapshot.ResumeSourceMessageID = firstNonEmpty(message.PlatformMessageID, message.Fingerprint, snapshot.ResumeSourceMessageID)
			return snapshot
		}
	}
	return snapshot
}

// boolPointer 返回供悬浮窗状态使用的布尔指针。
func boolPointer(value bool) *bool {
	return &value
}

// resumePhone 安全读取本轮页面简历中的手机号。
func resumePhone(value *model.AutoReplyResumeBundle) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.Phone)
}
