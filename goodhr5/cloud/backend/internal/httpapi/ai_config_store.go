// 本文件负责定义云端 AI 配置的数据模型和存储接口。
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

// AIConfigStore 定义系统默认配置和用户自定义配置的存储能力。
type AIConfigStore interface {
	SystemConfig() (AIConfig, error)
	SaveSystemConfig(config AIConfig) (AIConfig, error)
	UserConfig(userEmail string) (AIConfig, error)
	SaveUserConfig(userEmail string, config AIConfig) (AIConfig, error)
}

// MemoryAIConfigStore 提供开发期使用的内存 AI 配置存储。
type MemoryAIConfigStore struct {
	mu      sync.Mutex
	system  AIConfig
	users   map[string]AIConfig
	now     func() time.Time
	started time.Time
}

// NewMemoryAIConfigStore 创建开发期内存 AI 配置存储。
func NewMemoryAIConfigStore() *MemoryAIConfigStore {
	now := time.Now()
	return &MemoryAIConfigStore{
		system: AIConfig{
			BaseURL:        "https://api.siliconflow.cn/v1",
			Model:          "default-model",
			Temperature:    0.20,
			PromptTemplate: "",
			Enabled:        true,
			UpdatedAt:      now,
		},
		users:   make(map[string]AIConfig),
		now:     time.Now,
		started: now,
	}
}

// SystemConfig 读取系统默认 AI 配置。
func (s *MemoryAIConfigStore) SystemConfig() (AIConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.system, nil
}

// SaveSystemConfig 保存系统默认 AI 配置。
func (s *MemoryAIConfigStore) SaveSystemConfig(config AIConfig) (AIConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config.UpdatedAt = s.now()
	s.system = config
	return config, nil
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

// EffectiveAIConfig 合并系统默认配置和用户自定义配置。
func EffectiveAIConfig(system AIConfig, user AIConfig) AIConfig {
	effective := system

	if user.BaseURL != "" {
		effective.BaseURL = user.BaseURL
	}
	if user.Model != "" {
		effective.Model = user.Model
	}
	if user.APIKey != "" {
		effective.APIKey = user.APIKey
	}
	if user.Temperature != 0 {
		effective.Temperature = user.Temperature
	}
	if user.PromptTemplate != "" {
		effective.PromptTemplate = user.PromptTemplate
	}
	if !user.Enabled {
		effective.Enabled = false
	}
	if !user.UpdatedAt.IsZero() {
		effective.UpdatedAt = user.UpdatedAt
	}

	return effective
}
