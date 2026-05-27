// 本文件负责启动 GoodHR 5 云端 HTTP 服务。
package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"goodhr5/cloud/backend/internal/httpapi"
)

func main() {
	logPath, err := setupLogger()
	if err != nil {
		log.Fatalf("setup logger failed: %v", err)
	}
	addr := envOrDefault("GOODHR_CLOUD_ADDR", ":8084")
	server, err := httpapi.NewServer()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("GoodHR 5 cloud backend log file: %s", logPath)
	log.Printf("GoodHR 5 cloud backend listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Routes()); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func setupLogger() (string, error) {
	logPath := envOrDefault("GOODHR_CLOUD_LOG_FILE", "logs/backend.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return logPath, nil
}
