// Package main 是 GoodHR 新本地程序的唯一启动入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"goodhr5/local-agent-go-new/internal/bootstrap"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/system/console"
	"goodhr5/local-agent-go-new/internal/version"
)

// main 解析启动参数、组装应用并等待退出信号。
func main() {
	host := flag.String("host", config.DefaultHost, "本地监听地址")
	port := flag.Int("port", config.DefaultPort, "本地监听端口")
	dataDir := flag.String("data-dir", "", "本地数据目录")
	agentVersion := flag.String("version", version.Value, "本地程序版本号")
	flag.Parse()
	version.Value = strings.TrimSpace(*agentVersion)
	if version.Value == "" {
		log.Fatal("本地程序版本号不能为空")
	}

	cfg, err := config.Load(*host, *port, *dataDir)
	if err != nil {
		log.Fatalf("读取本地配置失败：%v", err)
	}
	logFile, err := setupFileLogger(cfg.LogsDir)
	if err != nil {
		log.Fatalf("初始化本地日志失败：%v", err)
	}
	defer logFile.Close()
	healthURL := fmt.Sprintf("http://%s/health", cfg.Address())
	existingCtx, cancelExisting := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	existing := console.ExistingAgent(existingCtx, healthURL, cfg.Port)
	cancelExisting()
	if existing {
		log.Printf("GoodHR 本地程序已经在运行，本次只复用现有实例")
		if cfg.AutoOpenConsole {
			openCtx, cancelOpen := context.WithTimeout(context.Background(), 3*time.Second)
			consoleURL := strings.TrimRight(cfg.CloudURL, "/") + "/admin/"
			if err = console.OpenWhenReady(openCtx, healthURL, consoleURL, cfg.Port); err != nil {
				log.Printf("打开现有 GoodHR 控制台失败：%v", err)
			}
			cancelOpen()
		}
		return
	}
	application, err := bootstrap.New(cfg)
	if err != nil {
		log.Fatalf("初始化本地程序失败：%v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := application.Run(ctx); err != nil {
		log.Fatalf("本地程序运行失败：%v", err)
	}
}

// setupFileLogger 把主程序日志同时写到终端和本地日志文件。
func setupFileLogger(logsDir string) (*os.File, error) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(logsDir, "local-agent.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.Printf("本地程序日志已启用：%s", path)
	return file, nil
}
