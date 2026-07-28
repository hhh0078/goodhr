// Package main 是 GoodHR 新本地程序的唯一启动入口。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"goodhr5/local-agent-go-new/internal/bootstrap"
	"goodhr5/local-agent-go-new/internal/config"
)

// main 解析启动参数、组装应用并等待退出信号。
func main() {
	host := flag.String("host", config.DefaultHost, "本地监听地址")
	port := flag.Int("port", config.DefaultPort, "本地监听端口")
	dataDir := flag.String("data-dir", "", "本地数据目录")
	flag.Parse()

	cfg, err := config.Load(*host, *port, *dataDir)
	if err != nil {
		log.Fatalf("读取本地配置失败：%v", err)
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
