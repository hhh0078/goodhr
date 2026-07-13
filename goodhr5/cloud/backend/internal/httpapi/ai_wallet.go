// 本文件负责内置 AI 钱包、用户专属 AI Key、AI 兼容中转和余额扣费。
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBuiltinAIBaseURL      = "https://goodhr5.58it.cn/api/ai-compatible/v1/chat/completions"
	defaultBuiltinAIModel        = "qwen3.7-plus"
	defaultSignupBonusCents      = 70
	defaultAIRechargeAmountCents = 500
	aiWalletUnitsPerYuan         = 10000
	aiWalletUnitsPerCent         = 100
	maxAICompatibleBodyBytes     = 8 << 20
)

// AIWalletRecord 表示一条内置 AI 余额流水。
type AIWalletRecord struct {
	ID                string
	UserEmail         string
	ChangeUnits       int64
	BalanceAfterUnits int64
	Category          string
	Reason            string
	RelatedOrderNo    string
	ModelID           string
	PromptTokens      int
	CompletionTokens  int
	CreatedAt         time.Time
}

// AIWalletStore 定义内置 AI 钱包持久化能力。
type AIWalletStore interface {
	// BalanceUnits 读取指定用户的 AI 余额，单位为 0.0001 元。
	BalanceUnits(email string) (int64, error)
	// AdjustBalance 调整指定用户的 AI 余额并写入流水。
	AdjustBalance(record AIWalletRecord) (int64, error)
	// ListRecords 读取指定用户的 AI 余额流水。
	ListRecords(email string, limit int, offset int) ([]AIWalletRecord, int, error)
	// UserEmailByAIKey 通过用户专属 AI Key 找到账号邮箱。
	UserEmailByAIKey(apiKey string) (string, error)
}

// builtinAIConfig 表示 system.app_config 中的内置 AI 配置。
type builtinAIConfig struct {
	PublicBaseURL    string           `json:"public_base_url"`
	UpstreamBaseURL  string           `json:"upstream_base_url"`
	UpstreamAPIKey   string           `json:"upstream_api_key"`
	DefaultModel     string           `json:"default_model"`
	SignupBonusCents int              `json:"signup_bonus_cents"`
	Models           []builtinAIModel `json:"models"`
}

// builtinAIModel 表示一个可用的内置 AI 模型和计费价格。
type builtinAIModel struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Description           string `json:"description"`
	InputPricePer1MCents  int    `json:"input_price_per_1m_cents"`
	OutputPricePer1MCents int    `json:"output_price_per_1m_cents"`
}

// AIWalletService 处理内置 AI 钱包、充值和 OpenAI 兼容中转请求。
type AIWalletService struct {
	auth          *AuthService
	wallet        AIWalletStore
	aiConfigs     AIConfigStore
	systemConfigs SystemConfigStore
	httpClient    *http.Client
}

// NewAIWalletService 创建内置 AI 钱包服务。
func NewAIWalletService(auth *AuthService, wallet AIWalletStore, aiConfigs AIConfigStore, systemConfigs SystemConfigStore) *AIWalletService {
	return &AIWalletService{
		auth:          auth,
		wallet:        wallet,
		aiConfigs:     aiConfigs,
		systemConfigs: systemConfigs,
		httpClient:    &http.Client{Timeout: 90 * time.Second},
	}
}

// Summary 返回当前登录用户的 AI 余额和内置 AI 配置摘要。
func (s *AIWalletService) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	balance, err := s.wallet.BalanceUnits(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load ai balance")
		return
	}
	cfg := s.loadBuiltinAIConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                     true,
		"balance_units":          balance,
		"balance_cents":          aiUnitsToCents(balance),
		"balance":                aiUnitsToYuanString(balance),
		"default_recharge_cents": defaultAIRechargeAmountCents,
		"default_model":          cfg.DefaultModel,
		"public_base_url":        cfg.PublicBaseURL,
		"models":                 cfg.Models,
	})
}

