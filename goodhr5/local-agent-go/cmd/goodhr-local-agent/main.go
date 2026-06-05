// Package main 是 GoodHR 5 Go 版本本地程序入口。
package main

import (
	"flag"
	"log"

	"goodhr5/local-agent-go/internal/app"
	"goodhr5/local-agent-go/internal/config"
)

// main 解析启动参数并运行本地服务。
func main() {
	host := flag.String("host", config.DefaultHost, "本地监听地址")
	port := flag.Int("port", config.DefaultPort, "本地优先监听端口")
	flag.Parse()

	cfg, err := config.New(*host, *port)
	if err != nil {
		log.Fatalf("初始化本地配置失败：%v", err)
	}
	server := app.NewServer(cfg)
	if err := server.Run(); err != nil {
		log.Fatalf("本地程序启动失败：%v", err)
	}
}
