package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goodhr4/backend/internal/store"
)

type Service struct {
	store      *store.Store
	sessionTTL time.Duration
}

const defaultAIPlatform = "siliconflow"

type SiteBootstrap struct {
	Config  map[string]any       `json:"config"`
	Updates []store.UpdateRecord `json:"updates"`
}

type SettingsPayload struct {
	RunMode             string           `json:"runMode"`
	Positions           json.RawMessage  `json:"positions"`
	CurrentPositionName string           `json:"currentPositionName"`
	IsAndMode           bool             `json:"isAndMode"`
	MatchLimit          int              `json:"matchLimit"`
	EnableSound         bool             `json:"enableSound"`
	ScrollDelayMin      int              `json:"scrollDelayMin"`
	ScrollDelayMax      int              `json:"scrollDelayMax"`
	ClickFrequency      int              `json:"clickFrequency"`
	CommunicationConfig CommunicationCfg `json:"communicationConfig"`
	CompanyInfo         CompanyInfo      `json:"companyInfo"`
	JobInfo             JobInfo          `json:"jobInfo"`
	RunModeConfig       RunModeCfg       `json:"runModeConfig"`
	AIConfig            AIConfig         `json:"ai_config"`
	AIExpireTime        string           `json:"ai_expire_time"`
	ExtraSettings       json.RawMessage  `json:"extra_settings,omitempty"`
}

type CommunicationCfg struct {
	CollectPhone  bool `json:"collectPhone"`
	CollectResume bool `json:"collectResume"`
	CollectWechat bool `json:"collectWechat"`
}

type CompanyInfo struct {
	Content string `json:"content"`
}

type JobInfo struct {
	ExtraInfo string `json:"extraInfo"`
}

type RunModeCfg struct {
	CommunicationEnabled bool `json:"communicationEnabled"`
	GreetingEnabled      bool `json:"greetingEnabled"`
}

type AIConfig struct {
	Token         string  `json:"token"`
	Model         string  `json:"model"`
	ClickPrompt   string  `json:"clickPrompt"`
	ContactPrompt *string `json:"contactPrompt"`
	Platform      string  `json:"platform"`
}

func New(st *store.Store, sessionTTL time.Duration) *Service {
	return &Service{store: st, sessionTTL: sessionTTL}
}

func DefaultSettings() SettingsPayload {
	return SettingsPayload{
		RunMode:             "ai",
		Positions:           json.RawMessage(`[]`),
		CurrentPositionName: "",
		IsAndMode:           false,
		MatchLimit:          60,
		EnableSound:         true,
		ScrollDelayMin:      3,
		ScrollDelayMax:      8,
		ClickFrequency:      7,
		CommunicationConfig: CommunicationCfg{CollectPhone: true, CollectResume: true, CollectWechat: true},
		CompanyInfo:         CompanyInfo{Content: ""},
		JobInfo:             JobInfo{ExtraInfo: ""},
		RunModeConfig:       RunModeCfg{CommunicationEnabled: true, GreetingEnabled: true},
		AIConfig: AIConfig{
			Token:       "",
			Model:       "gpt-5.1-chat",
			ClickPrompt: "",
			Platform:    defaultAIPlatform,
		},
		AIExpireTime:  "2099-10-30",
		ExtraSettings: json.RawMessage(`{}`),
	}
}

func (s *Service) Bind(ctx context.Context, identifier string) (store.Account, SettingsPayload, string, bool, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return store.Account{}, SettingsPayload{}, "", false, ErrBadIdentifier
	}

	if cached, err := s.store.ReadCachedUser(ctx, identifier); err == nil {
		var cachedPayload struct {
			Account  store.Account   `json:"account"`
			Settings SettingsPayload `json:"settings"`
		}
		if json.Unmarshal(cached, &cachedPayload) == nil {
			token, tokenErr := s.store.SaveSession(ctx, identifier, s.sessionTTL)
			return cachedPayload.Account, cachedPayload.Settings, token, false, tokenErr
		}
	}

	account, err := s.store.FindAccount(ctx, identifier)
	created := false
	if err != nil {
		defaults := DefaultSettings()
		account = buildAccount(identifier, defaults, 0, "active")
		account, err = s.store.CreateAccount(ctx, account)
		if err != nil {
			return store.Account{}, SettingsPayload{}, "", false, err
		}
		created = true
	}

	settings := composeSettings(account)
	_ = s.cacheUserPayload(ctx, identifier, account, settings)
	token, err := s.store.SaveSession(ctx, identifier, s.sessionTTL)
	if err != nil {
		return store.Account{}, SettingsPayload{}, "", false, err
	}
	return account, settings, token, created, nil
}

