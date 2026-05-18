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

func (c Config) AuthStore() AuthStore {
	if c.RedisAddr != "" {
		return NewRedisAuthStore(c.RedisAddr, c.RedisPassword, c.RedisDB)
	}
	return NewMemoryAuthStore()
}

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