// Records 返回当前用户或超管指定用户的内置 AI 余额流水。
func (s *AIWalletService) Records(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	email := session.Email
	requestedEmail := strings.TrimSpace(r.URL.Query().Get("email"))
	if requestedEmail != "" {
		if !s.auth.IsSuperAdmin(session.Email) {
			writeError(w, http.StatusForbidden, "super admin access required")
			return
		}
		normalized, ok := normalizeEmail(requestedEmail)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid email")
			return
		}
		email = normalized
	}
	limit := boundedQueryInt(r, "page_size", 20, 1, 100)
	page := boundedQueryInt(r, "page", 1, 1, 100000)
	records, total, err := s.wallet.ListRecords(email, limit, (page-1)*limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load ai records")
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, publicAIWalletRecord(record))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"email":    email,
		"records":  items,
		"total":    total,
		"page":     page,
		"pageSize": limit,
	})
}

// UseBuiltin 将当前用户的 AI 配置切换为系统内置 AI。
func (s *AIWalletService) UseBuiltin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session invalid or expired")
		return
	}
	config, err := s.ConfigureUserBuiltinAI(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed use builtin ai")
		return
	}
	balance, err := s.wallet.BalanceUnits(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load ai balance")
		return
	}
	cfg := s.loadBuiltinAIConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"config":          publicUserAIConfig(config),
		"balance_units":   balance,
		"balance_cents":   aiUnitsToCents(balance),
		"balance":         aiUnitsToYuanString(balance),
		"default_model":   cfg.DefaultModel,
		"public_base_url": cfg.PublicBaseURL,
		"models":          cfg.Models,
	})
}

// CompatibleChat 处理 OpenAI 兼容的 Chat Completions 请求。
func (s *AIWalletService) CompatibleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	apiKey := bearerToken(r.Header.Get("Authorization"))
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "missing ai key")
		return
	}
	email, err := s.wallet.UserEmailByAIKey(apiKey)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "AI Key 不太对，我先不敢乱花钱。")
		return
	}
	balance, err := s.wallet.BalanceUnits(email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load ai balance")
		return
	}
	if balance <= 0 {
		writeError(w, http.StatusPaymentRequired, "余额有点紧张啦，先充一点我再继续干活。")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAICompatibleBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed read ai request")
		return
	}
	cfg := s.loadBuiltinAIConfig()
	if strings.TrimSpace(cfg.UpstreamBaseURL) == "" || strings.TrimSpace(cfg.UpstreamAPIKey) == "" {
		writeError(w, http.StatusServiceUnavailable, "内置 AI 还没接上线，我先小声罢工一下。")
		return
	}
	modelID := aiModelFromBody(body, cfg.DefaultModel)
	model, ok := cfg.modelByID(modelID)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":               false,
			"error":            "这个模型暂时不在可用列表里。",
			"model":            modelID,
			"supported_models": cfg.modelIDs(),
		})
		return
	}
	body = rewriteAIModel(body, model.ID)
	streamRequest := aiRequestWantsStream(body)
	if streamRequest {
		body = ensureAIStreamUsage(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, normalizeAIChatCompletionsURL(cfg.UpstreamBaseURL), bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, "AI 接口地址无效")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.UpstreamAPIKey))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "AI 服务暂时没接上，我再试也得先缓缓。")
		return
	}
	defer resp.Body.Close()
	if streamRequest && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		copyHeader(w.Header(), resp.Header)
		w.Header().Del("Content-Length")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(resp.StatusCode)
		promptTokens, completionTokens, err := proxyAIStreamAndUsage(w, resp.Body)
		if err != nil {
			log.Printf("[内置AI] 流式响应转发失败 user=%s model=%s err=%v", email, model.ID, err)
			return
		}
		if promptTokens+completionTokens == 0 {
			log.Printf("[内置AI] 流式响应未返回 usage，无法扣费 user=%s model=%s", email, model.ID)
			return
		}
		if err := s.chargeAIUsage(email, model, promptTokens, completionTokens); err != nil {
			log.Printf("[内置AI] 流式扣费记录写入失败 user=%s model=%s prompt_tokens=%d completion_tokens=%d err=%v", email, model.ID, promptTokens, completionTokens, err)
		}
		return
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAICompatibleBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed read ai response")
		return
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		promptTokens, completionTokens := aiUsageFromResponse(respBody)
		if err := s.chargeAIUsage(email, model, promptTokens, completionTokens); err != nil {
			log.Printf("[内置AI] 扣费记录写入失败 user=%s model=%s prompt_tokens=%d completion_tokens=%d err=%v", email, model.ID, promptTokens, completionTokens, err)
			writeError(w, http.StatusInternalServerError, "AI 已返回，但扣费记录没写成功。我先拦一下，免得账本乱掉。")
			return
		}
	}
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBody)
}

