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
func (s *PostgresPositionStore) ListPositions(userEmail string) ([]Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(
		ctx,
		`
		SELECT p.id, p.name, p.keywords, p.exclude_keywords, p.description, p.greet_message, p.is_and_mode, p.created_at, p.updated_at
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
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&keywordsJSON,
			&excludeKeywordsJSON,
			&item.Description,
			&item.GreetMessage,
			&item.IsAndMode,
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

	var saved Position
	saved.UserEmail = position.UserEmail
	var row *sql.Row
	if position.ID == "" {
		row = s.db.QueryRowContext(
			ctx,
			`
			INSERT INTO positions (user_id, name, keywords, exclude_keywords, description, greet_message, is_and_mode)
			VALUES ($1, $2, $3::jsonb, $4::jsonb, $5, $6, $7)
			RETURNING id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, created_at, updated_at
			`,
			userID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
		)
	} else {
		row = s.db.QueryRowContext(
			ctx,
			`
			UPDATE positions
			SET
				name = $3,
				keywords = $4::jsonb,
				exclude_keywords = $5::jsonb,
				description = $6,
				greet_message = $7,
				is_and_mode = $8,
				updated_at = now()
			WHERE id = $1 AND user_id = $2
			RETURNING id, name, keywords, exclude_keywords, description, greet_message, is_and_mode, created_at, updated_at
			`,
			position.ID,
			userID,
			position.Name,
			string(keywordsJSON),
			string(excludeKeywordsJSON),
			position.Description,
			position.GreetMessage,
			position.IsAndMode,
		)
	}

	var savedKeywordsJSON []byte
	var savedExcludeKeywordsJSON []byte
	err = row.Scan(
		&saved.ID,
		&saved.Name,
		&savedKeywordsJSON,
		&savedExcludeKeywordsJSON,
		&saved.Description,
		&saved.GreetMessage,
		&saved.IsAndMode,
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
	return saved, nil
}

// PositionByID 读取 PostgreSQL 中当前用户的单个岗位配置。
func (s *PostgresPositionStore) PositionByID(userEmail string, positionID string) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item Position
	var rawKeywords, rawExclude []byte
	err := s.db.QueryRowContext(
		ctx,
		`
		SELECT p.id, p.name, CAST(p.keywords AS text), CAST(p.exclude_keywords AS text),
		       p.description, p.greet_message, p.is_and_mode, p.created_at, p.updated_at
		FROM positions p
		JOIN users u ON p.user_id = u.id
		WHERE u.email = $1 AND p.id = $2
		`,
		userEmail, positionID,
	).Scan(
		&item.ID, &item.Name, &rawKeywords, &rawExclude,
		&item.Description, &item.GreetMessage, &item.IsAndMode,
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
