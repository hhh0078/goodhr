// 本文件负责从环境变量加载云端后端配置，并创建云端后端依赖。
package httpapi

import (
	"database/sql"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type Config struct {
	PostgresDSN   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	SMTPHost      string
	SMTPPort      int
	SMTPUsername  string
	SMTPPassword  string
	SMTPFrom      string
}

// LoadConfigFromEnv 从环境变量读取云端后端配置。
func LoadConfigFromEnv() Config {
	return Config{
		PostgresDSN:   os.Getenv("GOODHR_PG_DSN"),
		RedisAddr:     os.Getenv("GOODHR_REDIS_ADDR"),
		RedisPassword: os.Getenv("GOODHR_REDIS_PASSWORD"),
		RedisDB:       envInt("GOODHR_REDIS_DB", 0),
		SMTPHost:      os.Getenv("GOODHR_SMTP_HOST"),
		SMTPPort:      envInt("GOODHR_SMTP_PORT", 465),
		SMTPUsername:  os.Getenv("GOODHR_SMTP_USERNAME"),
		SMTPPassword:  os.Getenv("GOODHR_SMTP_PASSWORD"),
		SMTPFrom:      os.Getenv("GOODHR_SMTP_FROM"),
	}
}

// PostgresDB 按环境变量创建 PostgreSQL 连接；未配置时返回 nil。
func (c Config) PostgresDB() (*sql.DB, error) {
	if c.PostgresDSN == "" {
		return nil, nil
	}

	db, err := sql.Open("postgres", c.PostgresDSN)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	// 调用数据库连接的 Ping，保证显式开启 PostgreSQL 时启动阶段就能发现配置错误。
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// AuthStore 创建认证存储；配置 Redis 时使用 Redis，否则使用内存实现。
func (c Config) AuthStore() AuthStore {
	if c.RedisAddr != "" {
		return NewRedisAuthStore(c.RedisAddr, c.RedisPassword, c.RedisDB)
	}
	return NewMemoryAuthStore()
}

// Mailer 创建验证码发信器；配置 SMTP 时真实发信，否则使用开发模式。
func (c Config) Mailer() (Mailer, bool) {
	if c.SMTPHost != "" && c.SMTPUsername != "" && c.SMTPPassword != "" {
		return SMTPMailer{
			Host:     c.SMTPHost,
			Port:     c.SMTPPort,
			Username: c.SMTPUsername,
			Password: c.SMTPPassword,
			From:     c.SMTPFrom,
		}, false
	}
	return DevMailer{}, true
}

// AgentStore 创建机器绑定存储；当前使用内存实现，后续替换为 PostgreSQL。
func (c Config) AgentStore() AgentStore {
	return NewMemoryAgentStore()
}

// AIConfigStore 创建 AI 配置存储；当前使用内存实现，后续替换为 PostgreSQL。
func (c Config) AIConfigStore() AIConfigStore {
	return NewMemoryAIConfigStore()
}

// PlatformAccountStore 创建平台账号映射存储；配置 PostgreSQL 时使用 PostgreSQL，否则使用内存实现。
func (c Config) PlatformAccountStore(db *sql.DB) PlatformAccountStore {
	if db != nil {
		return NewPostgresPlatformAccountStore(db)
	}
	return NewMemoryPlatformAccountStore()
}

// TaskStore 创建任务存储；配置 PostgreSQL 时使用 PostgreSQL，否则使用内存实现。
func (c Config) TaskStore(db *sql.DB) TaskStore {
	if db != nil {
		return NewPostgresTaskStore(db)
	}
	return NewMemoryTaskStore()
}

// TaskLogStore 创建任务日志存储；当前使用内存实现，后续替换为 PostgreSQL。
func (c Config) TaskLogStore() TaskLogStore {
	return NewMemoryTaskLogStore()
}

// envInt 从环境变量读取整数，读取失败时返回默认值。
func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