// chargeAIUsage 根据 token 用量扣除用户内置 AI 余额。
// email 为用户邮箱，model 为计费模型，promptTokens 和 completionTokens 为本次用量。
func (s *AIWalletService) chargeAIUsage(email string, model builtinAIModel, promptTokens int, completionTokens int) error {
	cost := aiUsageCostUnits(model, promptTokens, completionTokens)
	if cost <= 0 {
		return nil
	}
	_, err := s.wallet.AdjustBalance(AIWalletRecord{
		UserEmail:        email,
		ChangeUnits:      -cost,
		Category:         "ai_usage",
		Reason:           "内置AI调用扣费",
		ModelID:          model.ID,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	})
	return err
}

// ConfigureUserBuiltinAI 为用户保存系统内置 AI 配置，已有内置 Key 时尽量复用。
func (s *AIWalletService) ConfigureUserBuiltinAI(email string) (AIConfig, error) {
	if s == nil || s.aiConfigs == nil || s.wallet == nil {
		return AIConfig{}, errors.New("内置 AI 服务未初始化")
	}
	cfg := s.loadBuiltinAIConfig()
	current, err := s.aiConfigs.UserConfig(email)
	hasConfig := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return AIConfig{}, err
	}
	apiKey := strings.TrimSpace(current.APIKey)
	if !hasConfig || !strings.HasPrefix(apiKey, "ghai_") || strings.TrimSpace(current.BaseURL) != strings.TrimSpace(cfg.PublicBaseURL) {
		apiKey, err = generateBuiltinAIKey()
		if err != nil {
			return AIConfig{}, err
		}
	}
	saved, err := s.aiConfigs.SaveUserConfig(email, AIConfig{
		BaseURL:     cfg.PublicBaseURL,
		Model:       cfg.DefaultModel,
		APIKey:      apiKey,
		Temperature: 0,
		Enabled:     true,
	})
	if err != nil {
		return AIConfig{}, err
	}
	if memory, ok := s.wallet.(*MemoryAIWalletStore); ok {
		memory.BindAIKey(apiKey, email)
	}
	if !hasConfig {
		bonus := cfg.SignupBonusCents
		if bonus <= 0 {
			bonus = defaultSignupBonusCents
		}
		if _, err := s.wallet.AdjustBalance(AIWalletRecord{
			UserEmail:   email,
			ChangeUnits: centsToAIUnits(bonus),
			Category:    "signup_bonus",
			Reason:      "注册赠送内置AI余额",
		}); err != nil {
			return AIConfig{}, err
		}
	}
	return saved, nil
}

