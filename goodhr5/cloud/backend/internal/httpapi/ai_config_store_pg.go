// 本文件负责提供用户 AI 配置的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresAIConfigStore 使用 PostgreSQL 持久化用户 AI 配置。
type PostgresAIConfigStore struct {
	db *sql.DB
}

// NewPostgresAIConfigStore 创建 PostgreSQL AI 配置存储。
func NewPostgresAIConfigStore(db *sql.DB) *PostgresAIConfigStore {
	return &PostgresAIConfigStore{db: db}
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

// RepairUserModel 原子比对旧配置快照后仅更新模型；若用户已保存新参数则重新读取当前配置。
func (s *PostgresAIConfigStore) RepairUserModel(userEmail string, previous AIConfig, model string) (AIConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var saved AIConfig
	err := s.db.QueryRowContext(ctx, `
		UPDATE user_ai_configs AS uac
		SET model = $2, updated_at = now()
		FROM users AS u
		WHERE u.id = uac.user_id AND u.email = $1
			AND uac.updated_at = $3
			AND uac.base_url = $4 AND uac.model = $5
			AND uac.api_key_encrypted = $6 AND COALESCE(uac.temperature, 0) = $7
			AND uac.prompt_template = $8 AND uac.enabled = $9
		RETURNING uac.base_url, uac.model, uac.api_key_encrypted, COALESCE(uac.temperature, 0), uac.prompt_template, uac.enabled, uac.updated_at
	`, userEmail, model, previous.UpdatedAt, previous.BaseURL, previous.Model, previous.APIKey,
		previous.Temperature, previous.PromptTemplate, previous.Enabled).Scan(
		&saved.BaseURL, &saved.Model, &saved.APIKey, &saved.Temperature,
		&saved.PromptTemplate, &saved.Enabled, &saved.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return s.UserConfig(userEmail)
	}
	if err != nil {
		return AIConfig{}, err
	}
	return saved, nil
}
