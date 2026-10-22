// 本文件统一限制站内 AI 模型，并在读取旧配置时保存修复结果，外部接口继续使用自定义模型。
package httpapi

import (
	"net/url"
	"strings"
)

// modelRestrictedAIConfigStore 在原有 AI 配置存储外统一应用站内模型规则。
type modelRestrictedAIConfigStore struct {
	store         AIConfigStore
	systemConfigs SystemConfigStore
}

// newModelRestrictedAIConfigStore 创建供个人配置、登录初始化和云端 AI 调用共用的存储。
func newModelRestrictedAIConfigStore(store AIConfigStore, systemConfigs SystemConfigStore) AIConfigStore {
	return &modelRestrictedAIConfigStore{store: store, systemConfigs: systemConfigs}
}

// UserConfig 读取用户 AI 配置，并把站内已失效模型持久化为当前列表第一项。
func (s *modelRestrictedAIConfigStore) UserConfig(userEmail string) (AIConfig, error) {
	config, err := s.store.UserConfig(userEmail)
	if err != nil {
		return AIConfig{}, err
	}
	normalized, err := normalizeUserAIConfig(config, s.systemConfigs)
	if err != nil {
		return AIConfig{}, err
	}
	if normalized.Model != config.Model {
		return s.store.RepairUserModel(userEmail, config, normalized.Model)
	}
	return config, nil
}

// SaveUserConfig 保存用户 AI 配置，站内接口只能使用公共配置中列出的模型。
func (s *modelRestrictedAIConfigStore) SaveUserConfig(userEmail string, config AIConfig) (AIConfig, error) {
	normalized, err := normalizeUserAIConfig(config, s.systemConfigs)
	if err != nil {
		return AIConfig{}, err
	}
	return s.store.SaveUserConfig(userEmail, normalized)
}

// RepairUserModel 复用模型规范化及底层原子修复，防止调用方绕过站内模型规则。
func (s *modelRestrictedAIConfigStore) RepairUserModel(userEmail string, previous AIConfig, model string) (AIConfig, error) {
	candidate := previous
	candidate.Model = model
	normalized, err := normalizeUserAIConfig(candidate, s.systemConfigs)
	if err != nil {
		return AIConfig{}, err
	}
	return s.store.RepairUserModel(userEmail, previous, normalized.Model)
}

// normalizeUserAIConfig 仅为域名与当前网站完全一致的接口纠正模型，保留外部接口的模型输入。
func normalizeUserAIConfig(config AIConfig, systemConfigs SystemConfigStore) (AIConfig, error) {
	if !isCurrentSiteAIURL(config.BaseURL) {
		return config, nil
	}
	cfg, err := loadBuiltinAIConfig(systemConfigs)
	if err != nil {
		return AIConfig{}, err
	}
	model, ok := cfg.modelByID(config.Model)
	if !ok {
		model = cfg.Models[0]
	}
	config.Model = model.ID
	return config, nil
}

// isCurrentSiteAIURL 按当前部署的公开域名判断是否需要限制 AI 模型，域名比较忽略大小写。
func isCurrentSiteAIURL(baseURL string) bool {
	publicURL, err := url.Parse(deploymentPublicBaseURL())
	if err != nil || publicURL.Hostname() == "" || publicURL.User != nil {
		return false
	}
	endpoint, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || endpoint.User != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Hostname() == "" {
		return false
	}
	return strings.EqualFold(endpoint.Hostname(), publicURL.Hostname())
}
