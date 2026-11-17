// 本文件负责启动 GoodHR 5 云端 HTTP 服务。
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"goodhr5/cloud/backend/internal/httpapi"
)

// main 统一启动普通接口与自动回复模块，并在退出时取消任务、等待事务收尾。
func main() {
	logPath, err := setupLogger()
	if err != nil {
		log.Fatalf("setup logger failed: %v", err)
	}
	addr := envOrDefault("GOODHR_CLOUD_ADDR", ":8084")
	embedded, err := prepareAutoReplyEnvironment(logPath)
	if err != nil {
		log.Fatal(err)
	}
	server, err := httpapi.NewServer()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var closeReply func()
	if embedded {
		runtime, err := embeddedAutoReply(addr)
		if err != nil {
			log.Fatal(err)
		}
		server.SetAutoReplyRuntime(runtime)
		closeReply = runtime.Close
		log.Print("GoodHR 自动回复模块已随后台启动")
	}
	server.StartDailyMaintenance(ctx)
	httpServer := &http.Server{Addr: addr, Handler: server.Routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		if closeReply != nil {
			closeReply()
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()

	log.Printf("GoodHR 5 cloud backend log file: %s", logPath)
	log.Printf("GoodHR 5 cloud backend listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