func (s *Service) RegisterSite(ctx context.Context, identifier string, inviterID *int64) (store.Account, bool, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return store.Account{}, false, ErrBadIdentifier
	}

	account, err := s.store.FindAccount(ctx, identifier)
	created := false
	if err != nil {
		if err != store.ErrNotFound {
			return store.Account{}, false, err
		}
		defaults := DefaultSettings()
		account = buildAccount(identifier, defaults, 0, "active")
		account.InviterID = inviterID
		account, err = s.store.CreateAccount(ctx, account)
		if err != nil {
			return store.Account{}, false, err
		}
		created = true
	} else if inviterID != nil {
		settings := composeSettings(account)
		updated := buildAccount(identifier, settings, account.Balance, account.Status)
		updated.InviterID = inviterID
		account, err = s.store.UpsertAccount(ctx, updated)
		if err != nil {
			return store.Account{}, false, err
		}
	}

	settings := composeSettings(account)
	_ = s.cacheUserPayload(ctx, identifier, account, settings)
	return account, created, nil
}

func (s *Service) GetSettings(ctx context.Context, identifier string) (store.Account, SettingsPayload, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return store.Account{}, SettingsPayload{}, ErrBadIdentifier
	}

	if cached, err := s.store.ReadCachedUser(ctx, identifier); err == nil {
		var cachedPayload struct {
			Account  store.Account   `json:"account"`
			Settings SettingsPayload `json:"settings"`
		}
		if json.Unmarshal(cached, &cachedPayload) == nil {
			return cachedPayload.Account, cachedPayload.Settings, nil
		}
	}

	account, err := s.store.FindAccount(ctx, identifier)
	if err != nil {
		return store.Account{}, SettingsPayload{}, err
	}
	settings := composeSettings(account)
	_ = s.cacheUserPayload(ctx, identifier, account, settings)
	return account, settings, nil
}

func (s *Service) SaveSettings(ctx context.Context, identifier string, payload any) (store.Account, SettingsPayload, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return store.Account{}, SettingsPayload{}, ErrBadIdentifier
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return store.Account{}, SettingsPayload{}, err
	}

	var settings SettingsPayload
	if err := json.Unmarshal(raw, &settings); err != nil {
		return store.Account{}, SettingsPayload{}, err
	}

	current, err := s.store.FindAccount(ctx, identifier)
	if err != nil && err != store.ErrNotFound {
		return store.Account{}, SettingsPayload{}, err
	}

	account := buildAccount(identifier, settings, current.Balance, current.Status)
	account.InviterID = current.InviterID
	if account.Status == "" {
		account.Status = "active"
	}

	account, err = s.store.UpsertAccount(ctx, account)
	if err != nil {
		return store.Account{}, SettingsPayload{}, err
	}
	_ = s.store.DeleteUserCache(ctx, identifier)
	composed := composeSettings(account)
	return account, composed, nil
}

func (s *Service) GetSystemConfig(ctx context.Context, key string) (store.SystemConfig, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "frontend"
	}

	if cached, err := s.store.ReadCachedSystemConfig(ctx, key); err == nil {
		var cfg store.SystemConfig
		if json.Unmarshal(cached, &cfg) == nil {
			return cfg, nil
		}
	}

	prefixed, err := s.store.ListSystemConfigsByPrefix(ctx, key)
	if err != nil {
		return store.SystemConfig{}, err
	}
	if len(prefixed) > 0 {
		cfg, err := composeSystemConfig(key, prefixed)
		if err != nil {
			return store.SystemConfig{}, err
		}
		if encoded, err := json.Marshal(cfg); err == nil {
			_ = s.store.CacheSystemConfig(ctx, key, encoded, s.sessionTTL)
		}
		return cfg, nil
	}

	cfg, err := s.store.GetSystemConfig(ctx, key)
	if err != nil {
		return store.SystemConfig{}, err
	}
	if encoded, err := json.Marshal(cfg); err == nil {
		_ = s.store.CacheSystemConfig(ctx, key, encoded, s.sessionTTL)
	}
	return cfg, nil
}

func (s *Service) ListUpdateRecords(ctx context.Context, limit int) ([]store.UpdateRecord, error) {
	return s.store.ListUpdateRecords(ctx, limit)
}

func (s *Service) GetSiteBootstrap(ctx context.Context, updateLimit int) (SiteBootstrap, error) {
	cfg, err := s.GetSystemConfig(ctx, "frontend")
	if err != nil {
		return SiteBootstrap{}, err
	}

	var config map[string]any
	if err := json.Unmarshal(cfg.ConfigValue, &config); err != nil {
		return SiteBootstrap{}, err
	}
	updates, err := s.ListUpdateRecords(ctx, updateLimit)
	if err != nil {
		return SiteBootstrap{}, err
	}
	return SiteBootstrap{
		Config:  config,
		Updates: updates,
	}, nil
}

