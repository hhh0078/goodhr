// Package cloud 本文件持久保存通知及授权结果上传待办，网络请求始终在SQLite事务外执行。
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

// sendAutoReplyOutbox 根据明确待办类型执行云端请求，所有敏感正文仅保留本机。
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
			return fmt.Errorf("接管邮件还没发送成功，已保留本机重试")
		}
		return nil
	case "candidate":
		var input autoReplyCandidateUpload
		if err := json.Unmarshal(item.Payload, &input); err != nil {
			return err
		}
		raw, err := json.Marshal(candidateCloudRequest(input.Input))
		if err != nil {
			return err
		}
		var result AutoReplyCandidateSaveResult
		if err = c.doBody(ctx, http.MethodPost, autoReplyAgentBasePath+"/candidates", credentials.Token, credentials.MachineID, "application/json", bytes.NewReader(raw), &result); err != nil {
			if IsCandidateDeleted(err) {
				// 云端按稳定来源拒绝了本次具体上传，即使还没有取得云ID，也能准确清理这个本地候选人。
				return c.purgeAutoReplyCandidate(ctx, credentials, "", nil, input.LocalCandidateID)
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
			// 删除事件早于首次上传回执时，同一份请求给出了精确映射，补清其本地资料。
			return c.purgeAutoReplyCandidate(ctx, credentials, result.CandidateID, nil, input.LocalCandidateID)
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
			return fmt.Errorf("结构化简历仍在同步，报告已保存在本机")
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