// EnsureUserDefaultAI 确保用户有默认内置 AI 配置，并在首次初始化时赠送余额。
func (s *AIWalletService) EnsureUserDefaultAI(email string) error {
	if s == nil || s.aiConfigs == nil || s.wallet == nil {
		return nil
	}
	if config, err := s.aiConfigs.UserConfig(email); err == nil && strings.TrimSpace(config.BaseURL) != "" && strings.TrimSpace(config.Model) != "" && strings.TrimSpace(config.APIKey) != "" {
		return nil
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	cfg := s.loadBuiltinAIConfig()
	apiKey, err := generateBuiltinAIKey()
	if err != nil {
		return err
	}
	_, err = s.aiConfigs.SaveUserConfig(email, AIConfig{
		BaseURL:     cfg.PublicBaseURL,
		Model:       cfg.DefaultModel,
		APIKey:      apiKey,
		Temperature: 0,
		Enabled:     true,
	})
	if err != nil {
		return err
	}
	if memory, ok := s.wallet.(*MemoryAIWalletStore); ok {
		memory.BindAIKey(apiKey, email)
	}
	bonus := cfg.SignupBonusCents
	if bonus <= 0 {
		bonus = defaultSignupBonusCents
	}
	_, err = s.wallet.AdjustBalance(AIWalletRecord{
		UserEmail:   email,
		ChangeUnits: centsToAIUnits(bonus),
		Category:    "signup_bonus",
		Reason:      "注册赠送内置AI余额",
	})
	return err
}

// loadBuiltinAIConfig 从 system.app_config 读取内置 AI 配置。
func (s *AIWalletService) loadBuiltinAIConfig() builtinAIConfig {
	cfg := builtinAIConfig{
		PublicBaseURL:    defaultBuiltinAIBaseURL,
		DefaultModel:     defaultBuiltinAIModel,
		SignupBonusCents: defaultSignupBonusCents,
		Models: []builtinAIModel{{
			ID:                    defaultBuiltinAIModel,
			Name:                  "通义千问 Plus",
			Description:           "适合日常筛选，先稳稳开工",
			InputPricePer1MCents:  100,
			OutputPricePer1MCents: 400,
		}},
	}
	if s == nil || s.systemConfigs == nil {
		return cfg
	}
	item, err := s.systemConfigs.Get("system.app_config")
	if err != nil {
		return cfg
	}
	var app map[string]json.RawMessage
	if json.Unmarshal([]byte(item.ConfigValue), &app) != nil {
		return cfg
	}
	raw, ok := app["builtin_ai"]
	if !ok {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg)
	if strings.TrimSpace(cfg.PublicBaseURL) == "" {
		cfg.PublicBaseURL = defaultBuiltinAIBaseURL
	}
	if strings.TrimSpace(cfg.DefaultModel) == "" {
		cfg.DefaultModel = defaultBuiltinAIModel
	}
	if cfg.SignupBonusCents <= 0 {
		cfg.SignupBonusCents = defaultSignupBonusCents
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []builtinAIModel{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, InputPricePer1MCents: 100, OutputPricePer1MCents: 400}}
	}
	return cfg
}

// modelByID 按模型 ID 读取模型计费配置。
func (c builtinAIConfig) modelByID(modelID string) (builtinAIModel, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = c.DefaultModel
	}
	for _, model := range c.Models {
		if model.ID == modelID {
			return model, true
		}
	}
	return builtinAIModel{}, false
}

// modelIDs 返回当前内置 AI 配置中允许使用的模型 ID。
func (c builtinAIConfig) modelIDs() []string {
	ids := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		if strings.TrimSpace(model.ID) != "" {
			ids = append(ids, model.ID)
		}
	}
	return ids
}

// MemoryAIWalletStore 在内存中保存 AI 余额，用于开发环境。
type MemoryAIWalletStore struct {
	mu       sync.Mutex
	balances map[string]int64
	records  []AIWalletRecord
	aiKeys   map[string]string
}

// NewMemoryAIWalletStore 创建内存 AI 钱包存储。
func NewMemoryAIWalletStore() *MemoryAIWalletStore {
	return &MemoryAIWalletStore{balances: map[string]int64{}, aiKeys: map[string]string{}}
}

// BalanceUnits 读取内存中的 AI 余额。
func (s *MemoryAIWalletStore) BalanceUnits(email string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.balances[email], nil
}

// AdjustBalance 调整内存中的 AI 余额并写流水。
func (s *MemoryAIWalletStore) AdjustBalance(record AIWalletRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.CreatedAt = time.Now()
	record.BalanceAfterUnits = s.balances[record.UserEmail] + record.ChangeUnits
	s.balances[record.UserEmail] = record.BalanceAfterUnits
	s.records = append(s.records, record)
	return record.BalanceAfterUnits, nil
}

// ListRecords 读取内存中的 AI 余额流水。
func (s *MemoryAIWalletStore) ListRecords(email string, limit int, offset int) ([]AIWalletRecord, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]AIWalletRecord, 0, len(s.records))
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		if record.UserEmail == email {
			filtered = append(filtered, record)
		}
	}
	total := len(filtered)
	if offset >= total {
		return []AIWalletRecord{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]AIWalletRecord{}, filtered[offset:end]...), total, nil
}

