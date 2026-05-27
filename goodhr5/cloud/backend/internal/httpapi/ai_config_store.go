// 本文件负责定义云端用户 AI 配置的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// AIConfig 表示一套可用于任务筛选的 AI 配置。
type AIConfig struct {
	BaseURL        string
	Model          string
	APIKey         string
	Temperature    float64
	PromptTemplate string
	Enabled        bool
	UpdatedAt      time.Time
}

// AIConfigStore 定义用户自定义 AI 配置的存储能力。
type AIConfigStore interface {
	UserConfig(userEmail string) (AIConfig, error)
	SaveUserConfig(userEmail string, config AIConfig) (AIConfig, error)
}

// MemoryAIConfigStore 提供开发期使用的内存 AI 配置存储。
type MemoryAIConfigStore struct {
	mu      sync.Mutex
	users   map[string]AIConfig
	now     func() time.Time
	started time.Time
}

// NewMemoryAIConfigStore 创建开发期内存 AI 配置存储。
func NewMemoryAIConfigStore() *MemoryAIConfigStore {
	now := time.Now()
	return &MemoryAIConfigStore{
		users:   make(map[string]AIConfig),
		now:     time.Now,
		started: now,
	}
}

// UserConfig 读取指定用户的自定义 AI 配置。
func (s *MemoryAIConfigStore) UserConfig(userEmail string) (AIConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config, ok := s.users[userEmail]
	if !ok {
		return AIConfig{}, ErrNotFound
	}
	return config, nil
}

// SaveUserConfig 保存指定用户的自定义 AI 配置。
func (s *MemoryAIConfigStore) SaveUserConfig(userEmail string, config AIConfig) (AIConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config.UpdatedAt = s.now()
	s.users[userEmail] = config
	return config, nil
}
