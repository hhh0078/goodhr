// Package cloudserver 本文件在每次发命令和回执前核对云端租约，保存可追溯的操作摘要。
package cloudserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"goodhr5/local-agent-go-new/internal/storage"
)

type runLease struct {
	CurrentStep  string                 `json:"current_step,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Analysis     *shared.AnalysisStatus `json:"analysis,omitempty"`
	Stats        shared.Stats           `json:"stats"`
	ID           string                 `json:"id"`
	MachineID    string                 `json:"machine_id"`
	PositionID   string                 `json:"position_id"`
	Status       string                 `json:"status"`
	Expires      time.Time              `json:"expires_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// updateLease 只允许本次运行取得或续期账号独占权；超过租约即使旧进程尚在也不能发令。
func (r *activeRun) updateLease(ctx context.Context, action string) error {
	return r.client.ServerAutoReplyTransaction(ctx, r.cred, func(tx *storage.AutoReplyTx) error {
		keys := []string{r.leaseKey}
		if r.legacyLeaseKey != "" && r.legacyLeaseKey != r.leaseKey {
			keys = append(keys, r.legacyLeaseKey)
		}
		now := time.Now().UTC()
		for _, key := range keys {
			raw, err := tx.ServerLedger("runtime_runs", key, nil)
			if err != nil {
				return err
			}
			var old runLease
			if len(raw) > 0 {
				if err = json.Unmarshal(raw, &old); err != nil {
					return err
				}
			}
			if action == "claim" {
				if old.Status == "running" && old.Expires.After(now) {
					return fmt.Errorf("招聘账号已有另一个云任务")
				}
			} else if old.ID != r.id || old.MachineID != r.scope.machine || action == "heartbeat" && !old.Expires.After(now) {
				return fmt.Errorf("云端运行租约已变更")
			}
		}
		status := action
		if action == "claim" || action == "heartbeat" {
			status = "running"
		}
		curr := runLease{ID: r.id, MachineID: r.scope.machine, PositionID: r.position, Status: status, Expires: now.Add(90 * time.Second), UpdatedAt: now}
		r.mu.Lock()
		curr.CurrentStep, curr.ErrorMessage, curr.Stats = r.currentStep, r.errorMessage, r.stats
		if r.analysis.Kind != "" {
			// 账号租约跨候选复用，仅保留运行计数和阶段。姓名、证据和自由文本由可随简历删除的会话事件保管。
			a := r.analysis
			curr.Analysis = &shared.AnalysisStatus{Kind: a.Kind, Phase: a.Phase, Stage: a.Stage, Terminal: a.Terminal,
				ProcessedCount: a.ProcessedCount, SucceededCount: a.SucceededCount, SkippedCount: a.SkippedCount, UpdatedAt: a.UpdatedAt}
		}
		r.mu.Unlock()
		for _, key := range keys {
			if _, err := tx.ServerLedger("runtime_runs", key, curr); err != nil {
				return err
			}
		}
		_, err := tx.ServerLedger("runtime_runs", "run:"+r.id, curr)
		return err
	})
}

// commandState 在首次派发时绑定操作对象与源消息，延迟回执不能借用后来打开的候选人窗口。
type commandState struct {
	SendIntentID       string    `json:"send_intent_id,omitempty"`          // 仅服务器账本保存，设备协议不携带目标或业务版本。
	ResumeOfferKey     string    `json:"resume_offer_message_id,omitempty"` // 区分候选人主动邀约的同意与HR主动索要，不向网页暴露原生卡片内容。
	IssuedAt           time.Time `json:"issued_at,omitzero"`
	StartedAt          time.Time `json:"started_at,omitzero"`
	RunID              string    `json:"run_id"`
	MachineID          string    `json:"machine_id"`
	PositionID         string    `json:"position_id"`
	ConversationID     string    `json:"conversation_id,omitempty"`
	ThreadID           string    `json:"platform_thread_id,omitempty"`
	AccountID          string    `json:"platform_account_id,omitempty"`
	SourceKey          string    `json:"source_key,omitempty"`
	Action             string    `json:"action"`
	Status             string    `json:"status"`
	ResultCode         string    `json:"result_code,omitempty"`
	OutboundMessageKey string    `json:"outbound_message_key,omitempty"`
	AttachmentIDs      []string  `json:"attachment_ids,omitempty"`
	Deadline           time.Time `json:"deadline"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// persistCommand 仅允许领取者完成未超时的同一命令；附件与最终回执在同一权威事务内保存。
func (r *activeRun) persistCommand(ctx context.Context, c replybridge.Command, status string, result replybridge.Result) error {
	if c.ID == "" || c.RunID != r.id || c.PlatformID != r.platform {
		return fmt.Errorf("命令与当前运行不匹配")
	}
	rejected := false
	rejectionReason := cloud.ErrAutoReplyGoalChanged
	err := r.client.ServerAutoReplyTransaction(ctx, r.cred, func(tx *storage.AutoReplyTx) error {
		raw, err := tx.ServerLedger("runtime_runs", r.leaseKey, nil)
		if err != nil {
			return err
		}
		var lease runLease
		now := time.Now().UTC()
		if json.Unmarshal(raw, &lease) != nil || lease.ID != r.id || lease.Status != "running" || !lease.Expires.After(now) {
			return fmt.Errorf("设备操作的租约已失效")
		}
		raw, err = tx.ServerLedger("runtime_commands", c.ID, nil)
		if err != nil {
			return err
		}
		var bound commandState
		if status == "pending" {
			if len(raw) != 0 {
				return fmt.Errorf("命令已经登记")
			}
			var req replybridge.Request
			if err = json.Unmarshal(c.Payload, &req); err != nil {
				return err
			}
			r.mu.Lock()
			conversationID := r.conversation
			r.mu.Unlock()
			bound = commandState{IssuedAt: now, RunID: r.id, PositionID: r.position, MachineID: r.scope.machine, ConversationID: conversationID, Action: c.Action, Deadline: c.Deadline}
			if conversationID != "" {
				// 账号任务可处理多个岗位，命令凭据必须采用本次已绑定会话的真实岗位，不能沿用启动岗位。
				row, found, readErr := tx.Get("conversations", conversationID)
				if readErr != nil {
					return readErr
				}
				var conversation cloud.AutoReplyConversation
				if !found || json.Unmarshal(row.Data, &conversation) != nil || conversation.PlatformID != r.platform {
					return fmt.Errorf("设备命令尚未绑定真实平台会话")
				}
				if conversation.PositionID != "" {
					bound.PositionID = conversation.PositionID
				} else if c.Action == "read_online_profile" || c.Action == "request_info" {
					return fmt.Errorf("资料命令尚未核实当前会话岗位")
				}
				if req.Snapshot.PlatformThreadID != "" && req.Snapshot.PlatformThreadID != conversation.PlatformThreadID || req.Snapshot.PlatformAccountID != "" && req.Snapshot.PlatformAccountID != conversation.PlatformAccountID {
					return fmt.Errorf("设备命令来源与已绑定会话不一致")
				}
			}
			if c.Action == "send" || c.Action == "request_info" {
				bound.SendIntentID = cloud.AutoReplySendIntentFromContext(ctx)
				if c.Action == "request_info" && (bound.SendIntentID == "" || model.CandidateInfoRequestKind(req.InfoRequest) == "") {
					return fmt.Errorf("平台资料请求必须绑定单一类型的服务器发送意图")
				}
				if err = cloud.ValidateAutoReplySendIntent(tx, bound.SendIntentID, conversationID, commandOutboundContent(c.Action, req)); err != nil {
					return err
				}
				if err = validateResumeOfferCommand(tx, c.Action, req, conversationID, bound.SendIntentID); err != nil {
					return err
				}
			}
			if conversationID != "" {
				bound.ThreadID, bound.AccountID, bound.SourceKey = req.Snapshot.PlatformThreadID, req.Snapshot.PlatformAccountID, req.Snapshot.ResumeSourceMessageID
				if c.Action == "request_info" && req.Snapshot.ResumeOfferMessageID != "" {
					bound.ResumeOfferKey, bound.SourceKey = req.Snapshot.ResumeOfferMessageID, req.Snapshot.ResumeOfferMessageID
				}
			}
		} else {
			if json.Unmarshal(raw, &bound) != nil || bound.RunID != r.id || bound.MachineID != r.scope.machine || bound.Action != c.Action {
				return fmt.Errorf("无法核对原始命令")
			}
			switch status {
			case "executing":
				if bound.Status != "pending" || !bound.Deadline.After(now) {
					return fmt.Errorf("命令已领取或过期")
				}
				if c.Action == "send" || c.Action == "request_info" && bound.SendIntentID != "" {
					var req replybridge.Request
					if err = json.Unmarshal(c.Payload, &req); err != nil {
						return err
					}
					err = cloud.ValidateAutoReplySendIntent(tx, bound.SendIntentID, bound.ConversationID, commandOutboundContent(c.Action, req))
					if err == nil {
						err = validateResumeOfferCommand(tx, c.Action, req, bound.ConversationID, bound.SendIntentID)
					}
					if err == nil && c.Action == "send" {
						if bound.SendIntentID == "" {
							err = cloud.ErrAutoReplyTextPolicyChanged
						} else {
							// 配置路径不进入业务实体事务适配器；在现有领取事务内读取一次真实快照，避免额外的状态往返。
							var current cloud.AutoReplyPositionSnapshot
							current, err = r.client.AutoReplySnapshot(ctx, r.cred, bound.PositionID)
							if err == nil {
								err = cloud.ValidateAutoReplyQueuedText(tx, bound.SendIntentID, bound.ConversationID, req.Message, current)
							}
						}
					}
					if err != nil {
						if !errors.Is(err, cloud.ErrAutoReplyGoalChanged) && !errors.Is(err, cloud.ErrAutoReplyTextPolicyChanged) {
							return err
						}
						rejectionReason = err
						bound.Status, bound.ResultCode, bound.UpdatedAt = "cancelled", "REPLY_SUPERSEDED", now
						_, err = tx.ServerLedger("runtime_commands", c.ID, bound)
						rejected = err == nil
						return err
					}
				}
				now = time.Now().UTC()
				if !lease.Expires.After(now) || !bound.Deadline.After(now) {
					return fmt.Errorf("配置核对期间租约或命令已过期，暂不派发")
				}
				bound.StartedAt = now
			case "completed":
				if bound.Status != "executing" || !bound.Deadline.After(now) {
					return fmt.Errorf("命令已失效，拒绝新的结果")
				}
				if c.Action == "read_online_profile" && result.Error == "" {
					var metadata replybridge.Response
					if json.Unmarshal(result.Value, &metadata) != nil || metadata.OnlineProfile.SourceKey == "" || len(metadata.OnlineProfile.Screenshots) > 0 {
						return fmt.Errorf("在线资料回执缺少来源摘要或包含不应持久化的截图")
					}
					bound.SourceKey = metadata.OnlineProfile.SourceKey
				}
				bound.AttachmentIDs, err = r.fileReceipts(ctx, tx, bound, c, result)
				if err != nil {
					return err
				}
				if c.Action == "send" && result.Error == "" {
					var response replybridge.Response
					if json.Unmarshal(result.Value, &response) != nil || response.Latest.Direction != "self" {
						return fmt.Errorf("发送回执缺少实际己方消息")
					}
					bound.OutboundMessageKey = response.Latest.PlatformMessageID
					if bound.OutboundMessageKey == "" {
						bound.OutboundMessageKey = response.Latest.Key
					}
					var req replybridge.Request
					if err = json.Unmarshal(c.Payload, &req); err != nil {
						return err
					}
					if err = cloud.ConfirmAutoReplySendIntent(tx, bound.SendIntentID, bound.ConversationID, req.Message, bound.OutboundMessageKey); err != nil {
						return err
					}
				}
			case "unknown":
				if bound.Status != "pending" && bound.Status != "executing" {
					return fmt.Errorf("命令已有终态")
				}
			default:
				return fmt.Errorf("未知命令状态")
			}
		}
		bound.Status = status
		bound.ResultCode = result.Code
		bound.UpdatedAt = now
		_, err = tx.ServerLedger("runtime_commands", c.ID, bound)
		return err
	})
	if err == nil && rejected {
		return &replybridge.CommandRejectedError{Message: rejectionReason.Error()}
	}
	return err
}

// commandOutboundContent 让原生资料命令与普通发送共用账本校验，资料动作只使用固定类型说明，不把候选人号码放入命令摘要。
func commandOutboundContent(action string, request replybridge.Request) string {
	if action == "request_info" {
		if kind := model.CandidateInfoRequestKind(request.InfoRequest); kind != "" {
			if request.Snapshot.ResumeOfferMessageID != "" {
				if kind == "resume" {
					return cloud.AutoReplyResumeOfferContent
				}
				return ""
			}
			return "平台资料索要：" + kind
		}
		return ""
	}
	return request.Message
}

// validateResumeOfferCommand 在派发和领取时复核同一来源及其独立发送意图，普通索要账本不能借来点击同意。
func validateResumeOfferCommand(tx *storage.AutoReplyTx, action string, request replybridge.Request, conversation, intent string) error {
	if action != "request_info" || request.Snapshot.ResumeOfferMessageID == "" {
		return nil
	}
	offer := request.Snapshot.ResumeOfferMessageID
	if model.CandidateInfoRequestKind(request.InfoRequest) != "resume" || intent != cloud.AutoReplySendIntentID(conversation, cloud.AutoReplyResumeOfferSendKey(offer), 0) {
		return fmt.Errorf("%w：接收邀请未绑定其原生来源的独立操作", cloud.ErrAutoReplyGoalChanged)
	}
	return cloud.ValidateAutoReplyResumeOfferSource(tx, conversation, offer)
}
