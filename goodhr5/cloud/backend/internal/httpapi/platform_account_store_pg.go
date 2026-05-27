// 本文件负责提供平台账号映射的 PostgreSQL 存储实现。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// PostgresPlatformAccountStore 使用 PostgreSQL 持久化平台账号映射。
type PostgresPlatformAccountStore struct {
	db *sql.DB
}

// NewPostgresPlatformAccountStore 创建 PostgreSQL 平台账号映射存储。
func NewPostgresPlatformAccountStore(db *sql.DB) *PostgresPlatformAccountStore {
	return &PostgresPlatformAccountStore{db: db}
}

// ListPlatformAccounts 按用户和平台列出 PostgreSQL 中的平台账号映射。
func (s *PostgresPlatformAccountStore) ListPlatformAccounts(tenantID, userEmail, platformID string, isAdmin bool) ([]PlatformAccount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		SELECT pa.id, pa.platform_id, pa.display_name, pa.local_profile_id, pa.created_at
		FROM platform_accounts pa
		INNER JOIN users u ON u.id = pa.user_id
		WHERE u.email = $1
	`
	args := []any{userEmail}
	if platformID != "" {
		query += ` AND pa.platform_id = $2`
		args = append(args, platformID)
	}
	query += ` ORDER BY pa.created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]PlatformAccount, 0)
	for rows.Next() {
		var item PlatformAccount
		item.UserEmail = userEmail
		if err := rows.Scan(
			&item.ID,
			&item.PlatformID,
			&item.DisplayName,
			&item.LocalProfileID,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SavePlatformAccount 保存 PostgreSQL 平台账号映射，并避免重复创建。
func (s *PostgresPlatformAccountStore) SavePlatformAccount(account PlatformAccount) (PlatformAccount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	userID, err := ensureUserID(ctx, s.db, account.UserEmail)
	if err != nil {
		return PlatformAccount{}, err
	}

	var saved PlatformAccount
	saved.UserEmail = account.UserEmail
	err = s.db.QueryRowContext(
		ctx,
		`
		INSERT INTO platform_accounts (user_id, platform_id, display_name, local_profile_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, platform_id, display_name, local_profile_id, created_at
		`,
		userID,
		account.PlatformID,
		account.DisplayName,
		account.LocalProfileID,
	).Scan(
		&saved.ID,
		&saved.PlatformID,
		&saved.DisplayName,
		&saved.LocalProfileID,
		&saved.CreatedAt,
	)
	if err == nil {
		return saved, nil
	}

	if isPostgresUniqueViolation(err) {
		return PlatformAccount{}, ErrConflict
	}
	return PlatformAccount{}, err
}

// DeletePlatformAccount 删除 PostgreSQL 中当前用户的平台账号映射。
func (s *PostgresPlatformAccountStore) DeletePlatformAccount(userEmail string, accountID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := s.db.ExecContext(
		ctx,
		`
		DELETE FROM platform_accounts pa
		USING users u
		WHERE pa.user_id = u.id
		  AND u.email = $1
		  AND pa.id = $2
		`,
		userEmail,
		accountID,
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

// isPostgresUniqueViolation 判断是否为 PostgreSQL 唯一索引冲突。
func isPostgresUniqueViolation(err error) bool {
	var postgresError *pq.Error
	if errors.As(err, &postgresError) {
		return string(postgresError.Code) == "23505"
	}
	return false
}