// UserEmailByAIKey 通过内存 AI Key 查找用户邮箱。
func (s *MemoryAIWalletStore) UserEmailByAIKey(apiKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email, ok := s.aiKeys[apiKey]
	if !ok {
		return "", ErrNotFound
	}
	return email, nil
}

// BindAIKey 在内存环境中绑定用户专属 AI Key。
func (s *MemoryAIWalletStore) BindAIKey(apiKey string, email string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aiKeys[apiKey] = email
}

// PostgresAIWalletStore 使用 PostgreSQL 保存 AI 钱包数据。
type PostgresAIWalletStore struct {
	db *sql.DB
}

// NewPostgresAIWalletStore 创建 PostgreSQL AI 钱包存储。
func NewPostgresAIWalletStore(db *sql.DB) *PostgresAIWalletStore {
	return &PostgresAIWalletStore{db: db}
}

// BalanceUnits 读取 PostgreSQL 中的 AI 余额。
func (s *PostgresAIWalletStore) BalanceUnits(email string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return 0, err
	}
	var balance int64
	err := s.db.QueryRowContext(ctx, `SELECT ai_balance_units FROM users WHERE email=$1`, email).Scan(&balance)
	return balance, err
}

// AdjustBalance 调整 PostgreSQL 中的 AI 余额并写流水。
func (s *PostgresAIWalletStore) AdjustBalance(record AIWalletRecord) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	userID, err := ensureUserID(ctx, s.db, record.UserEmail)
	if err != nil {
		return 0, err
	}
	var balance int64
	err = tx.QueryRowContext(ctx, `UPDATE users SET ai_balance_units=ai_balance_units+$2 WHERE id=$1 RETURNING ai_balance_units`, userID, record.ChangeUnits).Scan(&balance)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_balance_records (
			user_id, user_email, change_cents, balance_after_cents, change_units, balance_after_units, category, reason,
			related_order_no, model_id, prompt_tokens, completion_tokens
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`, userID, record.UserEmail, aiUnitsToCents(record.ChangeUnits), aiUnitsToCents(balance), record.ChangeUnits, balance, record.Category, record.Reason, record.RelatedOrderNo, record.ModelID, record.PromptTokens, record.CompletionTokens)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return balance, nil
}

// ListRecords 读取 PostgreSQL 中的 AI 余额流水。
func (s *PostgresAIWalletStore) ListRecords(email string, limit int, offset int) ([]AIWalletRecord, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_balance_records WHERE user_email=$1`, email).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
	SELECT id::text, user_email, change_units, balance_after_units, category, reason,
		       related_order_no, model_id, prompt_tokens, completion_tokens, created_at
		FROM ai_balance_records
		WHERE user_email=$1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, email, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	records := []AIWalletRecord{}
	for rows.Next() {
		var record AIWalletRecord
		if err := rows.Scan(&record.ID, &record.UserEmail, &record.ChangeUnits, &record.BalanceAfterUnits, &record.Category, &record.Reason, &record.RelatedOrderNo, &record.ModelID, &record.PromptTokens, &record.CompletionTokens, &record.CreatedAt); err != nil {
			return nil, 0, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

// UserEmailByAIKey 通过 PostgreSQL 中的 AI Key 查找用户邮箱。
func (s *PostgresAIWalletStore) UserEmailByAIKey(apiKey string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var email string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.email
		FROM user_ai_configs ai
		INNER JOIN users u ON u.id=ai.user_id
		WHERE ai.api_key_encrypted=$1 AND ai.enabled=true
	`, apiKey).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// generateBuiltinAIKey 生成用户专属内置 AI Key。
func generateBuiltinAIKey() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ghai_" + hex.EncodeToString(buf), nil
}

// aiModelFromBody 从 OpenAI 兼容请求体中读取模型 ID。
func aiModelFromBody(body []byte, fallback string) string {
	var payload struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &payload)
	if strings.TrimSpace(payload.Model) == "" {
		return fallback
	}
	return strings.TrimSpace(payload.Model)
}

// rewriteAIModel 将请求体中的模型统一改成系统允许的模型 ID。
func rewriteAIModel(body []byte, modelID string) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	payload["model"] = modelID
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

// aiRequestWantsStream 判断 OpenAI 兼容请求是否启用了流式输出。
// body 为原始请求体 JSON。
func aiRequestWantsStream(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	return payload.Stream
}

// ensureAIStreamUsage 为流式请求补充 usage 返回开关。
// body 为原始请求体 JSON，返回补充后的请求体。
func ensureAIStreamUsage(body []byte) []byte {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	options := mapFromAny(payload["stream_options"])
	options["include_usage"] = true
	payload["stream_options"] = options
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

// mapFromAny 将任意值安全转换为字符串键 map。
// value 为原始 JSON 字段值，无法转换时返回空 map。
func mapFromAny(value any) map[string]any {
	if item, ok := value.(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

// proxyAIStreamAndUsage 转发上游 SSE 流，并从分片中提取 token 用量。
// w 为客户端响应，reader 为上游响应体，返回最终读取到的 token 用量。
func proxyAIStreamAndUsage(w http.ResponseWriter, reader io.Reader) (int, int, error) {
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxAICompatibleBodyBytes)
	promptTokens := 0
	completionTokens := 0
	for scanner.Scan() {
		line := scanner.Text()
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return promptTokens, completionTokens, err
		}
		if flusher != nil {
			flusher.Flush()
		}
		if prompt, completion := aiUsageFromStreamLine(line); prompt+completion > 0 {
			promptTokens = prompt
			completionTokens = completion
		}
	}
	if err := scanner.Err(); err != nil {
		return promptTokens, completionTokens, err
	}
	return promptTokens, completionTokens, nil
}

// aiUsageFromStreamLine 从单行 SSE data 中读取 token 用量。
// line 为上游流式响应中的一行文本。
func aiUsageFromStreamLine(line string) (int, int) {
	data := strings.TrimSpace(line)
	if !strings.HasPrefix(data, "data:") {
		return 0, 0
	}
	data = strings.TrimSpace(strings.TrimPrefix(data, "data:"))
	if data == "" || data == "[DONE]" {
		return 0, 0
	}
	return aiUsageFromResponse([]byte(data))
}

// aiUsageFromResponse 从 OpenAI 兼容响应中读取 token 用量。
func aiUsageFromResponse(body []byte) (int, int) {
	var payload struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return 0, 0
	}
	return payload.Usage.PromptTokens, payload.Usage.CompletionTokens
}

// aiUsageCostUnits 按模型价格和 token 用量计算本次费用，单位为 0.0001 元。
func aiUsageCostUnits(model builtinAIModel, promptTokens int, completionTokens int) int64 {
	cost := float64(promptTokens)*float64(model.InputPricePer1MCents)*aiWalletUnitsPerCent/1_000_000 +
		float64(completionTokens)*float64(model.OutputPricePer1MCents)*aiWalletUnitsPerCent/1_000_000
	if cost <= 0 && promptTokens+completionTokens > 0 {
		return 1
	}
	return int64(math.Ceil(cost))
}

// publicAIWalletRecord 将 AI 余额流水转换为前端展示结构。
func publicAIWalletRecord(record AIWalletRecord) map[string]any {
	return map[string]any{
		"id":                  record.ID,
		"user_email":          record.UserEmail,
		"change_units":        record.ChangeUnits,
		"change_cents":        aiUnitsToCents(record.ChangeUnits),
		"change":              aiUnitsToYuanString(record.ChangeUnits),
		"balance_after_units": record.BalanceAfterUnits,
		"balance_after_cents": aiUnitsToCents(record.BalanceAfterUnits),
		"balance_after":       aiUnitsToYuanString(record.BalanceAfterUnits),
		"category":            record.Category,
		"reason":              record.Reason,
		"related_order_no":    record.RelatedOrderNo,
		"model_id":            record.ModelID,
		"prompt_tokens":       record.PromptTokens,
		"completion_tokens":   record.CompletionTokens,
		"total_tokens":        record.PromptTokens + record.CompletionTokens,
		"created_at":          record.CreatedAt,
	}
}

// boundedQueryInt 读取带上下限的分页参数。
func boundedQueryInt(r *http.Request, key string, fallback int, minValue int, maxValue int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value < minValue {
		return fallback
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// copyHeader 复制上游响应头。
func copyHeader(dst http.Header, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
