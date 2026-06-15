// Package main 是 GoodHR 5 Go 版本本地程序入口。
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	goruntime "runtime"

	"goodhr5/local-agent-go/internal/app"
	"goodhr5/local-agent-go/internal/config"
)

// main 解析启动参数并运行本地服务。
func main() {
	host := flag.String("host", config.DefaultHost, "本地监听地址")
	port := flag.Int("port", config.DefaultPort, "本地优先监听端口")
	dataDir := flag.String("data-dir", "", "本地数据目录")
	openConsole := flag.Bool("open-console", os.Getenv("GOODHR_AUTO_OPEN_CONSOLE") != "false", "启动后自动打开控制台")
	flag.Parse()

	cfg, err := config.NewWithDataDir(*host, *port, *dataDir)
	if err != nil {
		log.Fatalf("初始化本地配置失败：%v", err)
	}
	logFile, err := setupFileLogger(cfg)
	if err != nil {
		log.Fatalf("初始化日志文件失败：%v", err)
	}
	if logFile != nil {
		defer logFile.Close()
	}
	cfg.AutoOpenConsole = *openConsole
	server, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("初始化本地服务失败：%v", err)
	}
	if err := server.Run(); err != nil {
		log.Fatalf("本地程序启动失败：%v", err)
	}
}

// setupFileLogger 初始化本地程序文件日志。
// cfg 为本地配置，返回打开的日志文件句柄。
func setupFileLogger(cfg *config.Config) (*os.File, error) {
	if cfg == nil || cfg.LogsDir == "" {
		return nil, nil
	}
	if err := os.MkdirAll(cfg.LogsDir, 0o755); err != nil {
		return nil, err
	}
	logPath := filepath.Join(cfg.LogsDir, "local-agent.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if goruntime.GOOS == "windows" {
		log.SetOutput(file)
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, file))
	}
	log.Printf("本地程序日志已启用：%s", logPath)
	return file, nil
}
