// 文件作用：定义账号私有消息合同，并提供内存存储、幂等推送、首次已读与72小时清理。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrMessageIdempotency = errors.New("这条消息编号已用于其他内容，请重新检查")

// UserMessage 表示纯文本消息，列表响应省略正文。
type UserMessage struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Type      string     `json:"type"`
	ActionURL string     `json:"action_url,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
}

// MessageInput 表示推送给指定账号的业务消息，不允许客户端指定收件人。
type MessageInput struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	Type           string `json:"type,omitempty"`
	ActionURL      string `json:"action_url,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

// MessageSummary 是用于红点轮询的轻量计数与递增版本。
type MessageSummary struct {
	UnreadCount int64 `json:"unread_count"`
	Version     int64 `json:"version"`
}

// MessagePushResult 返回首次创建或幂等重试的原消息与最新计数。
type MessagePushResult struct {
	Message UserMessage    `json:"message"`
	Created bool           `json:"created"`
	Summary MessageSummary `json:"summary"`
}

// MessageList 只返回消息标题、时间和已读状态，正文需单独展开。
type MessageList struct {
	Messages []UserMessage `json:"messages"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

// MessageUserSummary 是后台清理后需要更新缓存的账号快照。
type MessageUserSummary struct {
	UserEmail string
	Summary   MessageSummary
}

// MessageStore 提供可复用消息推送与账号隔离的数据访问。
type MessageStore interface {
	Push(userEmail string, input MessageInput) (MessagePushResult, error)
	Summary(userEmail string) (MessageSummary, error)
	List(userEmail string, page, pageSize int) (MessageList, error)
	Read(userEmail, messageID string) (UserMessage, MessageSummary, error)
	CleanupRead(cutoff time.Time) ([]MessageUserSummary, error)
}

type messageReceipt struct{ fingerprint, messageID string }

// MemoryMessageStore 用于未配置数据库的开发环境，所有变化受互斥锁保护。
type MemoryMessageStore struct {
	mu        sync.Mutex
	messages  map[string]map[string]UserMessage
	receipts  map[string]map[string]messageReceipt
	summaries map[string]MessageSummary
	now       func() time.Time
}

// NewMemoryMessageStore 创建无外部依赖的消息存储。
func NewMemoryMessageStore() *MemoryMessageStore {
	return &MemoryMessageStore{messages: map[string]map[string]UserMessage{}, receipts: map[string]map[string]messageReceipt{}, summaries: map[string]MessageSummary{}, now: time.Now}
}

// messageFingerprint 固定序列化业务字段，防止相同幂等编号提交不同内容。
func messageFingerprint(input MessageInput) string {
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// messagePagination 规范分页范围，避免过大的查询和负偏移。
func messagePagination(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	if page > 1000000 {
		page = 1000000
	}
	return page, size
}

// Push 创建一次消息并原子增加未读数量；重试不会重复计数。
func (s *MemoryMessageStore) Push(email string, input MessageInput) (MessagePushResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	fingerprint := messageFingerprint(input)
	if receipt, exists := s.receipts[email][input.IdempotencyKey]; exists {
		if receipt.fingerprint != fingerprint {
			return MessagePushResult{}, ErrMessageIdempotency
		}
		message, found := s.messages[email][receipt.messageID]
		if !found {
			message.ID = receipt.messageID
		}
		return MessagePushResult{Message: message, Summary: s.summaries[email]}, nil
	}
	if s.messages[email] == nil {
		s.messages[email] = map[string]UserMessage{}
	}
	if s.receipts[email] == nil {
		s.receipts[email] = map[string]messageReceipt{}
	}
	message := UserMessage{ID: randomMailID(), Title: input.Title, Body: input.Body, Type: input.Type, ActionURL: input.ActionURL, CreatedAt: s.now()}
	s.messages[email][message.ID] = message
	s.receipts[email][input.IdempotencyKey] = messageReceipt{fingerprint, message.ID}
	summary := s.summaries[email]
	summary.UnreadCount++
	summary.Version++
	s.summaries[email] = summary
	return MessagePushResult{Message: message, Created: true, Summary: summary}, nil
}

// Summary 返回该账号的轻量状态，不访问正文。
func (s *MemoryMessageStore) Summary(email string) (MessageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summaries[strings.ToLower(strings.TrimSpace(email))], nil
}

// List 返回账号私有标题列表，不暴露消息正文。
func (s *MemoryMessageStore) List(email string, page, size int) (MessageList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page, size = messagePagination(page, size)
	result := MessageList{Messages: []UserMessage{}, Page: page, PageSize: size}
	for _, message := range s.messages[strings.ToLower(strings.TrimSpace(email))] {
		message.Body = ""
		result.Messages = append(result.Messages, message)
	}
	sort.Slice(result.Messages, func(i, j int) bool {
		if result.Messages[i].CreatedAt.Equal(result.Messages[j].CreatedAt) {
			return result.Messages[i].ID > result.Messages[j].ID
		}
		return result.Messages[i].CreatedAt.After(result.Messages[j].CreatedAt)
	})
	result.Total = len(result.Messages)
	start := (page - 1) * size
	if start >= result.Total {
		result.Messages = []UserMessage{}
		return result, nil
	}
	end := start + size
	if end > result.Total {
		end = result.Total
	}
	result.Messages = result.Messages[start:end]
	return result, nil
}

// Read 仅允许收件账号读取正文，首次展开才减少未读数量。
func (s *MemoryMessageStore) Read(email, id string) (UserMessage, MessageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	message, found := s.messages[email][id]
	if !found {
		return UserMessage{}, MessageSummary{}, ErrNotFound
	}
	summary := s.summaries[email]
	if message.ReadAt == nil {
		now := s.now()
		message.ReadAt = &now
		s.messages[email][id] = message
		summary.UnreadCount--
		summary.Version++
		s.summaries[email] = summary
	}
	return message, summary, nil
}

// CleanupRead 删除已读时间不晚于截止时间的正文，未读消息和幂等凭据始终保留。
func (s *MemoryMessageStore) CleanupRead(cutoff time.Time) ([]MessageUserSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := []MessageUserSummary{}
	for email, messages := range s.messages {
		deleted := false
		for id, message := range messages {
			if message.ReadAt != nil && !message.ReadAt.After(cutoff) {
				delete(messages, id)
				deleted = true
			}
		}
		if deleted {
			summary := s.summaries[email]
			summary.Version++
			s.summaries[email] = summary
			changed = append(changed, MessageUserSummary{email, summary})
		}
	}
	return changed, nil
}
