// Package cloud 本文件按报告实体保存生成租约、结果与发布状态，并支持当前账号恢复后台任务。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"time"
)

// ErrAutoReplyReportRetriesExhausted 表示报告生成已达到自动重试上限，需要人工决定重试。
var ErrAutoReplyReportRetriesExhausted = errors.New("推荐报告已尝试三次，请人工确认后重试")

// localReportState 保存一份报告的生成与发布收据，不重复嵌入其他候选人的资料。
type localReportState struct {
	Report    json.RawMessage `json:"report,omitempty"`
	Attempts  int             `json:"attempts"`
	LeaseAt   time.Time       `json:"lease_at"`
	Published bool            `json:"published"`
}

// AutoReplyRecommendationJob 保存可恢复的推荐任务，不包含Token或模型密钥。
type AutoReplyRecommendationJob struct {
	ID                string                        `json:"id"`
	InputHash         string                        `json:"input_hash"`
	CandidateID       string                        `json:"candidate_id"`
	ConversationID    string                        `json:"conversation_id"`
	BasedOnMessageKey string                        `json:"based_on_message_key"`
	ConfigVersion     int                           `json:"config_version"`
	Conversation      AutoReplyConversation         `json:"conversation"`
	Position          AutoReplyPositionSnapshot     `json:"position"`
	Candidate         AutoReplyStoredCandidate      `json:"candidate"`
	Source            AutoReplyRecommendationReport `json:"source"`
	Model             string                        `json:"model"`
	Status            string                        `json:"status"`
	Attempts          int                           `json:"attempts"`
	LastError         string                        `json:"last_error"`
	NextAttemptAt     time.Time                     `json:"next_attempt_at"`
	LeaseUntil        time.Time                     `json:"lease_until"`
}

// ClaimAutoReplyReport 原子领取报告生成租约，完成结果可重用且失败最多自动尝试三次。
func (c *Client) ClaimAutoReplyReport(ctx context.Context, credentials AgentCredentials, key string) (report json.RawMessage, claimed bool, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		state, _, err := getAutoReplyEntity[localReportState](tx, "reports", key)
		if err != nil {
			return err
		}
		if len(state.Report) > 0 {
			report = state.Report
			return nil
		}
		if !state.LeaseAt.IsZero() && time.Since(state.LeaseAt) < 5*time.Minute {
			return nil
		}
		if state.Attempts >= 3 {
			return ErrAutoReplyReportRetriesExhausted
		}
		state.Attempts++
		state.LeaseAt = time.Now().UTC()
		claimed = true
		return tx.Put("reports", key, state)
	})
	return
}

// SaveAutoReplyReport 保存本机生成结果或释放失败租约；写入仅影响一份报告。
func (c *Client) SaveAutoReplyReport(ctx context.Context, credentials AgentCredentials, key string, report *AutoReplyRecommendationReport) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		state, _, err := getAutoReplyEntity[localReportState](tx, "reports", key)
		if err != nil {
			return err
		}
		state.LeaseAt = time.Time{}
		if report != nil {
			state.Report, err = json.Marshal(report)
			if err != nil {
				return err
			}
		}
		return tx.Put("reports", key, state)
	})
}

// PublishAutoReplyReport 先登记发布待办再尝试上传，失败与重启后均可恢复。
func (c *Client) PublishAutoReplyReport(ctx context.Context, credentials AgentCredentials, localCandidateID, key, model string, report AutoReplyRecommendationReport) error {
	id := "report:" + key
	if err := c.enqueueAutoReplyOutbox(ctx, credentials, id, "report", autoReplyReportUpload{LocalCandidateID: localCandidateID, Key: key, Model: model, Report: report}); err != nil {
		return err
	}
	if err := c.processAutoReplyOutbox(ctx, credentials, id); err != nil {
		return err
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, _, err := getAutoReplyEntity[autoReplyOutboxItem](tx, "outbox", id)
		if err != nil {
			return err
		}
		if item.Status != "completed" {
			return fmt.Errorf("推荐报告已保留，等待下一次发布重试")
		}
		return nil
	})
}

// EnqueueAutoReplyRecommendation 幂等登记本机推荐任务；已完成任务不会因轮询重新生成。
func (c *Client) EnqueueAutoReplyRecommendation(ctx context.Context, credentials AgentCredentials, job AutoReplyRecommendationJob) error {
	if job.ID == "" {
		job.ID = job.InputHash
	}
	if job.ID == "" || job.ConversationID == "" || job.CandidateID == "" {
		return fmt.Errorf("推荐待办缺少候选人或会话编号")
	}
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		_, exists, err := tx.Get("recommendations", job.ID)
		if err != nil || exists {
			return err
		}
		job.Status = "pending"
		return tx.Put("recommendations", job.ID, job)
	})
}

