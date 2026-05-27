// 本文件负责定义云端 Agent 机器绑定的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// AgentBinding 表示一个云端账号和一台本地机器的绑定关系。
type AgentBinding struct {
	UserEmail    string
	MachineID    string
	AgentVersion string
	LocalPort    int
	PublicKey    string
	BindStatus   string
	LastSeenAt   time.Time
	CreatedAt    time.Time
}

// AgentStore 定义机器绑定记录的持久化能力。
type AgentStore interface {
	SaveBinding(binding AgentBinding) (AgentBinding, error)
	CurrentBinding(userEmail string) (AgentBinding, error)
}

// MemoryAgentStore 提供开发期使用的内存机器绑定存储。
type MemoryAgentStore struct {
	mu       sync.Mutex
	bindings map[string]AgentBinding
	now      func() time.Time
}

// NewMemoryAgentStore 创建开发期内存机器绑定存储。
func NewMemoryAgentStore() *MemoryAgentStore {
	return &MemoryAgentStore{
		bindings: make(map[string]AgentBinding),
		now:      time.Now,
	}
}

// SaveBinding 保存或更新当前用户的机器绑定记录。
func (s *MemoryAgentStore) SaveBinding(binding AgentBinding) (AgentBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.LastSeenAt = now
	if binding.BindStatus == "" {
		binding.BindStatus = "active"
	}

	s.bindings[binding.UserEmail] = binding
	return binding, nil
}

// CurrentBinding 读取当前用户最近绑定的一台本地机器。
func (s *MemoryAgentStore) CurrentBinding(userEmail string) (AgentBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[userEmail]
	if !ok {
		return AgentBinding{}, ErrNotFound
	}
	return binding, nil
}
