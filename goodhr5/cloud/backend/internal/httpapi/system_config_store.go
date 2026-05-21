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
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Domain   string           `json:"domain"`
	Pages    []PlatformPage   `json:"pages,omitempty"`
	Card     PlatformCard     `json:"card"`
	Actions  PlatformActions  `json:"actions"`
	Detail   PlatformDetail   `json:"detail"`
	Extras   []PlatformExtra  `json:"extras,omitempty"`
	Behavior PlatformBehavior `json:"behavior"`
}

// PlatformPage 定义平台的合法页面。
type PlatformPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// ElementLocatorConfig 定义统一元素定位协议的配置结构。
type ElementLocatorConfig struct {
	ParentClasses [][]string `json:"parent_classes,omitempty"`
	TargetClasses [][]string `json:"target_classes"`
	FindAttempts  int        `json:"find_attempts,omitempty"`
	FindInterval  int        `json:"find_interval_ms,omitempty"`
}

func normalizeSelectorGroups(groups [][]string) [][]string {
	result := make([][]string, 0, len(groups))
	for _, group := range groups {
		normalized := normalizeSelectorList(group)
		if len(normalized) > 0 {
			result = append(result, normalized)
		}
	}
	return result
}

func (c ElementLocatorConfig) AsPayload() map[string]any {
	targets := normalizeSelectorGroups(c.TargetClasses)
	if len(targets) == 0 {
		return nil
	}
	payload := map[string]any{
		"target_classes": targets,
	}
	if parents := normalizeSelectorGroups(c.ParentClasses); len(parents) > 0 {
		payload["parent_classes"] = parents
	}
	if c.FindAttempts > 0 {
		payload["find_attempts"] = c.FindAttempts
	}
	if c.FindInterval > 0 {
		payload["find_interval_ms"] = c.FindInterval
	}
	return payload
}

// PlatformCard 定义候选人卡片、滚动目标和字段定位配置。
type PlatformCard struct {
	Scroll ElementLocatorConfig `json:"scroll"`
	Item   ElementLocatorConfig `json:"item"`
	Fields []map[string]ElementLocatorConfig `json:"fields"`
}

// PlatformActions 定义候选人操作按钮的定位配置。
type PlatformActions struct {
	GreetBtn    ElementLocatorConfig `json:"greetBtn"`
	ContinueBtn ElementLocatorConfig `json:"continueBtn"`
	PhoneBtn    ElementLocatorConfig `json:"phoneBtn"`
	WechatBtn   ElementLocatorConfig `json:"wechatBtn"`
	ResumeBtn   ElementLocatorConfig `json:"resumeBtn"`
	ConfirmBtn  ElementLocatorConfig `json:"confirmBtn"`
}

// PlatformDetail 定义候选人详情弹框的定位配置。
type PlatformDetail struct {
	OpenTarget  ElementLocatorConfig `json:"openTarget"`
	CloseBtn    ElementLocatorConfig `json:"closeBtn"`
	MessageTip  ElementLocatorConfig `json:"messageTip"`
	MessageItem ElementLocatorConfig `json:"messageItem"`
}

// PlatformExtra 定义额外提取字段的定位配置。
type PlatformExtra struct {
	Element ElementLocatorConfig `json:"element"`
	Label   string               `json:"label"`
}

// PlatformBehavior 定义平台特定行为配置。
type PlatformBehavior struct {
	NeedsDetailPage       bool   `json:"needsDetailPage"`
	SupportsPaging        bool   `json:"supportsPaging"`
	NextPageBtn           string `json:"nextPageBtn"`
	NextPageDisabledClass string `json:"nextPageDisabledClass"`
}

func normalizeSelectorList(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

// ExtractFieldRequests 提取 PlatformCard 中所有字段定位规则，
// 用于调用 Local Agent POST /api/v1/page/extract-fields。
func (pc *PlatformCard) ExtractFieldRequests() []map[string]any {
	fields := make([]map[string]any, 0, len(pc.Fields))
	for _, item := range pc.Fields {
		for name, cfg := range item {
			if payload := cfg.AsPayload(); payload != nil {
				fields = append(fields, map[string]any{name: payload})
			}
		}
	}
	return fields
}

func (pc *PlatformCard) CardElement() map[string]any {
	return pc.Item.AsPayload()
}

func (pc *PlatformCard) ScrollElement() map[string]any {
	return pc.Scroll.AsPayload()
}

// ParsePlatformConfig 从 JSON 字符串解析平台配置。
func ParsePlatformConfig(raw string) (PlatformConfig, error) {
	var cfg PlatformConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return PlatformConfig{}, err
	}
	return cfg, nil
}
