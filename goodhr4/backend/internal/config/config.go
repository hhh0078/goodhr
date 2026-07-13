package config

import (
	"os"
	"time"
)

type Config struct {
	Addr          string
	DatabaseURL   string
	RedisAddr     string
	RedisPass     string
	RedisDB       int
	SessionTTL    time.Duration
	AllowedOrigin string
}

func Load() Config {
	return Config{
		Addr:          envOr("GOODHR4_ADDR", ":8787"),
		DatabaseURL:   envOr("GOODHR4_DATABASE_URL", "postgres://goodhr4:goodhr4@127.0.0.1:5432/goodhr4?sslmode=disable"),
		RedisAddr:     envOr("GOODHR4_REDIS_ADDR", "127.0.0.1:6379"),
		RedisPass:     os.Getenv("GOODHR4_REDIS_PASSWORD"),
		RedisDB:       0,
		SessionTTL:    30 * 24 * time.Hour,
		AllowedOrigin: envOr("GOODHR4_ALLOWED_ORIGIN", "*"),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
