// 本文件负责定义云端 Agent 机器绑定的数据模型和存储接口。
package httpapi

import (
	"errors"
	"sync"
	"time"
)

var ErrAgentAlreadyBound = errors.New("agent already bound to another machine")

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
	DisableBindings(userEmail string) error
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

	current, ok := s.bindings[binding.UserEmail]
	if ok && current.BindStatus == "active" && current.MachineID != "" && current.MachineID != binding.MachineID {
		return AgentBinding{}, ErrAgentAlreadyBound
	}

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
	if !ok || binding.BindStatus != "active" {
		return AgentBinding{}, ErrNotFound
	}
	return binding, nil
}

// DisableBindings 解除当前用户所有本地机器绑定。
func (s *MemoryAgentStore) DisableBindings(userEmail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[userEmail]
	if !ok {
		return nil
	}
	binding.BindStatus = "disabled"
	binding.LastSeenAt = s.now()
	s.bindings[userEmail] = binding
	return nil
}
