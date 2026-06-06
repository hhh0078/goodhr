// Package localdb 负责管理招聘平台账号对应的本机浏览器 Profile。
package localdb

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Profile 表示招聘平台账号对应的本机浏览器目录。
type Profile struct {
	ID             string `json:"id"`
	PlatformID     string `json:"platform_id"`
	DisplayName    string `json:"display_name"`
	LocalProfileID string `json:"local_profile_id"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// ListProfiles 读取本机浏览器 Profile 列表。
// platformID 为空时返回全部平台的 Profile。
func (db *DB) ListProfiles(platformID string) ([]Profile, error) {
	query := `SELECT id, platform_id, display_name, local_profile_id, status, created_at, updated_at FROM local_profiles`
	args := []any{}
	if strings.TrimSpace(platformID) != "" {
		query += ` WHERE platform_id=?`
		args = append(args, strings.TrimSpace(platformID))
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取浏览器 Profile 失败：%w", err)
	}
	defer rows.Close()
	result := []Profile{}
	for rows.Next() {
		var item Profile
		if err := rows.Scan(&item.ID, &item.PlatformID, &item.DisplayName, &item.LocalProfileID, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// SaveProfile 保存本机浏览器 Profile。
// payload 为前端传入的 Profile 参数。
func (db *DB) SaveProfile(payload map[string]any) (Profile, error) {
	now := nowISO()
	item := Profile{
		ID:             strings.TrimSpace(stringOr(payload["id"], uuid.NewString())),
		PlatformID:     strings.TrimSpace(stringOr(payload["platform_id"], "boss")),
		DisplayName:    strings.TrimSpace(stringOr(payload["display_name"], stringOr(payload["name"], ""))),
		LocalProfileID: strings.TrimSpace(stringOr(payload["local_profile_id"], "")),
		Status:         strings.TrimSpace(stringOr(payload["status"], "active")),
		CreatedAt:      stringOr(payload["created_at"], now),
		UpdatedAt:      now,
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.PlatformID == "" {
		item.PlatformID = "boss"
	}
	if item.DisplayName == "" {
		item.DisplayName = item.PlatformID + "账号"
	}
	if item.LocalProfileID == "" {
		item.LocalProfileID = item.ID
	}
	if item.Status == "" {
		item.Status = "active"
	}
	_, err := db.conn.Exec(`
INSERT INTO local_profiles(id, platform_id, display_name, local_profile_id, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    platform_id=excluded.platform_id,
    display_name=excluded.display_name,
    local_profile_id=excluded.local_profile_id,
    status=excluded.status,
    updated_at=excluded.updated_at`,
		item.ID, item.PlatformID, item.DisplayName, item.LocalProfileID, item.Status, item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return Profile{}, fmt.Errorf("保存浏览器 Profile 失败：%w", err)
	}
	return db.GetProfile(item.ID)
}

// GetProfile 读取单个本机浏览器 Profile。
// profileID 为 Profile ID。
func (db *DB) GetProfile(profileID string) (Profile, error) {
	row := db.conn.QueryRow(
		`SELECT id, platform_id, display_name, local_profile_id, status, created_at, updated_at FROM local_profiles WHERE id=?`,
		strings.TrimSpace(profileID),
	)
	var item Profile
	err := row.Scan(&item.ID, &item.PlatformID, &item.DisplayName, &item.LocalProfileID, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("浏览器 Profile 不存在")
	}
	return item, err
}

// DeleteProfile 删除本机浏览器 Profile 元数据。
// profileID 为 Profile ID，实际浏览器目录暂不删除。
func (db *DB) DeleteProfile(profileID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_profiles WHERE id=?`, strings.TrimSpace(profileID))
	if err != nil {
		return fmt.Errorf("删除浏览器 Profile 失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("浏览器 Profile 不存在")
	}
	return nil
}
