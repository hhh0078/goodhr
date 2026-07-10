// 本文件负责定义云端 Agent 本地程序连接记录的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// AgentBinding 表示一个云端账号和一台本地程序的连接记录。
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

// AgentStore 定义本地程序连接记录的持久化能力。
type AgentStore interface {
	SaveBinding(binding AgentBinding) (AgentBinding, error)
	CurrentBinding(userEmail string) (AgentBinding, error)
	DisableBindings(userEmail string) error
	ActiveBindingCount() (int, error)
}

// MemoryAgentStore 提供开发期使用的内存连接记录存储。
type MemoryAgentStore struct {
	mu       sync.Mutex
	bindings map[string]AgentBinding
	now      func() time.Time
}

// NewMemoryAgentStore 创建开发期内存连接记录存储。
func NewMemoryAgentStore() *MemoryAgentStore {
	return &MemoryAgentStore{
		bindings: make(map[string]AgentBinding),
		now:      time.Now,
	}
}

// SaveBinding 保存或更新当前用户的本地程序连接记录。
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

// CurrentBinding 读取当前用户最近连接的一台本地机器。
func (s *MemoryAgentStore) CurrentBinding(userEmail string) (AgentBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[userEmail]
	if !ok || binding.BindStatus != "active" {
		return AgentBinding{}, ErrNotFound
	}
	return binding, nil
}

// DisableBindings 清理当前用户所有本地程序连接记录。
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

// ActiveBindingCount 统计当前有效绑定数量。
// 返回 bind_status 为 active 的内存绑定数量。
func (s *MemoryAgentStore) ActiveBindingCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, binding := range s.bindings {
		if binding.BindStatus == "active" {
			count++
		}
	}
	return count, nil
}
