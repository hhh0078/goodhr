// 本文件负责提供 AI 配置的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresAIConfigStore 使用 PostgreSQL 持久化系统和用户 AI 配置。
type PostgresAIConfigStore struct {
	db *sql.DB
}

// NewPostgresAIConfigStore 创建 PostgreSQL AI 配置存储。
func NewPostgresAIConfigStore(db *sql.DB) *PostgresAIConfigStore {
	return &PostgresAIConfigStore{db: db}
}

// SystemConfig 读取 PostgreSQL 中的系统默认 AI 配置。
func (s *PostgresAIConfigStore) SystemConfig() (AIConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var config AIConfig
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT base_url, model, api_key_encrypted, temperature, prompt_template, enabled, updated_at
		FROM system_ai_configs
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 1
		`,
	).Scan(
		&config.BaseURL,
		&config.Model,
		&config.APIKey,
		&config.Temperature,
		&config.PromptTemplate,
		&config.Enabled,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		defaultConfig := DefaultSystemAIConfig()
		return s.SaveSystemConfig(defaultConfig)
	}
	if err != nil {
		return AIConfig{}, err
	}
	return config, nil
}

// SaveSystemConfig 保存 PostgreSQL 中的系统默认 AI 配置。
func (s *PostgresAIConfigStore) SaveSystemConfig(config AIConfig) (AIConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var saved AIConfig
	err := s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO system_ai_configs (base_url, model, api_key_encrypted, temperature, prompt_template, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING base_url, model, api_key_encrypted, temperature, prompt_template, enabled, updated_at
		`,
		config.BaseURL,
		config.Model,
		config.APIKey,
		config.Temperature,
		config.PromptTemplate,
		config.Enabled,
	).Scan(
		&saved.BaseURL,
		&saved.Model,
		&saved.APIKey,
		&saved.Temperature,
		&saved.PromptTemplate,
		&saved.Enabled,
		&saved.UpdatedAt,
	)
	if err != nil {
		return AIConfig{}, err
	}
	return saved, nil
}

// UserConfig 读取 PostgreSQL 中指定用户的自定义 AI 配置。
func (s *PostgresAIConfigStore) UserConfig(userEmail string) (AIConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var config AIConfig
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT uac.base_url, uac.model, uac.api_key_encrypted, COALESCE(uac.temperature, 0), uac.prompt_template, uac.enabled, uac.updated_at
		FROM user_ai_configs uac
		INNER JOIN users u ON u.id = uac.user_id
		WHERE u.email = $1
		`,
		userEmail,
	).Scan(
		&config.BaseURL,
		&config.Model,
		&config.APIKey,
		&config.Temperature,
		&config.PromptTemplate,
		&config.Enabled,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AIConfig{}, ErrNotFound
	}
	if err != nil {
		return AIConfig{}, err
	}
	return config, nil
}

// SaveUserConfig 保存 PostgreSQL 中指定用户的自定义 AI 配置。
func (s *PostgresAIConfigStore) SaveUserConfig(userEmail string, config AIConfig) (AIConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, userEmail)
	if err != nil {
		return AIConfig{}, err
	}

	var temperature any
	if config.Temperature != 0 {
		temperature = config.Temperature
	}

	var saved AIConfig
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO user_ai_configs (
			user_id,
			base_url,
			model,
			api_key_encrypted,
			temperature,
			prompt_template,
			enabled
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id)
		DO UPDATE SET
			base_url = EXCLUDED.base_url,
			model = EXCLUDED.model,
			api_key_encrypted = EXCLUDED.api_key_encrypted,
			temperature = EXCLUDED.temperature,
			prompt_template = EXCLUDED.prompt_template,
			enabled = EXCLUDED.enabled,
			updated_at = now()
		RETURNING base_url, model, api_key_encrypted, COALESCE(temperature, 0), prompt_template, enabled, updated_at
		`,
		userID,
		config.BaseURL,
		config.Model,
		config.APIKey,
		temperature,
		config.PromptTemplate,
		config.Enabled,
	).Scan(
		&saved.BaseURL,
		&saved.Model,
		&saved.APIKey,
		&saved.Temperature,
		&saved.PromptTemplate,
		&saved.Enabled,
		&saved.UpdatedAt,
	)
	if err != nil {
		return AIConfig{}, err
	}
	return saved, nil
}
