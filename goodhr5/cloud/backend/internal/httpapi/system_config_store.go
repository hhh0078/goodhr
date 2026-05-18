// 本文件负责提供云端系统配置的存储抽象和内存/PostgreSQL 双实现。
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// SystemConfig 表示一条系统配置记录。
type SystemConfig struct {
	ConfigKey   string `json:"config_key"`
	ConfigValue string `json:"config_value"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// SystemConfigStore 定义系统配置的持久化接口。
type SystemConfigStore interface {
	// Get 按配置键读取一条配置。
	Get(key string) (SystemConfig, error)
	// List 列出所有启用且键名匹配 prefix 的配置。
	List(prefix string) ([]SystemConfig, error)
}

var ErrConfigNotFound = errors.New("system config not found")

// ---------- 内存实现 ----------

// MemorySystemConfigStore 在内存中存储系统配置，用于未启用 PostgreSQL 时的开发环境。
type MemorySystemConfigStore struct {
	configs map[string]SystemConfig
}

// NewMemorySystemConfigStore 创建内存系统配置存储实例。
func NewMemorySystemConfigStore() *MemorySystemConfigStore {
	return &MemorySystemConfigStore{
		configs: map[string]SystemConfig{},
	}
}

// Get 按配置键读取一条配置。
func (s *MemorySystemConfigStore) Get(key string) (SystemConfig, error) {
	cfg, ok := s.configs[key]
	if !ok || !cfg.Enabled {
		return SystemConfig{}, ErrConfigNotFound
	}
	return cfg, nil
}

// List 列出所有启用且键名匹配 prefix 的配置。
func (s *MemorySystemConfigStore) List(prefix string) ([]SystemConfig, error) {
	var result []SystemConfig
	for key, cfg := range s.configs {
		if !cfg.Enabled {
			continue
		}
		if strings.HasPrefix(key, prefix) {
			result = append(result, cfg)
		}
	}
	return result, nil
}

// ---------- PostgreSQL 实现 ----------

// PostgresSystemConfigStore 使用 PostgreSQL 持久化系统配置。
type PostgresSystemConfigStore struct {
	db *sql.DB
}

// NewPostgresSystemConfigStore 创建 PostgreSQL 系统配置存储实例。
func NewPostgresSystemConfigStore(db *sql.DB) *PostgresSystemConfigStore {
	return &PostgresSystemConfigStore{db: db}
}

// Get 从 PostgreSQL system_configs 表读取一条配置。
func (s *PostgresSystemConfigStore) Get(key string) (SystemConfig, error) {
	var cfg SystemConfig
	var rawValue []byte

	err := s.db.QueryRow(
		`SELECT config_key, config_value, description, enabled
		 FROM system_configs
		 WHERE config_key = $1 AND enabled = true`,
		key,
	).Scan(&cfg.ConfigKey, &rawValue, &cfg.Description, &cfg.Enabled)

	if errors.Is(err, sql.ErrNoRows) {
		return SystemConfig{}, ErrConfigNotFound
	}
	if err != nil {
		return SystemConfig{}, err
	}

	cfg.ConfigValue = string(rawValue)
	return cfg, nil
}

// List 从 PostgreSQL 列出所有启用且键名匹配 prefix 的配置。
func (s *PostgresSystemConfigStore) List(prefix string) ([]SystemConfig, error) {
	rows, err := s.db.Query(
		`SELECT config_key, config_value, description, enabled
		 FROM system_configs
		 WHERE config_key LIKE $1 AND enabled = true
		 ORDER BY config_key`,
		prefix+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SystemConfig
	for rows.Next() {
		var cfg SystemConfig
		var rawValue []byte
		if err := rows.Scan(&cfg.ConfigKey, &rawValue, &cfg.Description, &cfg.Enabled); err != nil {
			return nil, err
		}
		cfg.ConfigValue = string(rawValue)
		result = append(result, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 确保至少返回空数组而不是 null
	if result == nil {
		result = []SystemConfig{}
	}
	return result, nil
}

// PlatformConfig 存储系统配置 JSON 的 value 部分。
type PlatformConfig struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Domain  string        `json:"domain"`
	Pages   []PlatformPage `json:"pages,omitempty"`
	Card    PlatformCard   `json:"card"`
	Actions PlatformActions `json:"actions"`
	Detail  PlatformDetail `json:"detail"`
	Extras  []PlatformExtra `json:"extras,omitempty"`
	Behavior PlatformBehavior `json:"behavior"`
}

// PlatformPage 定义平台的合法页面。
type PlatformPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// PlatformCard 定义候选人卡片的选择器。
type PlatformCard struct {
	Container     string   `json:"container"`
	Cards         []string `json:"card"`
	Name          string   `json:"name"`
	BasicInfo     []string `json:"basicInfo"`
	Education     []string `json:"education"`
	University    string   `json:"university"`
	Description   string   `json:"description"`
}

// PlatformActions 定义候选人操作按钮的选择器。
type PlatformActions struct {
	GreetBtn    []string `json:"greetBtn"`
	ContinueBtn []string `json:"continueBtn"`
	PhoneBtn    []string `json:"phoneBtn"`
	WechatBtn   []string `json:"wechatBtn"`
	ResumeBtn   []string `json:"resumeBtn"`
	ConfirmBtn  []string `json:"confirmBtn"`
}

// PlatformDetail 定义候选人详情弹框的选择器。
type PlatformDetail struct {
	OpenTarget  []string `json:"openTarget"`
	CloseBtn    []string `json:"closeBtn"`
	MessageTip  string   `json:"messageTip"`
	MessageItem string   `json:"messageItem"`
}

// PlatformExtra 定义额外提取字段的选择器。
type PlatformExtra struct {
	Selector string `json:"selector"`
	Label    string `json:"label"`
}

// PlatformBehavior 定义平台特定行为配置。
type PlatformBehavior struct {
	NeedsDetailPage        bool   `json:"needsDetailPage"`
	SupportsPaging         bool   `json:"supportsPaging"`
	NextPageBtn            string `json:"nextPageBtn"`
	NextPageDisabledClass  string `json:"nextPageDisabledClass"`
}

// ExtractFieldSelectors 提取 PlatformCard 中所有字段选择器的映射，
// 用于调用 Local Agent POST /api/v1/page/extract。
func (pc *PlatformCard) ExtractFieldSelectors() map[string]string {
	fields := map[string]string{}
	if pc.Name != "" {
		fields["name"] = pc.Name
	}
	for i, sel := range pc.BasicInfo {
		key := "basic_info"
		if i > 0 {
			key = "basic_info_" + string(rune('0'+i))
		}
		if sel != "" {
			fields[key] = sel
		}
	}
	for i, sel := range pc.Education {
		key := "education"
		if i > 0 {
			key = "education_" + string(rune('0'+i))
		}
		if sel != "" {
			fields[key] = sel
		}
	}
	if pc.University != "" {
		fields["university"] = pc.University
	}
	if pc.Description != "" {
		fields["description"] = pc.Description
	}
	return fields
}

// ParsePlatformConfig 从 JSON 字符串解析平台配置。
func ParsePlatformConfig(raw string) (PlatformConfig, error) {
	var cfg PlatformConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return PlatformConfig{}, err
	}
	return cfg, nil
}
