// Package main 是 GoodHR 新本地程序的唯一启动入口。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"goodhr5/local-agent-go-new/internal/bootstrap"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/system/console"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
	"goodhr5/local-agent-go-new/internal/system/startup"
	"goodhr5/local-agent-go-new/internal/version"
)

// main 解析启动参数、组装应用并等待退出信号。
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

// run 先打开独立启动页并准备数据，再组装业务服务；失败时保留页面供用户查看。
func run() (runErr error) {
	host := flag.String("host", config.DefaultHost, "本地监听地址")
	port := flag.Int("port", config.DefaultPort, "本地监听端口")
	dataDir := flag.String("data-dir", "", "本地数据目录")
	agentVersion := flag.String("version", version.Value, "本地程序版本号")
	flag.Parse()
	version.Value = strings.TrimSpace(*agentVersion)
	if version.Value == "" {
		return fmt.Errorf("本地程序版本号不能为空")
	}

	cfg, err := config.Load(*host, *port, *dataDir)
	if err != nil {
		return fmt.Errorf("读取本地配置失败：%w", err)
	}
	healthURL := fmt.Sprintf("http://%s/health", cfg.Address())
	existingCtx, cancelExisting := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	existing := console.ExistingAgent(existingCtx, healthURL, cfg.Port)
	cancelExisting()
	if existing {
		log.Printf("GoodHR 本地程序已经在运行，本次只复用现有实例")
		if cfg.AutoOpenConsole {
			openCtx, cancelOpen := context.WithTimeout(context.Background(), 3*time.Second)
			if err = console.OpenWhenReady(openCtx, healthURL, cfg.ConsolePageURL(), cfg.Port); err != nil {
				log.Printf("打开现有 GoodHR 控制台失败：%v", err)
			}
			cancelOpen()
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var view *startup.View
	var report func(config.DataProgress)
	if cfg.AutoOpenConsole {
		view, err = startup.New(cfg.ConsolePageURL(), cfg.Port)
		if err != nil {
			return fmt.Errorf("创建启动页面失败：%w", err)
		}
		defer view.Close()
		if err := console.OpenWhenReady(ctx, view.URL+"health", view.URL, cfg.Port); err != nil {
			return err
		}
		report = view.Update
		cfg.AutoOpenConsole = false // 启动页会在服务就绪后跳转，避免再打开第二个标签页。
		defer func() {
			if runErr != nil {
				view.Fail(runErr)
				select {
				case <-view.Done:
				case <-ctx.Done():
				}
			}
		}()
	}
	if err := cfg.PrepareDataWithProgress(report); err != nil {
		return fmt.Errorf("准备本地数据失败：%w", err)
	}
	if view != nil {
		view.Update(config.DataProgress{Stage: "initializing", Message: "数据已准备好，正在启动小助手"})
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	runLog, err := diagnostics.Open(cfg.LogsDir, "agent.log")
	if err != nil {
		return fmt.Errorf("创建运行日志失败：%w", err)
	}
	defer func() {
		if runErr != nil {
			log.Printf("event=agent_start_or_run_failed error=%q", runErr)
		}
		log.SetOutput(os.Stderr)
		_ = runLog.Close()
	}()
	log.SetOutput(runLog)
	crash, err := os.OpenFile(filepath.Join(cfg.LogsDir, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("创建崩溃日志失败：%w", err)
	}
	if err = debug.SetCrashOutput(crash, debug.CrashOptions{}); err != nil {
		log.Printf("启用崩溃日志失败：%v", err)
	}
	_ = crash.Close()
	debug.SetTraceback("all")
	log.Printf("event=agent_start pid=%d version=%s logs=%s", os.Getpid(), version.Value, cfg.LogsDir)
	defer log.Printf("event=agent_exit pid=%d", os.Getpid())
	application, err := bootstrap.New(cfg)
	if err != nil {
		return fmt.Errorf("初始化本地程序失败：%w", err)
	}
	if view != nil {
		go view.FollowReady(ctx, healthURL, cfg.Port)
	}
	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("本地程序运行失败：%w", err)
	}
	return nil
}
