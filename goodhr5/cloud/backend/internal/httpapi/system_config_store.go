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
	// Save 保存或更新一条系统配置。
	Save(cfg SystemConfig) error
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
		configs: defaultMemorySystemConfigs(),
	}
}

// defaultMemorySystemConfigs 返回开发环境默认系统配置。
func defaultMemorySystemConfigs() map[string]SystemConfig {
	return map[string]SystemConfig{
		"system.app_config": {
			ConfigKey: "system.app_config",
			ConfigValue: `{
				"free_daily_greet_limit": 100,
				"position_requirement_optimize_prompt": "你是一个招聘筛选规则整理助手。请把用户输入的岗位要求整理成适合 AI 筛选候选人简历的规则。\n\n要求：\n1. 只保留候选人自身条件，不要保留岗位福利、薪资待遇、工作时间、公司介绍、岗位职责、工作内容。\n2. 去掉无法从简历中稳定判断的主观要求，例如：有上进心、责任心强、抗压能力强、沟通能力好、性格开朗、团队意识强、吃苦耐劳等。\n3. 优先保留硬性条件，例如：学历、专业、工作年限、行业经验、岗位经验、证书、技能、城市、年龄、到岗状态。\n4. 如果原文里有模糊条件，请改写成更清晰的筛选规则。\n5. 输出中文，按条目列出，不要解释，不要输出 JSON。\n\n用户输入：\n{{input}}",
				"email_domain_whitelist": ["qq.com", "foxmail.com", "163.com", "126.com", "yeah.net", "sina.com", "sina.cn", "sohu.com", "aliyun.com", "139.com", "189.cn", "wo.cn", "gmail.com", "outlook.com", "hotmail.com", "live.com", "icloud.com", "yahoo.com", "proton.me", "protonmail.com"],
				"announcements_enabled": true,
				"announcements": [
					{
						"id": "2026-05-26-v1",
						"title": "GoodHR 5 更新公告",
						"content": "GoodHR 5 本地执行器版本从 5.0.0 起步，低版本请及时更新。",
						"url": "",
						"once": true,
						"enabled": true,
						"created_at": "2026-05-26"
					}
				],
				"admin_banner": {
					"enabled": true,
					"text": "GoodHR 猎头管理系统已上线（完全免费），点击前往体验。",
					"background_color": "#fff7df",
					"text_color": "#6b4a00",
					"url": "https://goodhr5.58it.cn"
				},
				"admin_banners": [
					{
						"enabled": true,
						"text": "GoodHR 猎头管理系统已上线（完全免费），点击前往体验。",
						"background_color": "#fff7df",
						"text_color": "#6b4a00",
						"url": "https://goodhr5.58it.cn"
					}
				]
			}`,
			Description: "前端公共系统配置：本地执行器版本要求、系统公告列表和后台广告位",
			Enabled:     true,
		},
		"system.subscription_plans": {
			ConfigKey: "system.subscription_plans",
			ConfigValue: `[
				{
					"id": "monthly",
					"name": "按月订阅",
					"member_type": "plus",
					"duration_days": 30,
					"original_price": 70,
					"discount_amount": 0,
					"features": ["Plus会员权益", "任务启动权限", "本地执行器联动"],
					"description": "适合短期招聘任务使用，按月开通Plus会员。",
					"created_at": "2026-05-26"
				},
				{
					"id": "quarterly",
					"name": "按季度订阅",
					"member_type": "plus",
					"duration_days": 90,
					"original_price": 210,
					"discount_amount": 30,
					"features": ["Plus会员权益", "任务启动权限", "本地执行器联动", "季度优惠"],
					"description": "适合连续招聘使用，季度订阅原价210元，优惠30元。",
					"created_at": "2026-05-26"
				},
				{
					"id": "yearly",
					"name": "按年订阅",
					"member_type": "plus",
					"duration_days": 365,
					"original_price": 840,
					"discount_amount": 240,
					"features": ["Plus会员权益", "任务启动权限", "本地执行器联动", "年度优惠"],
					"description": "适合长期招聘团队使用，年度订阅原价840元，优惠240元。",
					"created_at": "2026-05-26"
				}
			]`,
			Description: "订阅套餐配置，供前端订阅页面展示",
			Enabled:     true,
		},
		"system.onboarding_config": {
			ConfigKey: "system.onboarding_config",
			ConfigValue: `{
	"local_agent": [
		{"version": "5.0.0", "url_win": "", "url_mac": "", "sha256": "", "note": "GoodHR 本地程序安装包"}
	],
	"local_agent_console_url": "https://goodhr5.58it.cn/admin",
	"runtime_components": {
					"node_runtime": {
						"win": {"version": "22.19.0", "url": "https://oss.58it.cn/goodhr-node-runtime-win-x64.zip", "sha256": "ea3fad0e67a991d8477d8c01344b56e69c676ccb733f065b22436994b1253f86", "note": "GoodHR Node 运行环境 Windows x64"},
						"mac": {"version": "22.19.0", "url": "https://oss.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz", "sha256": "c59006db713c770d6ec63ae16cb3edc11f49ee093b5c415d667bb4f436c6526d", "note": "GoodHR Node 运行环境 macOS Apple Silicon"}
					},
					"cloakbrowser": {
						"win": {"version": "146.0.7680.177.5", "url": "https://oss.58it.cn/cloakbrowser-windows-x64.zip", "sha256": "", "note": "CloakBrowser Windows x64"},
						"mac": {"version": "145.0.7632.109.2", "url": "https://oss.58it.cn/cloakbrowser-darwin-arm64.tar.gz", "sha256": "505582aa1bd3971c577f70e0cbbe016431702bdb693529abfd943b5bd9120c1c", "note": "CloakBrowser macOS Apple Silicon"}
					},
					"ocr": {
						"win": {"version": "rapidocr-json-2.0.0", "url": "https://oss.58it.cn/goodhr-ocr-win-x64.zip", "sha256": "4209f60feb4248376c56b8b9924d7c21aaf91de5058c6daddccc6bd1e0a025f3", "note": "RapidOCR JSON Windows x64"},
						"mac": {"version": "", "url": "", "sha256": "", "note": "macOS OCR 组件待上传"}
					}
				},
				"trial_days": 3
			}`,
			Description: "新手教学配置，包含本地程序下载链接、运行组件下载链接、版本号、版本说明和注册赠送会员天数",
			Enabled:     true,
		},
		"system.invite_config": {
			ConfigKey: "system.invite_config",
			ConfigValue: `{
				"register_reward_days": 3,
				"paid_month_reward_days": 5,
				"activity_title": "邀请好友奖励会员天数",
				"activity_description": "邀请好友注册成功后，邀请人可获得注册奖励；好友充值会员后，邀请人还可按购买月份获得额外会员天数。"
			}`,
			Description: "邀请奖励配置，包含注册奖励天数和按月充值奖励天数",
			Enabled:     true,
		},
		"system.guide": {
			ConfigKey:   "system.guide",
			ConfigValue: defaultSystemGuideConfig(),
			Description: "系统指南配置，供帮助中心和 AI 助手使用",
			Enabled:     true,
		},
	}
}

