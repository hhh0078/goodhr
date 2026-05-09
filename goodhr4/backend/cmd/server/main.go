package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"goodhr4/backend/internal/config"
	"goodhr4/backend/internal/httpapi"
	"goodhr4/backend/internal/service"
	"goodhr4/backend/internal/store"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	dbpool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect pg failed: %v", err)
	}
	defer dbpool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	if err := dbpool.Ping(ctx); err != nil {
		log.Fatalf("ping pg failed: %v", err)
	}
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("ping redis failed: %v", err)
	}

	st := store.New(dbpool, rdb)
	svc := service.New(st, cfg.SessionTTL)
	handler := httpapi.New(svc, cfg.AllowedOrigin)

	mux := http.NewServeMux()
	handler.Register(mux)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("goodhr4 backend listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server stopped: %v", err)
	}
}
