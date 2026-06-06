// Package localdb 负责管理本地 AI 配置。
package localdb

import (
	"encoding/json"
	"fmt"
)

const defaultAIConfigID = "default"

// AIConfig 表示本机保存的 AI 接口配置。
type AIConfig struct {
	ID          string         `json:"id"`
	Provider    string         `json:"provider"`
	BaseURL     string         `json:"base_url"`
	APIKey      string         `json:"api_key"`
	Model       string         `json:"model"`
	Temperature float64        `json:"temperature"`
	Timeout     int            `json:"timeout"`
	Extra       map[string]any `json:"extra"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

// GetAIConfig 读取默认 AI 配置。
// 未配置时返回空配置。
func (db *DB) GetAIConfig() (AIConfig, error) {
	row := db.conn.QueryRow(`SELECT * FROM local_ai_config WHERE id=?`, defaultAIConfigID)
	config, err := scanAIConfig(row)
	if err != nil {
		return emptyAIConfig(), nil
	}
	return config, nil
}

// SaveAIConfig 保存默认 AI 配置。
// payload 为配置参数。
func (db *DB) SaveAIConfig(payload map[string]any) (AIConfig, error) {
	current, _ := db.GetAIConfig()
	now := nowISO()
	createdAt := current.CreatedAt
	if createdAt == "" {
		createdAt = now
	}
	config := AIConfig{
		ID:          defaultAIConfigID,
		Provider:    stringOr(payload["provider"], current.Provider),
		BaseURL:     stringOr(payload["base_url"], stringOr(payload["api_url"], current.BaseURL)),
		APIKey:      stringOr(payload["api_key"], current.APIKey),
		Model:       stringOr(payload["model"], stringOr(payload["model_id"], current.Model)),
		Temperature: floatValue(payload["temperature"], current.Temperature),
		Timeout:     maxInt(1, intValueOr(payload["timeout"], current.Timeout)),
		Extra:       mapValueOr(payload["extra"], mapValueOr(payload["extra_body"], current.Extra)),
		CreatedAt:   createdAt,
		UpdatedAt:   now,
	}
	extraJSON, _ := json.Marshal(config.Extra)
	_, err := db.conn.Exec(`
INSERT INTO local_ai_config (
    id, provider, base_url, api_key, model, temperature, timeout, extra_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    provider=excluded.provider,
    base_url=excluded.base_url,
    api_key=excluded.api_key,
    model=excluded.model,
    temperature=excluded.temperature,
    timeout=excluded.timeout,
    extra_json=excluded.extra_json,
    updated_at=excluded.updated_at`,
		config.ID, config.Provider, config.BaseURL, config.APIKey, config.Model,
		config.Temperature, config.Timeout, string(extraJSON), config.CreatedAt, config.UpdatedAt,
	)
	if err != nil {
		return AIConfig{}, fmt.Errorf("保存 AI 配置失败：%w", err)
	}
	return db.GetAIConfig()
}

// scanAIConfig 从数据库行扫描 AI 配置。
// scanner 为 QueryRow 或 Rows。
func scanAIConfig(scanner interface{ Scan(dest ...any) error }) (AIConfig, error) {
	var config AIConfig
	var extraJSON string
	err := scanner.Scan(
		&config.ID, &config.Provider, &config.BaseURL, &config.APIKey, &config.Model,
		&config.Temperature, &config.Timeout, &extraJSON, &config.CreatedAt, &config.UpdatedAt,
	)
	if err != nil {
		return AIConfig{}, err
	}
	config.Extra = decodeMap(extraJSON)
	return config, nil
}

// emptyAIConfig 返回空 AI 配置。
// 用于未配置时返回给前端。
func emptyAIConfig() AIConfig {
	return AIConfig{ID: defaultAIConfigID, Temperature: 0.2, Timeout: 120, Extra: map[string]any{}}
}

// floatValue 将值转换为浮点数。
// value 为原始值，fallback 为默认值。
func floatValue(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case json.Number:
		v, err := typed.Float64()
		if err == nil {
			return v
		}
	}
	return fallback
}

// intValueOr 将值转换为整数，空值使用默认值。
// value 为原始值，fallback 为默认值。
func intValueOr(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	converted := intValue(value)
	if converted == 0 {
		return fallback
	}
	return converted
}

// mapValueOr 将值转换为 map，空值使用默认值。
// value 为原始值，fallback 为默认值。
func mapValueOr(value any, fallback map[string]any) map[string]any {
	if value == nil {
		return fallback
	}
	return mapValue(value)
}
