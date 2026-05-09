package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("not found")

type Account struct {
	ID                  int64           `json:"id"`
	Identifier          string          `json:"identifier"`
	IdentityType        string          `json:"identity_type"`
	InviterID           *int64          `json:"inviter_id,omitempty"`
	Phone               string          `json:"phone,omitempty"`
	Email               string          `json:"email,omitempty"`
	Balance             float64         `json:"balance"`
	Status              string          `json:"status"`
	RunMode             string          `json:"run_mode"`
	CurrentPositionName string          `json:"current_position_name"`
	AIExpireTime        string          `json:"ai_expire_time,omitempty"`
	IsAndMode           bool            `json:"is_and_mode"`
	MatchLimit          int             `json:"match_limit"`
	EnableSound         bool            `json:"enable_sound"`
	ScrollDelayMin      int             `json:"scroll_delay_min"`
	ScrollDelayMax      int             `json:"scroll_delay_max"`
	ClickFrequency      int             `json:"click_frequency"`
	CollectPhone        bool            `json:"collect_phone"`
	CollectWechat       bool            `json:"collect_wechat"`
	CollectResume       bool            `json:"collect_resume"`
	CommunicationOn     bool            `json:"communication_enabled"`
	GreetingOn          bool            `json:"greeting_enabled"`
	CompanyInfoContent  string          `json:"company_info_content"`
	JobExtraInfo        string          `json:"job_extra_info"`
	AIModel             string          `json:"ai_model"`
	AIClickPrompt       string          `json:"ai_click_prompt"`
	AIContactPrompt     *string         `json:"ai_contact_prompt"`
	APIKey              string          `json:"api_key,omitempty"`
	Positions           json.RawMessage `json:"positions"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type SystemConfig struct {
	ConfigKey   string          `json:"config_key"`
	ConfigValue json.RawMessage `json:"config_value"`
	Description string          `json:"description"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type UpdateRecord struct {
	ID          int64     `json:"id"`
	Version     string    `json:"version"`
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	ForceUpdate bool      `json:"force_update"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Store struct {
	db    *pgxpool.Pool
	redis *redis.Client
}

func New(db *pgxpool.Pool, redisClient *redis.Client) *Store {
	return &Store{db: db, redis: redisClient}
}

func InferIdentityType(identifier string) string {
	if strings.Contains(identifier, "@") {
		return "email"
	}
	return "phone"
}

func SplitIdentifier(identifier string) (phone, email string) {
	if InferIdentityType(identifier) == "email" {
		return "", identifier
	}
	return identifier, ""
}

func (s *Store) FindAccount(ctx context.Context, identifier string) (Account, error) {
	const query = `
select
  id, identifier, identity_type, inviter_id, phone, email, balance, status, run_mode,
  current_position_name, coalesce(ai_expire_time::text, ''), is_and_mode, match_limit, enable_sound,
  scroll_delay_min, scroll_delay_max, click_frequency,
  collect_phone, collect_wechat, collect_resume,
  communication_enabled, greeting_enabled,
  company_info_content, job_extra_info,
  ai_model, ai_click_prompt, ai_contact_prompt, api_key,
  positions, created_at, updated_at
from accounts
where identifier = $1`

	var account Account
	err := s.db.QueryRow(ctx, query, identifier).Scan(
		&account.ID,
		&account.Identifier,
		&account.IdentityType,
		&account.InviterID,
		&account.Phone,
		&account.Email,
		&account.Balance,
		&account.Status,
		&account.RunMode,
		&account.CurrentPositionName,
		&account.AIExpireTime,
		&account.IsAndMode,
		&account.MatchLimit,
		&account.EnableSound,
		&account.ScrollDelayMin,
		&account.ScrollDelayMax,
		&account.ClickFrequency,
		&account.CollectPhone,
		&account.CollectWechat,
		&account.CollectResume,
		&account.CommunicationOn,
		&account.GreetingOn,
		&account.CompanyInfoContent,
		&account.JobExtraInfo,
		&account.AIModel,
		&account.AIClickPrompt,
		&account.AIContactPrompt,
		&account.APIKey,
		&account.Positions,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrNotFound
		}
		return Account{}, err
	}
	return account, nil
}

func (s *Store) CreateAccount(ctx context.Context, account Account) (Account, error) {
	const query = `
insert into accounts (
  identifier, identity_type, inviter_id, phone, email, balance, status, run_mode,
  current_position_name, ai_expire_time, is_and_mode, match_limit, enable_sound,
  scroll_delay_min, scroll_delay_max, click_frequency,
  collect_phone, collect_wechat, collect_resume,
  communication_enabled, greeting_enabled,
  company_info_content, job_extra_info,
  ai_model, ai_click_prompt, ai_contact_prompt, api_key, positions
) values (
  $1,$2,$3,$4,$5,$6,$7,$8,
  $9,$10,$11,$12,$13,
  $14,$15,$16,
  $17,$18,$19,
  $20,$21,
  $22,$23,
  $24,$25,$26,$27,$28
)
returning
  id, identifier, identity_type, inviter_id, phone, email, balance, status, run_mode,
  current_position_name, coalesce(ai_expire_time::text, ''), is_and_mode, match_limit, enable_sound,
  scroll_delay_min, scroll_delay_max, click_frequency,
  collect_phone, collect_wechat, collect_resume,
  communication_enabled, greeting_enabled,
  company_info_content, job_extra_info,
  ai_model, ai_click_prompt, ai_contact_prompt, api_key,
  positions, created_at, updated_at`

	return s.queryAccount(ctx, query, account)
}

func (s *Store) UpsertAccount(ctx context.Context, account Account) (Account, error) {
	const query = `
insert into accounts (
  identifier, identity_type, inviter_id, phone, email, balance, status, run_mode,
  current_position_name, ai_expire_time, is_and_mode, match_limit, enable_sound,
  scroll_delay_min, scroll_delay_max, click_frequency,
  collect_phone, collect_wechat, collect_resume,
  communication_enabled, greeting_enabled,
  company_info_content, job_extra_info,
  ai_model, ai_click_prompt, ai_contact_prompt, api_key, positions
) values (
  $1,$2,$3,$4,$5,$6,$7,$8,
  $9,$10,$11,$12,$13,
  $14,$15,$16,
  $17,$18,$19,
  $20,$21,
  $22,$23,
  $24,$25,$26,$27,$28
)
on conflict (identifier) do update set
  identity_type = excluded.identity_type,
  inviter_id = excluded.inviter_id,
  phone = excluded.phone,
  email = excluded.email,
  balance = excluded.balance,
  status = excluded.status,
  run_mode = excluded.run_mode,
  current_position_name = excluded.current_position_name,
  ai_expire_time = excluded.ai_expire_time,
  is_and_mode = excluded.is_and_mode,
  match_limit = excluded.match_limit,
  enable_sound = excluded.enable_sound,
  scroll_delay_min = excluded.scroll_delay_min,
  scroll_delay_max = excluded.scroll_delay_max,
  click_frequency = excluded.click_frequency,
  collect_phone = excluded.collect_phone,
  collect_wechat = excluded.collect_wechat,
  collect_resume = excluded.collect_resume,
  communication_enabled = excluded.communication_enabled,
  greeting_enabled = excluded.greeting_enabled,
  company_info_content = excluded.company_info_content,
  job_extra_info = excluded.job_extra_info,
  ai_model = excluded.ai_model,
  ai_click_prompt = excluded.ai_click_prompt,
  ai_contact_prompt = excluded.ai_contact_prompt,
  api_key = excluded.api_key,
  positions = excluded.positions,
  updated_at = now()
returning
  id, identifier, identity_type, inviter_id, phone, email, balance, status, run_mode,
  current_position_name, coalesce(ai_expire_time::text, ''), is_and_mode, match_limit, enable_sound,
  scroll_delay_min, scroll_delay_max, click_frequency,
  collect_phone, collect_wechat, collect_resume,
  communication_enabled, greeting_enabled,
  company_info_content, job_extra_info,
  ai_model, ai_click_prompt, ai_contact_prompt, api_key,
  positions, created_at, updated_at`

	return s.queryAccount(ctx, query, account)
}

func (s *Store) queryAccount(ctx context.Context, query string, account Account) (Account, error) {
	var result Account
	err := s.db.QueryRow(
		ctx,
		query,
		account.Identifier,
		account.IdentityType,
		account.InviterID,
		account.Phone,
		account.Email,
		account.Balance,
		account.Status,
		account.RunMode,
		account.CurrentPositionName,
		account.AIExpireTime,
		account.IsAndMode,
		account.MatchLimit,
		account.EnableSound,
		account.ScrollDelayMin,
		account.ScrollDelayMax,
		account.ClickFrequency,
		account.CollectPhone,
		account.CollectWechat,
		account.CollectResume,
		account.CommunicationOn,
		account.GreetingOn,
		account.CompanyInfoContent,
		account.JobExtraInfo,
		account.AIModel,
		account.AIClickPrompt,
		account.AIContactPrompt,
		account.APIKey,
		account.Positions,
	).Scan(
		&result.ID,
		&result.Identifier,
		&result.IdentityType,
		&result.InviterID,
		&result.Phone,
		&result.Email,
		&result.Balance,
		&result.Status,
		&result.RunMode,
		&result.CurrentPositionName,
		&result.AIExpireTime,
		&result.IsAndMode,
		&result.MatchLimit,
		&result.EnableSound,
		&result.ScrollDelayMin,
		&result.ScrollDelayMax,
		&result.ClickFrequency,
		&result.CollectPhone,
		&result.CollectWechat,
		&result.CollectResume,
		&result.CommunicationOn,
		&result.GreetingOn,
		&result.CompanyInfoContent,
		&result.JobExtraInfo,
		&result.AIModel,
		&result.AIClickPrompt,
		&result.AIContactPrompt,
		&result.APIKey,
		&result.Positions,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	return result, err
}

func (s *Store) GetSystemConfig(ctx context.Context, key string) (SystemConfig, error) {
	const query = `select config_key, config_value, description, updated_at from system_configs where config_key = $1`
	var cfg SystemConfig
	err := s.db.QueryRow(ctx, query, key).Scan(&cfg.ConfigKey, &cfg.ConfigValue, &cfg.Description, &cfg.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SystemConfig{}, ErrNotFound
		}
		return SystemConfig{}, err
	}
	return cfg, nil
}

func (s *Store) ListSystemConfigsByPrefix(ctx context.Context, prefix string) ([]SystemConfig, error) {
	const query = `
select config_key, config_value, description, updated_at
from system_configs
where config_key like $1 || '.%'
order by config_key`

	rows, err := s.db.Query(ctx, query, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []SystemConfig
	for rows.Next() {
		var cfg SystemConfig
		if err := rows.Scan(&cfg.ConfigKey, &cfg.ConfigValue, &cfg.Description, &cfg.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return configs, nil
}

func (s *Store) ListUpdateRecords(ctx context.Context, limit int) ([]UpdateRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	const query = `
select id, version, title, content, force_update, published_at, created_at, updated_at
from update_records
order by published_at desc, id desc
limit $1`

	rows, err := s.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []UpdateRecord
	for rows.Next() {
		var item UpdateRecord
		if err := rows.Scan(
			&item.ID,
			&item.Version,
			&item.Title,
			&item.Content,
			&item.ForceUpdate,
			&item.PublishedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *Store) CacheUser(ctx context.Context, identifier string, payload []byte, ttl time.Duration) error {
	return s.redis.Set(ctx, "goodhr4:user:"+identifier, payload, ttl).Err()
}

func (s *Store) ReadCachedUser(ctx context.Context, identifier string) ([]byte, error) {
	return s.redis.Get(ctx, "goodhr4:user:"+identifier).Bytes()
}

func (s *Store) DeleteUserCache(ctx context.Context, identifier string) error {
	return s.redis.Del(ctx, "goodhr4:user:"+identifier).Err()
}

func (s *Store) CacheSystemConfig(ctx context.Context, key string, payload []byte, ttl time.Duration) error {
	return s.redis.Set(ctx, "goodhr4:system:"+key, payload, ttl).Err()
}

func (s *Store) ReadCachedSystemConfig(ctx context.Context, key string) ([]byte, error) {
	return s.redis.Get(ctx, "goodhr4:system:"+key).Bytes()
}

func (s *Store) SaveSession(ctx context.Context, identifier string, ttl time.Duration) (string, error) {
	token := uuid.NewString()
	if err := s.redis.Set(ctx, "goodhr4:session:"+token, identifier, ttl).Err(); err != nil {
		return "", err
	}
	return token, nil
}
