// Package main 是 GoodHR 5 Go 版本本地程序入口。
package main

import (
	"flag"
	"log"
	"os"

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
	cfg.AutoOpenConsole = *openConsole
	server, err := app.NewServer(cfg)
	if err != nil {
		log.Fatalf("初始化本地服务失败：%v", err)
	}
	if err := server.Run(); err != nil {
		log.Fatalf("本地程序启动失败：%v", err)
	}
}
