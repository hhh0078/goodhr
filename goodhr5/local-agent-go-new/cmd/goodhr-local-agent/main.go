// Package main 是 GoodHR 新本地程序的唯一启动入口。
package main

import (
	"context"
	"encoding/json"
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
	exitReason := "正常退出"
	var view *startup.View
	var report func(config.DataProgress)
	if cfg.AutoOpenConsole {
		view, err = startup.New(cfg.ConsolePageURL(), cfg.Port)
		if err != nil {
			return fmt.Errorf("创建启动页面失败：%w", err)
		}
		defer view.Close()
		defer func() {
			if runErr != nil {
				view.Fail(runErr)
				select {
				case <-view.Done:
				case <-ctx.Done():
				}
			}
		}()
		if err := console.OpenWhenReady(ctx, view.URL+"health", view.URL, cfg.Port); err != nil {
			return err
		}
		report = view.Update
		cfg.AutoOpenConsole = false // 启动页会在服务就绪后跳转，避免再打开第二个标签页。
	}
	if err := cfg.PrepareDataWithProgress(report); err != nil {
		return fmt.Errorf("准备本地数据失败：%w", err)
	}
	if view != nil {
		view.Update(config.DataProgress{Stage: "initializing", Message: "数据已准备好，正在启动小助手"})
	}
	crashPath := filepath.Join(cfg.LogsDir, "crash.log")
	previousBeforeReset := diagnostics.ReadPreviousRun(cfg.LogsDir, crashPath)
	logsReset, err := diagnostics.ResetLegacyLogs(cfg.LogsDir)
	if err != nil {
		return fmt.Errorf("清理旧格式日志失败：%w", err)
	}
	log.SetFlags(0)
	runLog, err := diagnostics.Open(cfg.LogsDir, "goodhr.jsonl")
	if err != nil {
		return fmt.Errorf("创建运行日志失败：%w", err)
	}
	log.SetOutput(runLog)
	var marker *diagnostics.RunMarker
	defer func() {
		panicked := false
		if recovered := recover(); recovered != nil {
			panicked = true
			runErr = fmt.Errorf("本地程序发生未捕获异常：%v", recovered)
			stack := debug.Stack()
			if len(stack) > 8000 {
				stack = stack[:8000]
			}
			logAgentLifecycle("agent_panic", "本地程序发生未捕获异常", runErr.Error(), true, map[string]any{"stack": strings.ToValidUTF8(string(stack), "")})
		}
		reason := exitReason
		if runErr != nil {
			reason = "运行失败：" + runErr.Error()
		} else if ctx.Err() != nil {
			reason = "收到退出信号"
		}
		logAgentLifecycle("agent_exit", "本地程序已退出", reason, runErr != nil, map[string]any{"pid": os.Getpid()})
		if !panicked && runErr == nil {
			if err := marker.Clear(); err != nil {
				log.Printf("event=agent_run_marker_clear_failed error=%q", err)
			}
		}
		log.SetOutput(os.Stderr)
		_ = runLog.Close()
	}()
	crash, err := os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("创建崩溃日志失败：%w", err)
	}
	if err = debug.SetCrashOutput(crash, debug.CrashOptions{}); err != nil {
		log.Printf("启用崩溃日志失败：%v", err)
	}
	crashInfo, _ := crash.Stat()
	_ = crash.Close()
	debug.SetTraceback("all")
	crashOffset := int64(0)
	if crashInfo != nil {
		crashOffset = crashInfo.Size()
	}
	marker, previous, err := diagnostics.MarkRun(cfg.LogsDir, crashPath, crashOffset)
	if err != nil {
		log.Printf("event=agent_run_marker_failed error=%q", err)
	}
	if logsReset {
		previous = previousBeforeReset
	}
	if previous != nil {
		logAgentLifecycle("agent_previous_run_unclean", "检测到上次异常退出", previous.Reason, true, map[string]any{"previous_pid": previous.PID, "previous_started_at": previous.StartedAt})
	}
	logAgentLifecycle("agent_start", "本地程序已启动", "", false, map[string]any{"pid": os.Getpid(), "version": version.Value})
	application, err := bootstrap.New(cfg, runLog, previous)
	if err != nil {
		return fmt.Errorf("初始化本地程序失败：%w", err)
	}
	if view != nil {
		go view.FollowReady(ctx, healthURL, cfg.Port)
	}
	if err := application.Run(ctx); err != nil {
		return fmt.Errorf("本地程序运行失败：%w", err)
	}
	exitReason = application.ExitReason()
	return nil
}

// logAgentLifecycle 把程序启动、退出和崩溃原因写成同一 JSONL 格式。
func logAgentLifecycle(event string, message string, reason string, isError bool, details map[string]any) {
	category := "运行"
	if isError {
		category = "异常退出"
	}
	output := map[string]any{"event": event}
	for key, value := range details {
		output[key] = value
	}
	if reason != "" {
		output["exit_reason"] = reason
	}
	errorMessage := ""
	if isError {
		errorMessage = reason
	}
	data, err := json.Marshal(map[string]any{
		"action":    strings.ReplaceAll(event, "_", "."),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"category":  category, "is_error": isError, "error_message": errorMessage,
		"message": message, "input": map[string]any{}, "output": output,
	})
	if err != nil {
		log.Printf("记录程序生命周期日志失败：%v", err)
		return
	}
	log.Print(string(data))
}
