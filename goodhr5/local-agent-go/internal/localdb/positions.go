// Package localdb 负责管理本地岗位模板数据。
package localdb

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Position 表示本地岗位模板。
type Position struct {
	ID              string         `json:"id"`
	PlatformID      string         `json:"platform_id"`
	Name            string         `json:"name"`
	Keywords        []string       `json:"keywords"`
	ExcludeKeywords []string       `json:"exclude_keywords"`
	Description     string         `json:"description"`
	GreetMessage    string         `json:"greet_message"`
	IsAndMode       bool           `json:"is_and_mode"`
	CommonConfig    map[string]any `json:"common_config"`
	AIConfig        map[string]any `json:"ai_config"`
	KeywordConfig   map[string]any `json:"keyword_config"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

// ListPositions 读取本地岗位模板列表。
// 返回值按更新时间倒序排列。
func (db *DB) ListPositions() ([]Position, error) {
	rows, err := db.conn.Query(`SELECT * FROM local_positions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("读取岗位模板失败：%w", err)
	}
	defer rows.Close()
	result := []Position{}
	for rows.Next() {
		item, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// SavePosition 新增或更新本地岗位模板。
// payload 为岗位模板参数。
func (db *DB) SavePosition(payload map[string]any) (Position, error) {
	now := nowISO()
	positionID := stringOr(payload["id"], uuid.NewString())
	current, _ := db.getPosition(positionID)
	createdAt := now
	if current.ID != "" {
		createdAt = current.CreatedAt
	}
	position := Position{
		ID:              positionID,
		PlatformID:      stringOr(payload["platform_id"], "boss"),
		Name:            stringOr(payload["name"], ""),
		Keywords:        stringList(payload["keywords"]),
		ExcludeKeywords: stringList(payload["exclude_keywords"]),
		Description:     stringOr(payload["description"], ""),
		GreetMessage:    stringOr(payload["greet_message"], ""),
		IsAndMode:       boolValue(payload["is_and_mode"]),
		CommonConfig:    mapValue(payload["common_config"]),
		AIConfig:        mapValue(payload["ai_config"]),
		KeywordConfig:   mapValue(payload["keyword_config"]),
		CreatedAt:       createdAt,
		UpdatedAt:       now,
	}
	if position.Name == "" {
		return Position{}, fmt.Errorf("岗位名称不能为空")
	}
	keywordsJSON, _ := json.Marshal(position.Keywords)
	excludeJSON, _ := json.Marshal(position.ExcludeKeywords)
	commonJSON, _ := json.Marshal(position.CommonConfig)
	aiJSON, _ := json.Marshal(position.AIConfig)
	keywordJSON, _ := json.Marshal(position.KeywordConfig)
	_, err := db.conn.Exec(`
INSERT INTO local_positions (
    id, platform_id, name, keywords_json, exclude_keywords_json, description,
    greet_message, is_and_mode, common_config_json, ai_config_json,
    keyword_config_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    platform_id=excluded.platform_id,
    name=excluded.name,
    keywords_json=excluded.keywords_json,
    exclude_keywords_json=excluded.exclude_keywords_json,
    description=excluded.description,
    greet_message=excluded.greet_message,
    is_and_mode=excluded.is_and_mode,
    common_config_json=excluded.common_config_json,
    ai_config_json=excluded.ai_config_json,
    keyword_config_json=excluded.keyword_config_json,
    updated_at=excluded.updated_at`,
		position.ID, position.PlatformID, position.Name, string(keywordsJSON), string(excludeJSON),
		position.Description, position.GreetMessage, boolInt(position.IsAndMode), string(commonJSON),
		string(aiJSON), string(keywordJSON), position.CreatedAt, position.UpdatedAt,
	)
	if err != nil {
		return Position{}, fmt.Errorf("保存岗位模板失败：%w", err)
	}
	return db.getPosition(positionID)
}

// DeletePosition 删除本地岗位模板。
// positionID 为岗位模板 ID。
func (db *DB) DeletePosition(positionID string) error {
	result, err := db.conn.Exec(`DELETE FROM local_positions WHERE id=?`, positionID)
	if err != nil {
		return fmt.Errorf("删除岗位模板失败：%w", err)
	}
	if count, _ := result.RowsAffected(); count <= 0 {
		return fmt.Errorf("岗位模板不存在")
	}
	return nil
}

// getPosition 按 ID 读取本地岗位模板。
// positionID 为岗位模板 ID。
func (db *DB) getPosition(positionID string) (Position, error) {
	row := db.conn.QueryRow(`SELECT * FROM local_positions WHERE id=?`, positionID)
	return scanPosition(row)
}

// scanPosition 从数据库行扫描岗位模板。
// scanner 为 QueryRow 或 Rows。
func scanPosition(scanner interface{ Scan(dest ...any) error }) (Position, error) {
	var item Position
	var keywordsJSON, excludeJSON, commonJSON, aiJSON, keywordJSON string
	var isAndMode int
	err := scanner.Scan(
		&item.ID, &item.PlatformID, &item.Name, &keywordsJSON, &excludeJSON, &item.Description,
		&item.GreetMessage, &isAndMode, &commonJSON, &aiJSON, &keywordJSON, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return Position{}, err
	}
	item.IsAndMode = isAndMode == 1
	item.Keywords = decodeStringList(keywordsJSON)
	item.ExcludeKeywords = decodeStringList(excludeJSON)
	item.CommonConfig = decodeMap(commonJSON)
	item.AIConfig = decodeMap(aiJSON)
	item.KeywordConfig = decodeMap(keywordJSON)
	return item, nil
}

// stringList 将值转换为字符串列表。
// value 为原始值。
func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return strings
		}
		return []string{}
	}
	result := []string{}
	for _, item := range items {
		if text := stringOr(item, ""); text != "" {
			result = append(result, text)
		}
	}
	return result
}

// decodeStringList 解码 JSON 字符串列表。
// raw 为 JSON 字符串。
func decodeStringList(raw string) []string {
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return []string{}
	}
	return result
}

// decodeMap 解码 JSON 对象。
// raw 为 JSON 字符串。
func decodeMap(raw string) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}
