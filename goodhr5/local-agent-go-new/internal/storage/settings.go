// Package storage 文件作用：读写本地程序的键值设置，供下载目录等用户偏好重启后继续使用。
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SettingKeyDownloadRoots 保存用户切换过的全部下载目录，多个目录用换行分隔。
const SettingKeyDownloadRoots = "download_roots"

// GetSetting 读取一个本地设置，设置不存在时返回空字符串。
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM local_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取本地设置失败：%w", err)
	}
	return value, nil
}

// SetSetting 保存一个本地设置，已存在时直接覆盖。
func (s *Store) SetSetting(ctx context.Context, key string, value string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO local_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now,
	)
	if err != nil {
		return fmt.Errorf("保存本地设置失败：%w", err)
	}
	return nil
}
