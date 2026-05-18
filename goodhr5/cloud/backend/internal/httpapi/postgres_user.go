// 本文件负责封装 PostgreSQL 中与用户行解析和创建相关的公共方法。
package httpapi

import (
	"context"
	"database/sql"
)

// ensureUserID 确保指定邮箱在 users 表中存在，并返回对应的 user_id。
func ensureUserID(ctx context.Context, db *sql.DB, email string) (string, error) {
	var userID string
	err := db.QueryRowContext(
		ctx,
		`
		INSERT INTO users (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id
		`,
		email,
	).Scan(&userID)
	if err != nil {
		return "", err
	}
	return userID, nil
}
