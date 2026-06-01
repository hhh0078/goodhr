// 本文件负责提供岗位配置的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PostgresPositionStore 使用 PostgreSQL 持久化岗位配置。
type PostgresPositionStore struct {
	db *sql.DB
}

// NewPostgresPositionStore 创建 PostgreSQL 岗位配置存储。
func NewPostgresPositionStore(db *sql.DB) *PostgresPositionStore {
	return &PostgresPositionStore{db: db}
}

// ListPositions 列出 PostgreSQL 中当前用户的岗位配置。
func (s *PostgresPositionStore) ListPositions(tenantID, userEmail string, isAdmin bool) ([]Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT p.id, COALESCE(p.platform_id, 'boss'), p.name, p.keywords, p.exclude_keywords, p.description, p.greet_message, p.is_and_mode,
		       p.common_config, p.ai_config, p.keyword_config, p.created_at, p.updated_at
		FROM positions p
		INNER JOIN users u ON u.id = p.user_id
		WHERE u.email = $1
		ORDER BY p.updated_at DESC, p.created_at DESC
		`,
		userEmail,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Position, 0)
	for rows.Next() {
		var item Position
		var keywordsJSON []byte
		var excludeKeywordsJSON []byte
		var commonConfigJSON []byte
		var aiConfigJSON []byte
		var keywordConfigJSON []byte
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.PlatformID,
			&item.Name,
			&keywordsJSON,
			&excludeKeywordsJSON,
			&item.Description,
			&item.GreetMessage,
			&item.IsAndMode,
			&commonConfigJSON,
			&aiConfigJSON,
			&keywordConfigJSON,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := decodeStringArray(keywordsJSON, &item.Keywords); err != nil {
			return nil, err
		}
		if err := decodeStringArray(excludeKeywordsJSON, &item.ExcludeKeywords); err != nil {
			return nil, err
		}
		if err := decodeObject(commonConfigJSON, &item.CommonConfig); err != nil {
			return nil, err
		}
		if err := decodeObject(aiConfigJSON, &item.AIConfig); err != nil {
			return nil, err
		}
		if err := decodeObject(keywordConfigJSON, &item.KeywordConfig); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SavePosition 保存 PostgreSQL 中的岗位配置。
func (s *PostgresPositionStore) SavePosition(position Position) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, position.UserEmail)
	if err != nil {
		return Position{}, err
	}

	keywordsJSON, err := json.Marshal(position.Keywords)
	if err != nil {
		return Position{}, err
	}
	excludeKeywordsJSON, err := json.Marshal(position.ExcludeKeywords)
	if err != nil {
		return Position{}, err
	}
	commonConfigJSON, err := json.Marshal(nonNilMap(position.CommonConfig))
	if err != nil {
		return Position{}, err
	}
	aiConfigJSON, err := json.Marshal(nonNilMap(position.AIConfig))
	if err != nil {
		return Position{}, err
	}
	keywordConfigJSON, err := json.Marshal(nonNilMap(position.KeywordConfig))
	if err != nil {
		return Position{}, err
	}

	var saved Position
	saved.UserEmail = position.UserEmail
	var row *sql.Row
	if position.ID == "" {
		row = s.db.QueryRowContext(
			ctx,
			`
			INSERT INTO positions (user_id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config)
			VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, $7, $8, $9::jsonb, $10::jsonb, $11::jsonb)
			RETURNING id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config, created_at, updated_at
			`,
			userID,
			position.PlatformID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
			string(commonConfigJSON),
			string(aiConfigJSON),
			string(keywordConfigJSON),
		)
	} else {
		row = s.db.QueryRowContext(
			ctx,
			`
			UPDATE positions
			SET
				platform_id = $3,
				name = $4,
				keywords = $5::jsonb,
				exclude_keywords = $6::jsonb,
				description = $7,
				greet_message = $8,
				is_and_mode = $9,
				common_config = $10::jsonb,
				ai_config = $11::jsonb,
				keyword_config = $12::jsonb,
				updated_at = now()
			WHERE id = $1 AND user_id = $2
			RETURNING id, platform_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, common_config, ai_config, keyword_config, created_at, updated_at
			`,
			position.ID,
			userID,
			position.PlatformID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
			string(commonConfigJSON),
			string(aiConfigJSON),
			string(keywordConfigJSON),
		)
	}

	var savedKeywordsJSON []byte
	var savedExcludeKeywordsJSON []byte
	var savedCommonConfigJSON []byte
	var savedAIConfigJSON []byte
	var savedKeywordConfigJSON []byte
	err = row.Scan(
		&saved.ID,
		&saved.PlatformID,
		&saved.Name,
		&savedKeywordsJSON,
		&savedExcludeKeywordsJSON,
		&saved.Description,
		&saved.GreetMessage,
		&saved.IsAndMode,
		&savedCommonConfigJSON,
		&savedAIConfigJSON,
		&savedKeywordConfigJSON,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}

	if err := decodeStringArray(savedKeywordsJSON, &saved.Keywords); err != nil {
		return Position{}, err
	}
	if err := decodeStringArray(savedExcludeKeywordsJSON, &saved.ExcludeKeywords); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedCommonConfigJSON, &saved.CommonConfig); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedAIConfigJSON, &saved.AIConfig); err != nil {
		return Position{}, err
	}
	if err := decodeObject(savedKeywordConfigJSON, &saved.KeywordConfig); err != nil {
		return Position{}, err
	}
	return saved, nil
}

// PositionByID 读取 PostgreSQL 中当前用户的单个岗位配置。
func (s *PostgresPositionStore) PositionByID(tenantID, userEmail, positionID string, isAdmin bool) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item Position
	var rawKeywords, rawExclude []byte
	var rawCommonConfig, rawAIConfig, rawKeywordConfig []byte
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT p.id, COALESCE(p.platform_id, 'boss'), p.name, CAST(p.keywords AS text), CAST(p.exclude_keywords AS text),
		       p.description, p.greet_message, p.is_and_mode, CAST(p.common_config AS text), CAST(p.ai_config AS text), CAST(p.keyword_config AS text), p.created_at, p.updated_at
		FROM positions p
		JOIN users u ON p.user_id = u.id
		WHERE u.email = $1 AND p.id = $2
		`,
		userEmail, positionID,
	).Scan(
		&item.ID, &item.PlatformID, &item.Name, &rawKeywords, &rawExclude,
		&item.Description, &item.GreetMessage, &item.IsAndMode,
		&rawCommonConfig, &rawAIConfig, &rawKeywordConfig,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}

	_ = decodeStringArray(rawKeywords, &item.Keywords)
	_ = decodeStringArray(rawExclude, &item.ExcludeKeywords)
	_ = decodeObject(rawCommonConfig, &item.CommonConfig)
	_ = decodeObject(rawAIConfig, &item.AIConfig)
	_ = decodeObject(rawKeywordConfig, &item.KeywordConfig)
	return item, nil
}

// DeletePosition 删除 PostgreSQL 中当前用户的岗位配置。
func (s *PostgresPositionStore) DeletePosition(userEmail string, positionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := s.db.ExecContext(
		ctx,
		`
		DELETE FROM positions p
		USING users u
		WHERE p.user_id = u.id
		  AND u.email = $1
		  AND p.id = $2
		`,
		userEmail,
		positionID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// decodeStringArray 解析数据库里的 JSON 字符串数组。
func decodeStringArray(value []byte, target *[]string) error {
	if len(value) == 0 {
		*target = []string{}
		return nil
	}
	return json.Unmarshal(value, target)
}

func decodeObject(value []byte, target *map[string]any) error {
	if len(value) == 0 {
		*target = map[string]any{}
		return nil
	}
	return json.Unmarshal(value, target)
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}