// defaultSystemGuideConfig 返回帮助中心默认系统指南 JSON。
func defaultSystemGuideConfig() string {
	return `{
		"version": "2026-05-27",
		"title": "GoodHR 5 系统指南",
		"summary": "GoodHR 5 是面向招聘场景的自动打招呼工具。云端负责账号、配置、任务、订阅和 AI 决策，本地 Agent 负责浏览器控制、截图、OCR、cookie 解密和页面执行。",
		"videos": [],
		"cards": [
			{
				"id": "quick-start",
				"title": "第一次使用",
				"summary": "先启动本地程序，再创建平台账号、岗位模板和任务。",
				"content": "推荐顺序：1. 打开控制台确认本地 Agent 已连接；2. 到平台账号里扫码登录招聘平台；3. 到岗位模板里填写岗位要求、关键词和 AI 提示；4. 到个人配置里填写千问 API 地址、模型和 Key；5. 到任务列表创建并开始任务。"
			},
			{
				"id": "local-agent",
				"title": "本地程序",
				"summary": "本地 Agent 是浏览器执行器，必须保持启动。",
				"content": "前端会检测 http://127.0.0.1:55271/health。本地程序返回版本、端口、机器码和公钥。云端会记录连接信息，用于任务执行和 cookie 解密。若显示未连接，请先双击启动 GoodHRLocalAgent。"
			},
			{
				"id": "platform-account",
				"title": "平台账号",
				"summary": "平台账号保存为加密 cookie，用于自动打开招聘平台。",
				"content": "创建平台账号时，本地浏览器会打开招聘平台登录页。扫码登录后，系统导出 cookie 并加密保存。cookie 会按团队成员已连接过的本地程序公钥分别加密。新成员后加入时，旧 cookie 可能需要重新登录或更新。"
			},
			{
				"id": "task-run",
				"title": "任务运行",
				"summary": "任务会按平台、账号、岗位模板和筛选模式执行。",
				"content": "任务开始前会检查订阅是否有效，并锁定平台账号 cookie。运行中会扫描候选人、打开详情、AI 或关键词筛选、打招呼并记录日志。任务列表可查看扫描、打招呼、跳过和失败数量。"
			},
			{
				"id": "ai-config",
				"title": "AI 配置",
				"summary": "个人配置里填写 AI 地址、模型和 Key。",
				"content": "API 地址通常是 OpenAI 兼容的 chat/completions 地址，默认使用千问的 https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions。模型是实际调用的模型名，默认 qwen3.7-plus。API Key 留空保存时会保留旧 Key。岗位模板里的 AI 提示会影响打分和筛选结果。"
			},
			{
				"id": "errors",
				"title": "常见异常",
				"summary": "本地未连接、cookie 失效、AI 配置缺失是最常见问题。",
				"content": "本地未连接：检查 GoodHRLocalAgent 是否启动。cookie 解密失败：确认本机已连接，旧账号可能需要重新登录。AI 配置缺失：检查个人配置或超管配置。任务失败：展开任务日志，先看启动浏览器、准备 cookie、AI 请求和平台页面操作的错误。"
			}
		],
		"sections": [
			{
				"id": "intro",
				"title": "系统介绍",
				"items": [
					"GoodHR 5 用于帮助 HR 在招聘平台上自动筛选候选人并打招呼。",
					"系统分为云端前端、云端 Go 后端和本地 Python Agent。",
					"云端保存用户、团队、系统配置、任务、日志摘要、订阅和支付记录。",
					"本地 Agent 负责浏览器启动、页面点击、文本提取、截图、OCR、声音提醒和 cookie 解密。"
				]
			},
			{
				"id": "quick-start",
				"title": "系统使用指南",
				"items": [
					"登录后先进入控制台，确认本地 Agent 状态为已连接。",
					"进入平台账号，选择平台并扫码登录，登录成功后保存账号。",
					"进入岗位模板，填写岗位名称、岗位要求、问候语、关键词、排除词和 AI 筛选提示。",
					"进入个人配置，填写 AI API 地址、模型、API Key，以及点击详情前、关闭详情前、打招呼前和摸鱼休息参数。",
					"进入任务列表，选择平台账号、岗位模板、筛选模式和本次打招呼上限，然后点击开始。"
				]
			},
			{
				"id": "console",
				"title": "控制台说明",
				"items": [
					"控制台展示本地 Agent 连接状态、版本、WebSocket 状态、机器码和端口。",
					"如果本地版本低于系统配置要求，顶部和控制台会提醒更新。",
					"控制台还展示今日打招呼概览，便于 HR 快速知道当前自动化效果。"
				]
			},
			{
				"id": "account-cookie",
				"title": "平台账号和 Cookie",
				"items": [
					"平台账号底层使用 cookie_data 保存，不在前端暴露 cookie 明文。",
					"cookie 内容使用 AES-GCM 加密，数据密钥再用每台本地 Agent 的公钥加密。",
					"同一团队成员如果在保存 cookie 前已经连接过本地 Agent，后续可以共享解密。",
					"如果成员或电脑在 cookie 保存后才绑定，需要让可用账号重新登录或更新一次 cookie。"
				]
			},
			{
				"id": "position",
				"title": "岗位模板说明",
				"items": [
					"岗位模板包含岗位名称、问候语、岗位描述、关键词、排除词、匹配方式和 AI 配置。",
					"关键词模式会先看排除词，再按 AND 或 OR 匹配关键词。",
					"AI 模式会把岗位要求和候选人文本发送给模型，让模型返回分数和原因。",
					"详情建议分用于判断是否打开候选人详情，打招呼建议分用于判断是否发送问候。"
				]
			},
			{
				"id": "personal-config",
				"title": "个人配置说明",
				"items": [
					"API 地址是 AI 服务接口地址，必须是 chat/completions 兼容接口。",
					"模型是 AI 服务支持的模型名，默认 qwen3.7-plus。",
					"API Key 是调用 AI 的密钥，保存后前端只显示脱敏值。",
					"点击详情前延时、关闭详情前延时、打招呼前延时和摸鱼休息参数用于模拟正常人工操作节奏。"
				]
			},
			{
				"id": "task",
				"title": "任务参数说明",
				"items": [
					"平台决定任务进入哪个招聘站点，目前页面可选 Boss直聘、智联招聘、猎聘。",
					"账号是已保存的平台账号 cookie。",
					"岗位模板决定筛选条件和问候语。",
					"筛选模式包括关键词筛选和 AI 筛选。",
					"本次打招呼上限表示每次启动任务最多打招呼的人数，默认 50 个；停止后下次启动会重新按这个数量计算。"
				]
			},
			{
				"id": "subscription",
				"title": "订阅说明",
				"items": [
					"新用户注册默认赠送试用会员，赠送天数来自系统配置。",
					"开始任务前后端会校验订阅是否有效。",
					"订阅过期后，用户会被引导到订阅页面，续费后才能继续开始任务。",
					"支付记录用户可查看自己的记录，超级管理员可查看全部记录。"
				]
			},
			{
				"id": "errors",
				"title": "异常处理",
				"items": [
					"本地 Agent 未连接：确认本地程序已启动，端口 55271 到 55279 没被占用。",
					"版本过低：下载并替换新版 GoodHRLocalAgent。",
					"cookie 解密失败：确认本机已连接；旧 cookie 需要重新登录或更新。",
					"平台账号过期：重新扫码登录并保存 cookie。",
					"AI API 错误：检查 API 地址、模型名、API Key 和余额。",
					"任务卡住或失败：打开任务日志，查看浏览器启动、页面选择器、OCR、AI 请求和打招呼动作的错误。"
				]
			},
			{
				"id": "logs",
				"title": "日志和排查",
				"items": [
					"前端任务列表可以展开任务日志，查看云端编排摘要。",
					"本地程序窗口会展示 Local Agent 日志，也会写入本地日志文件。",
					"排查问题时优先看任务日志，再看本地程序日志。"
				]
			}
		]
	}`
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

// Save 保存或更新一条系统配置。
func (s *MemorySystemConfigStore) Save(cfg SystemConfig) error {
	s.configs[cfg.ConfigKey] = cfg
	return nil
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

// Save 保存或更新一条系统配置。
func (s *PostgresSystemConfigStore) Save(cfg SystemConfig) error {
	_, err := s.db.Exec(
		`INSERT INTO system_configs (config_key, config_value, description, enabled)
		 VALUES ($1, $2::jsonb, $3, $4)
		 ON CONFLICT (config_key) DO UPDATE
		 SET config_value = EXCLUDED.config_value,
		     description = EXCLUDED.description,
		     enabled = EXCLUDED.enabled`,
		cfg.ConfigKey,
		cfg.ConfigValue,
		cfg.Description,
		cfg.Enabled,
	)
	return err
}

// PlatformConfig 存储系统配置 JSON 的 value 部分。
type PlatformConfig struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Open     *bool            `json:"open,omitempty"`
	Domain   string           `json:"domain"`
	Auth     PlatformSection  `json:"auth"`
	Public   PlatformSection  `json:"public"`
	Card     PlatformCard     `json:"card"`
	Actions  PlatformActions  `json:"actions"`
	Detail   PlatformDetail   `json:"detail"`
	Position PlatformPosition `json:"position,omitempty"`
	Extras   []PlatformExtra  `json:"extras,omitempty"`
	Behavior PlatformBehavior `json:"behavior"`
}

// PlatformSection 定义平台按登录状态分组的页面配置。
type PlatformSection struct {
	Pages []PlatformPage `json:"pages"`
}

// PlatformPage 定义平台的合法页面。
type PlatformPage struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Match string `json:"match,omitempty"`
	Entry bool   `json:"entry,omitempty"`
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
	Scroll ElementLocatorConfig              `json:"scroll"`
	Item   ElementLocatorConfig              `json:"item"`
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
	Content     ElementLocatorConfig `json:"content"`
	MessageTip  ElementLocatorConfig `json:"messageTip"`
	MessageItem ElementLocatorConfig `json:"messageItem"`
}

// PlatformPosition 定义页面当前岗位读取和岗位切换定位配置。
type PlatformPosition struct {
	Current      ElementLocatorConfig `json:"current"`
	SwitchButton ElementLocatorConfig `json:"switchBtn"`
	List         ElementLocatorConfig `json:"list"`
	Item         ElementLocatorConfig `json:"item"`
	ItemText     ElementLocatorConfig `json:"itemText,omitempty"`
	SearchInput  ElementLocatorConfig `json:"searchInput,omitempty"`
	ClickTarget  ElementLocatorConfig `json:"clickTarget,omitempty"`
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
// 用于调用 Local Agent POST /api/v1/page/find-elements 时同步提取字段。
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