func composeSystemConfig(root string, parts []store.SystemConfig) (store.SystemConfig, error) {
	payload := make(map[string]any, len(parts))
	description := "聚合系统配置"
	var updatedAt time.Time

	for _, part := range parts {
		childKey := strings.TrimPrefix(part.ConfigKey, root+".")
		if childKey == part.ConfigKey || childKey == "" {
			continue
		}

		var value any
		if err := json.Unmarshal(part.ConfigValue, &value); err != nil {
			return store.SystemConfig{}, fmt.Errorf("decode system config %s: %w", part.ConfigKey, err)
		}
		payload[childKey] = value
		if part.Description != "" {
			description = part.Description
		}
		if part.UpdatedAt.After(updatedAt) {
			updatedAt = part.UpdatedAt
		}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return store.SystemConfig{}, err
	}

	return store.SystemConfig{
		ConfigKey:   root,
		ConfigValue: encoded,
		Description: description,
		UpdatedAt:   updatedAt,
	}, nil
}

func (s *Service) cacheUserPayload(ctx context.Context, identifier string, account store.Account, settings SettingsPayload) error {
	payload, err := json.Marshal(map[string]any{
		"account":  account,
		"settings": settings,
	})
	if err != nil {
		return err
	}
	return s.store.CacheUser(ctx, identifier, payload, s.sessionTTL)
}

func buildAccount(identifier string, settings SettingsPayload, balance float64, status string) store.Account {
	phone, email := store.SplitIdentifier(identifier)
	if settings.Positions == nil {
		settings.Positions = json.RawMessage(`[]`)
	}
	if settings.ExtraSettings == nil {
		settings.ExtraSettings = json.RawMessage(`{}`)
	}
	return store.Account{
		Identifier:          identifier,
		IdentityType:        store.InferIdentityType(identifier),
		Phone:               phone,
		Email:               email,
		Balance:             balance,
		Status:              status,
		RunMode:             settings.RunMode,
		CurrentPositionName: settings.CurrentPositionName,
		AIExpireTime:        settings.AIExpireTime,
		IsAndMode:           settings.IsAndMode,
		MatchLimit:          settings.MatchLimit,
		EnableSound:         settings.EnableSound,
		ScrollDelayMin:      settings.ScrollDelayMin,
		ScrollDelayMax:      settings.ScrollDelayMax,
		ClickFrequency:      settings.ClickFrequency,
		CollectPhone:        settings.CommunicationConfig.CollectPhone,
		CollectWechat:       settings.CommunicationConfig.CollectWechat,
		CollectResume:       settings.CommunicationConfig.CollectResume,
		CommunicationOn:     settings.RunModeConfig.CommunicationEnabled,
		GreetingOn:          settings.RunModeConfig.GreetingEnabled,
		CompanyInfoContent:  settings.CompanyInfo.Content,
		JobExtraInfo:        settings.JobInfo.ExtraInfo,
		AIModel:             settings.AIConfig.Model,
		AIClickPrompt:       settings.AIConfig.ClickPrompt,
		AIContactPrompt:     settings.AIConfig.ContactPrompt,
		Positions:           settings.Positions,
	}
}

func composeSettings(account store.Account) SettingsPayload {
	positions := account.Positions
	if len(positions) == 0 {
		positions = json.RawMessage(`[]`)
	}
	return SettingsPayload{
		RunMode:             account.RunMode,
		Positions:           positions,
		CurrentPositionName: account.CurrentPositionName,
		IsAndMode:           account.IsAndMode,
		MatchLimit:          account.MatchLimit,
		EnableSound:         account.EnableSound,
		ScrollDelayMin:      account.ScrollDelayMin,
		ScrollDelayMax:      account.ScrollDelayMax,
		ClickFrequency:      account.ClickFrequency,
		CommunicationConfig: CommunicationCfg{
			CollectPhone:  account.CollectPhone,
			CollectResume: account.CollectResume,
			CollectWechat: account.CollectWechat,
		},
		CompanyInfo:   CompanyInfo{Content: account.CompanyInfoContent},
		JobInfo:       JobInfo{ExtraInfo: account.JobExtraInfo},
		RunModeConfig: RunModeCfg{CommunicationEnabled: account.CommunicationOn, GreetingEnabled: account.GreetingOn},
		AIConfig: AIConfig{
			Token:         "",
			Model:         account.AIModel,
			ClickPrompt:   account.AIClickPrompt,
			ContactPrompt: account.AIContactPrompt,
			Platform:      defaultAIPlatform,
		},
		AIExpireTime:  account.AIExpireTime,
		ExtraSettings: json.RawMessage(`{}`),
	}
}