// PendingAutoReplyRecommendations 返回当前账号已到期或租约失效的推荐待办。
func (c *Client) PendingAutoReplyRecommendations(ctx context.Context, credentials AgentCredentials) (items []AutoReplyRecommendationJob, err error) {
	items = []AutoReplyRecommendationJob{}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		rows, err := tx.DueAutoReplyRows("recommendations", 30)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var job AutoReplyRecommendationJob
			if err = json.Unmarshal(row.Data, &job); err != nil {
				return err
			}
			items = append(items, job)
		}
		return nil
	})
	return
}

// ClaimAutoReplyRecommendation 用数据库事务领取单份推荐任务，不会跨进程重复领取有效租约。
func (c *Client) ClaimAutoReplyRecommendation(ctx context.Context, credentials AgentCredentials, id string) (claimed bool, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		job, found, err := getAutoReplyEntity[AutoReplyRecommendationJob](tx, "recommendations", id)
		if err != nil || !found {
			return err
		}
		if (job.Status == "completed" || job.Status == "blocked") || job.NextAttemptAt.After(time.Now()) || job.LeaseUntil.After(time.Now()) {
			return nil
		}
		job.Status = "running"
		job.Attempts++
		job.LeaseUntil = time.Now().UTC().Add(5 * time.Minute)
		claimed = true
		return tx.Put("recommendations", id, job)
	})
	return
}

// FinishAutoReplyRecommendation 持久记录完成结果或下一次自动重试，保留错误供人工查看。
func (c *Client) FinishAutoReplyRecommendation(ctx context.Context, credentials AgentCredentials, id string, completed bool, lastError string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		job, found, err := getAutoReplyEntity[AutoReplyRecommendationJob](tx, "recommendations", id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("推荐待办没有找到")
		}
		job.LeaseUntil = time.Time{}
		job.LastError = lastError
		if completed {
			job.Status = "completed"
		} else {
			job.Status = "failed"
			job.NextAttemptAt = time.Now().UTC().Add(time.Duration(min(max(job.Attempts, 1), 15)) * time.Minute)
		}
		return tx.Put("recommendations", id, job)
	})
}

// BlockAutoReplyRecommendation 保留需要人工处理的失败任务，避免自动无限空转。
func (c *Client) BlockAutoReplyRecommendation(ctx context.Context, credentials AgentCredentials, id, reason string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		job, found, err := getAutoReplyEntity[AutoReplyRecommendationJob](tx, "recommendations", id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("推荐待办没有找到")
		}
		job.Status = "blocked"
		job.LastError = reason
		job.LeaseUntil = time.Time{}
		return tx.Put("recommendations", id, job)
	})
}

// ReadAutoReplyReport 仅读取已经完成的报告，读取缓存不领取租约或消耗模型重试次数。
func (c *Client) ReadAutoReplyReport(ctx context.Context, credentials AgentCredentials, key string) (report json.RawMessage, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		item, _, err := getAutoReplyEntity[localReportState](tx, "reports", key)
		report = item.Report
		return err
	})
	return
}

// CountBlockedAutoReplyRecommendations 统计当前登录账号等待人工重试的报告任务。
func (c *Client) CountBlockedAutoReplyRecommendations(ctx context.Context, credentials AgentCredentials) (count int, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		var queryErr error
		count, queryErr = tx.CountBlockedRecommendations()
		return queryErr
	})
	return
}

// RetryBlockedAutoReplyRecommendations 原子重启当前账号明确阻塞的推荐，不改已生成报告或发送记录。
func (c *Client) RetryBlockedAutoReplyRecommendations(ctx context.Context, credentials AgentCredentials) (count int, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		jobs, err := listAutoReplyEntities[AutoReplyRecommendationJob](tx, "recommendations", autoReplyFilter("status", "blocked"))
		if err != nil {
			return err
		}
		for _, job := range jobs {
			state, found, err := getAutoReplyEntity[localReportState](tx, "reports", job.InputHash)
			if err != nil {
				return err
			}
			if found && len(state.Report) == 0 && !state.Published {
				// 同一份事实可能被另一任务生成；人工重试不能撤销仍然有效的生成租约。
				if !state.LeaseAt.IsZero() && time.Since(state.LeaseAt) < 5*time.Minute {
					continue
				}
				state.Attempts, state.LeaseAt = 0, time.Time{}
				if err = tx.Put("reports", job.InputHash, state); err != nil {
					return err
				}
			}
			job.Status, job.Attempts, job.LastError = "pending", 0, ""
			job.NextAttemptAt, job.LeaseUntil = time.Time{}, time.Time{}
			if err = tx.Put("recommendations", job.ID, job); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		count = 0
	}
	return
}
