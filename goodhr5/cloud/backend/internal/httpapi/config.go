package httpapi

import (
	"os"
	"strconv"
)

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

func LoadConfigFromEnv() Config {
	return Config{
		RedisAddr:     os.Getenv("GOODHR_REDIS_ADDR"),
		RedisPassword: os.Getenv("GOODHR_REDIS_PASSWORD"),
		RedisDB:       envInt("GOODHR_REDIS_DB", 0),
	}
}

func (c Config) AuthStore() AuthStore {
	if c.RedisAddr != "" {
		return NewRedisAuthStore(c.RedisAddr, c.RedisPassword, c.RedisDB)
	}
	return NewMemoryAuthStore()
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
