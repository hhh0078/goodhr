// 本文件负责从环境变量加载云端后端配置，并创建开发期依赖。
package httpapi

import (
	"os"
	"strconv"
)

type Config struct {
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

// PlatformAccountStore 创建平台账号映射存储；当前使用内存实现，后续替换为 PostgreSQL。
func (c Config) PlatformAccountStore() PlatformAccountStore {
	return NewMemoryPlatformAccountStore()
}

// TaskStore 创建任务存储；当前使用内存实现，后续替换为 PostgreSQL。
func (c Config) TaskStore() TaskStore {
	return NewMemoryTaskStore()
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
