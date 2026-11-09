// Package cloud 本文件在当前权威仓储保存通知及授权结果发布待办，网络请求在业务事务外执行。
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"net/http"
	"time"
)

// autoReplyOutboxItem 不保存登录凭证，恢复时只使用当前已验证账号的凭证。
type autoReplyOutboxItem struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Status        string          `json:"status"`
	Payload       json.RawMessage `json:"payload"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	LeaseUntil    time.Time       `json:"lease_until"`
	LastError     string          `json:"last_error"`
}

// autoReplyCandidateUpload 保存一次结构化同步所需的本地关联和已脱敏资料。
type autoReplyCandidateUpload struct {
	LocalCandidateID string                  `json:"local_candidate_id"`
	Input            AutoReplyCandidateInput `json:"input"`
	Hash             string                  `json:"hash"`
	Revision         int64                   `json:"revision"`
}

// autoReplyReportUpload 保存本地已完成报告的授权发布待办。
type autoReplyReportUpload struct {
	LocalCandidateID string                        `json:"local_candidate_id"`
	Key              string                        `json:"key"`
	Model            string                        `json:"model"`
	Report           AutoReplyRecommendationReport `json:"report"`
}

// enqueueAutoReplyOutbox 先持久登记待办，相同键完成后不再产生网络副作用。
func (c *Client) enqueueAutoReplyOutbox(ctx context.Context, credentials AgentCredentials, id, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {

		old, exists, err := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		if err != nil {
			return err
		}
		if exists {
			if kind != "candidate" || bytes.Equal(old.Payload, raw) {
				return nil
			}
			var previous, incoming autoReplyCandidateUpload
			if err = json.Unmarshal(old.Payload, &previous); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &incoming); err != nil {
				return err
			}
			if incoming.Hash == previous.Hash || incoming.Revision < previous.Revision {
				return nil
			}
			old.Payload = raw
			old.Status = "pending"
			old.NextAttemptAt = time.Time{}
			old.LastError = ""
			old.Attempts = 0
			return tx.Put("outbox", id, old)
		}

		return tx.Put("outbox", id, autoReplyOutboxItem{ID: id, Kind: kind, Status: "pending", Payload: raw})
	})
}

// SyncAutoReplyStructuredCandidate 只登记有手机号的授权结构化结果，失败后由持久待办重试。
func (c *Client) SyncAutoReplyStructuredCandidate(ctx context.Context, credentials AgentCredentials, localCandidateID string, input AutoReplyCandidateInput) error {
	if !c.LocalAutoReplyEnabled() {
		return nil
	}
	if input.Phone == "" {
		return nil
	}
	safe := input
	var revision int64
	if err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		row, found, err := tx.Get("candidates", localCandidateID)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("本地简历尚未保存")
		}
		var candidate AutoReplyStoredCandidate
		if err = json.Unmarshal(row.Data, &candidate); err != nil {
			return err
		}
		conv, err := candidateContact(tx, localCandidateID)
		if err != nil {
			return err
		}
		if conv.DeletionBoundary != nil && (conv.PlatformID != input.PlatformID || conv.PlatformAccountID != input.PlatformAccountID || conv.DeletionBoundary.SourceHash != autoReplyDigest(input.PlatformID+"\x00"+input.PlatformCandidateID)) {
			return fmt.Errorf("新简历上传来源与本地会话不一致")
		}
		// 重试使用已保存的附件来源，避免相同结果因调用方省略内部字段而绕过发布退避。
		safe.ResumeSourceMessageID = candidate.ResumeSourceMessageID
		safe.ReopenProof, err = storedContactAttachmentProof(tx, conv, candidate.ResumeSourceMessageID)
		if err != nil {
			return err
		}
		if safe.ReopenProof != nil && c.localAutoStore.UsesAutoReplyAuthority() {
			safe.ReopenProof.ConversationID, safe.ReopenProof.LocalCandidateID = conv.ID, localCandidateID
		}
		revision = row.Revision
		safe.StructuredCandidate = structuredLocalAutoReplyCandidate(candidate)
		safe.Gender = candidate.Gender
		safe.BirthYMPrecision = candidate.BirthYMPrecision
		return nil
	}); err != nil {
		return err
	}
	if safe.Phone == "" {
		return nil
	}
	safe.RawText = ""
	safe.BasicInfo = ""
	safe.ColleagueCommunications = nil
	safe.AvatarURL = ""
	raw, err := json.Marshal(safe)
	if err != nil {
		return err
	}
	hash := autoReplyDigest(string(raw))
	id := "candidate:" + localCandidateID + ":" + input.PositionID
	if err = c.enqueueAutoReplyOutbox(ctx, credentials, id, "candidate", autoReplyCandidateUpload{LocalCandidateID: localCandidateID, Input: safe, Hash: hash, Revision: revision}); err != nil {
		return err
	}
	return c.processAutoReplyOutbox(ctx, credentials, id)
}

// CandidatePublicationStatus 核实本岗位上传任务和正式简历链接的真实结果，未确认时继续保留待办。
func (c *Client) CandidatePublicationStatus(ctx context.Context, credentials AgentCredentials, candidate, position string) (status string, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, ok, e := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", "candidate:"+candidate+":"+position)
		if e != nil || !ok {
			return e
		}
		status = item.Status
		if status == "completed" {
			link, ok, e := getAutoReplyEntity[localCloudLink](tx, "cloud_links", "candidate:"+candidate)
			if e != nil {
				return e
			}
			if !ok || link.Value == "" {
				status = "pending"
			}
		}
		return nil
	})
	return
}

// notifyLocalAutoReply 持久登记人工接管通知，失败状态与提示明确返回且不会持锁等待网络。
func (c *Client) notifyLocalAutoReply(ctx context.Context, credentials AgentCredentials, input AutoReplyNotification) (result AutoReplyNotificationResult, err error) {
	id := autoReplyNoticeID(input)
	result.Notification = input
	if err = c.enqueueAutoReplyOutbox(ctx, credentials, id, "notice", input); err != nil {
		return result, err
	}
	result.Queued = true
	readErr := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, _, e := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		result.EmailSent = item.Status == "completed"
		result.Warning = item.LastError
		return e
	})
	if err == nil {
		err = readErr
	}
	if !result.EmailSent && result.Warning == "" {
		result.Warning = "接管提醒还在等待发送，本机已记下并会继续重试"
	}
	return
}

// RetryAutoReplyOutbox 校验当前账号后恢复到期上传，不持久化或沿用历史登录凭证。
func (c *Client) RetryAutoReplyOutbox(ctx context.Context, credentials AgentCredentials) error {
	if _, err := c.ValidateSession(ctx, credentials.Token); err != nil {
		return err
	}
	items := []autoReplyOutboxItem{}
	err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		rows, err := tx.DueAutoReplyRows("outbox", 30)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var item autoReplyOutboxItem
			if err = json.Unmarshal(row.Data, &item); err != nil {
				return err
			}
			if item.Kind != "report" {
				items = append(items, item)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var first error
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = c.processAutoReplyOutbox(ctx, credentials, item.ID); err != nil && first == nil {
			first = err
		}
	}
	return errors.Join(first, c.retryPurgedEventFiles(ctx, credentials))
}

// processAutoReplyOutbox 使用短租约领取单项任务，成功或失败分别持久记录，失败按退避时间恢复。
func (c *Client) processAutoReplyOutbox(ctx context.Context, credentials AgentCredentials, id string) error {
	var item autoReplyOutboxItem
	claimed := false
	now := time.Now().UTC()
	err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		var exists bool
		var err error
		item, exists, err = getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		if err != nil || !exists {
			return err
		}
		if item.Status == "completed" || item.NextAttemptAt.After(now) || item.LeaseUntil.After(now) {
			return nil
		}
		item.Status = "running"
		item.LeaseUntil = now.Add(3 * time.Minute)
		item.Attempts++
		claimed = true
		return tx.Put("outbox", id, item)
	})
	if err != nil || !claimed {
		return err
	}
	sendErr := c.sendAutoReplyOutbox(ctx, credentials, item)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = c.autoReplyTransaction(finishCtx, credentials, func(tx *storage.AutoReplyTx) error {
		current, found, readErr := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		if readErr != nil {
			return readErr
		}
		if !found {
			// 删除事件可能在网络请求期间清空本行，迟到响应不能重新建回含简历的上传任务。
			return nil
		}
		if !bytes.Equal(current.Payload, item.Payload) {
			current.LeaseUntil = time.Time{}
			current.Status = "pending"
			current.NextAttemptAt = time.Time{}
			return tx.Put("outbox", id, current)
		}
		item.LeaseUntil = time.Time{}
		if sendErr == nil {
			item.Status = "completed"
			item.LastError = ""
		} else {
			item.Status = "failed"
			item.LastError = sendErr.Error()
			delay := time.Duration(min(item.Attempts, 30)) * time.Minute
			item.NextAttemptAt = time.Now().UTC().Add(delay)
		}
		return tx.Put("outbox", id, item)
	})
	if sendErr != nil {
		return sendErr
	}
	return err
}

// sendAutoReplyOutbox 按待办类型发布授权资料，聊天原文保留在当前账号的业务仓储。
func (c *Client) sendAutoReplyOutbox(ctx context.Context, credentials AgentCredentials, item autoReplyOutboxItem) error {
	switch item.Kind {
	case "notice":
		var input AutoReplyNotification
		if err := json.Unmarshal(item.Payload, &input); err != nil {
			return err
		}
		raw, err := json.Marshal(noticeCloudRequest(item.ID, input))
		if err != nil {
			return err
		}
		var result AutoReplyNotificationResult
		if err = c.doBody(ctx, http.MethodPost, autoReplyAgentBasePath+"/notifications", credentials.Token, credentials.MachineID, "application/json", bytes.NewReader(raw), &result); err != nil {
			return err
		}
		if !result.EmailSent {
			if result.Warning != "" {
				return fmt.Errorf("%s", result.Warning)
			}
			return fmt.Errorf("接管邮件还没发送成功，已保留待办，稍后重试")
		}
		return nil
	case "candidate":
		var input autoReplyCandidateUpload
		if err := json.Unmarshal(item.Payload, &input); err != nil {
			return err
		}
		request := candidateCloudRequest(input.Input)
		if c.localAutoStore.UsesAutoReplyAuthority() {
			request.PlatformAccountID = AutoReplyAccountKey(request.PlatformID, request.PlatformAccountID)
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		var result AutoReplyCandidateSaveResult
		if err = c.doBody(ctx, http.MethodPost, autoReplyAgentBasePath+"/candidates", credentials.Token, credentials.MachineID, "application/json", bytes.NewReader(raw), &result); err != nil {
			var apiErr *APIError
			if IsCandidateDeleted(err) || errors.As(err, &apiErr) && apiErr.Code == "CANDIDATE_REOPEN_PROOF_CONFLICT" {
				// 发布被拒绝不等于当前代已删除。只有独立删除事件确切封印本候选编号时才将迟到的任务视为已结束。
				return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
					gone, e := tx.HasPurgedKey("candidates", input.LocalCandidateID)
					if e != nil {
						return e
					}
					if gone {
						return nil
					}
					return errors.New("云端删除记录与本次建档凭据尚未对齐，已保留沟通、附件和简历同步待办")
				})
			}
			return err
		}
		if result.CandidateID == "" {
			return fmt.Errorf("云端没有确认结构化简历保存结果")
		}
		linkErr := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
			return tx.Put("cloud_links", "candidate:"+input.LocalCandidateID, localCloudLink{Value: result.CandidateID})
		})
		if errors.Is(linkErr, storage.ErrAutoReplyCandidateDeleted) {
			if input.Input.ReopenProof != nil {
				// 新一代绝不重新挂到已封印的旧云记录。保留本代原件和待办，由云端登记真正的新归属。
				return errors.New("简历库回执指向已删除的旧记录，当前新资料已保留，等待核对后重试")
			}
			// 成功回执给出了精确的云 ID，再核对独立登记的删除事件。不能用通用写入拒绝代替确认。
			var confirmed bool
			if e := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
				var e error
				confirmed, e = tx.HasPurgedKey("cloud", result.CandidateID)
				return e
			}); e != nil {
				return e
			}
			if confirmed {
				return c.purgeAutoReplyCandidate(ctx, credentials, result.CandidateID, nil, input.LocalCandidateID)
			}
			return linkErr
		}
		return linkErr
	case "report":
		var input autoReplyReportUpload
		if err := json.Unmarshal(item.Payload, &input); err != nil {
			return err
		}
		var cloudID string
		err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
			link, _, err := getAutoReplyEntity[localCloudLink](tx, "cloud_links", "candidate:"+input.LocalCandidateID)
			cloudID = link.Value
			return err
		})
		if err != nil {
			return err
		}
		if cloudID == "" {
			return fmt.Errorf("结构化简历仍在同步，报告已保存")
		}
		report := input.Report
		report.Candidate.ID = cloudID
		report.Candidate.BasicInfo = ""
		report.Candidate.AvatarURL = ""
		for i := range report.Conditions {
			report.Conditions[i].Evidence = nil
		}
		payload := struct {
			PositionID  string                        `json:"position_id"`
			CandidateID string                        `json:"candidate_id"`
			InputHash   string                        `json:"input_hash"`
			Report      AutoReplyRecommendationReport `json:"report_data"`
			Model       string                        `json:"model"`
			TokenUsage  int                           `json:"token_usage"`
		}{report.Position.ID, cloudID, input.Key, report, input.Model, 0}
		var result struct {
			OK bool `json:"ok"`
		}
		if err = c.doWithMachineID(ctx, http.MethodPost, autoReplyAgentBasePath+"/recommendations/completed", credentials.Token, credentials.MachineID, payload, &result); err != nil {
			return err
		}
		if !result.OK {
			return fmt.Errorf("云端尚未确认推荐报告")
		}
		return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
			state, _, err := getAutoReplyEntity[localReportState](tx, "reports", input.Key)
			if err != nil {
				return err
			}
			state.Published = true
			return tx.Put("reports", input.Key, state)
		})
	default:
		return fmt.Errorf("未知本地待办类型：%s", item.Kind)
	}
}
