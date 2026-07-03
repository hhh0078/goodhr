// 本文件负责超管邮件发送记录的内存存储实现。
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type EmailBatch struct {
	ID             string     `json:"id"`
	Subject        string     `json:"subject"`
	TargetSummary  string     `json:"target_summary"`
	SourceKey      string     `json:"source_key"`
	CreatedByEmail string     `json:"created_by_email"`
	TotalCount     int        `json:"total_count"`
	SentCount      int        `json:"sent_count"`
	FailedCount    int        `json:"failed_count"`
	OpenedCount    int        `json:"opened_count"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type EmailRecipient struct {
	ID           string     `json:"id"`
	BatchID      string     `json:"batch_id"`
	Email        string     `json:"email"`
	Status       string     `json:"status"`
	ErrorMessage string     `json:"error_message"`
	Opened       bool       `json:"opened"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	SentAt       *time.Time `json:"sent_at,omitempty"`
}

type EmailTargetFilter struct {
	Mode                string
	Emails              []string
	Tags                []string
	FlowSteps           []string
	CreatedDay          string
	LastLoginBeforeDays int
	LastLoginExactDays  int
}

type EmailTargetUser struct {
	Email string
	Flow  AdminUserFlow
	Tags  []string
}

type EmailCampaignStore interface {
	CreateBatch(subject string, targetSummary string, sourceKey string, createdBy string, emails []string) (EmailBatch, []EmailRecipient, error)
	GetBatch(id string) (EmailBatch, []EmailRecipient, error)
	ListBatches(limit int) ([]EmailBatch, error)
	MarkRecipientSent(id string) error
	MarkRecipientFailed(id string, message string) error
	MarkRecipientOpened(id string) error
	FindTargetUsers(filter EmailTargetFilter) ([]EmailTargetUser, error)
	SourceKeyExists(sourceKey string) (bool, error)
}

type MemoryEmailCampaignStore struct {
	mu         sync.Mutex
	batches    map[string]EmailBatch
	recipients map[string]EmailRecipient
}

// NewMemoryEmailCampaignStore 创建内存邮件记录存储。
func NewMemoryEmailCampaignStore() *MemoryEmailCampaignStore {
	return &MemoryEmailCampaignStore{batches: map[string]EmailBatch{}, recipients: map[string]EmailRecipient{}}
}

// CreateBatch 创建邮件批次和收件人记录。
func (s *MemoryEmailCampaignStore) CreateBatch(subject string, targetSummary string, sourceKey string, createdBy string, emails []string) (EmailBatch, []EmailRecipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sourceKey != "" {
		for _, batch := range s.batches {
			if batch.SourceKey == sourceKey {
				return EmailBatch{}, nil, ErrAlreadyExists
			}
		}
	}
	now := time.Now()
	batch := EmailBatch{ID: randomMailID(), Subject: subject, TargetSummary: targetSummary, SourceKey: sourceKey, CreatedByEmail: createdBy, TotalCount: len(emails), CreatedAt: now}
	s.batches[batch.ID] = batch
	recipients := make([]EmailRecipient, 0, len(emails))
	for _, email := range emails {
		item := EmailRecipient{ID: randomMailID(), BatchID: batch.ID, Email: email, Status: "pending", CreatedAt: now}
		s.recipients[item.ID] = item
		recipients = append(recipients, item)
	}
	return batch, recipients, nil
}

// GetBatch 读取邮件批次和收件人记录。
func (s *MemoryEmailCampaignStore) GetBatch(id string) (EmailBatch, []EmailRecipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[id]
	if !ok {
		return EmailBatch{}, nil, ErrNotFound
	}
	recipients := []EmailRecipient{}
	for _, item := range s.recipients {
		if item.BatchID == id {
			recipients = append(recipients, item)
		}
	}
	sort.Slice(recipients, func(i, j int) bool { return recipients[i].CreatedAt.Before(recipients[j].CreatedAt) })
	return batch, recipients, nil
}

// ListBatches 返回最近邮件批次。
func (s *MemoryEmailCampaignStore) ListBatches(limit int) ([]EmailBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]EmailBatch, 0, len(s.batches))
	for _, item := range s.batches {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// MarkRecipientSent 标记收件人发送成功。
func (s *MemoryEmailCampaignStore) MarkRecipientSent(id string) error {
	return s.updateRecipient(id, func(item *EmailRecipient) { now := time.Now(); item.Status = "sent"; item.SentAt = &now })
}

// MarkRecipientFailed 标记收件人发送失败。
func (s *MemoryEmailCampaignStore) MarkRecipientFailed(id string, message string) error {
	return s.updateRecipient(id, func(item *EmailRecipient) { item.Status = "failed"; item.ErrorMessage = strings.TrimSpace(message) })
}

// MarkRecipientOpened 标记收件人已打开追踪图片。
func (s *MemoryEmailCampaignStore) MarkRecipientOpened(id string) error {
	return s.updateRecipient(id, func(item *EmailRecipient) { now := time.Now(); item.Opened = true; item.OpenedAt = &now })
}

// FindTargetUsers 内存实现暂不维护完整用户画像，返回空列表。
func (s *MemoryEmailCampaignStore) FindTargetUsers(EmailTargetFilter) ([]EmailTargetUser, error) {
	return []EmailTargetUser{}, nil
}

// SourceKeyExists 判断自动任务幂等键是否已存在。
func (s *MemoryEmailCampaignStore) SourceKeyExists(sourceKey string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range s.batches {
		if batch.SourceKey == sourceKey {
			return true, nil
		}
	}
	return false, nil
}

// updateRecipient 更新收件人记录并重算批次统计。
func (s *MemoryEmailCampaignStore) updateRecipient(id string, fn func(*EmailRecipient)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.recipients[id]
	if !ok {
		return ErrNotFound
	}
	fn(&item)
	s.recipients[id] = item
	s.recountBatch(item.BatchID)
	return nil
}

// recountBatch 重算邮件批次统计。
func (s *MemoryEmailCampaignStore) recountBatch(batchID string) {
	batch := s.batches[batchID]
	batch.SentCount, batch.FailedCount, batch.OpenedCount = 0, 0, 0
	done := 0
	for _, item := range s.recipients {
		if item.BatchID != batchID {
			continue
		}
		if item.Status == "sent" {
			batch.SentCount++
			done++
		}
		if item.Status == "failed" {
			batch.FailedCount++
			done++
		}
		if item.Opened {
			batch.OpenedCount++
		}
	}
	if done >= batch.TotalCount {
		now := time.Now()
		batch.FinishedAt = &now
	}
	s.batches[batchID] = batch
}

var ErrAlreadyExists = errors.New("already exists")

// randomMailID 生成内存存储使用的随机 ID。
func randomMailID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(buf)
}
