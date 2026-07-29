// Package bootstrap 负责组装新本地程序依赖、启动服务和按顺序清理资源。
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/api"
	"goodhr5/local-agent-go-new/internal/browser/client"
	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	downloadflow "goodhr5/local-agent-go-new/internal/flow/download"
	"goodhr5/local-agent-go-new/internal/flow/greeting"
	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/flow/preflight"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"goodhr5/local-agent-go-new/internal/profile"
	runtimemanager "goodhr5/local-agent-go-new/internal/runtime"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/console"
	systemfiles "goodhr5/local-agent-go-new/internal/system/files"
	"goodhr5/local-agent-go-new/internal/system/notification"
	"goodhr5/local-agent-go-new/internal/system/power"
	"goodhr5/local-agent-go-new/internal/updater"
	"goodhr5/local-agent-go-new/internal/version"
)

// Application 保存程序运行期间需要清理的顶层组件。
type Application struct {
	cfg       config.Config
	server    *api.Server
	runner    *lifecycle.Runner
	runtime   *runtimemanager.Manager
	browser   *client.Client
	store     *storage.Store
	downloads *downloadflow.Monitor
	ocr       *ocr.Client
}

// New 按依赖顺序创建 SQLite、客户端、流程和本地 HTTP 服务。
func New(cfg config.Config) (*Application, error) {
	cleanupLocalFiles(cfg)
	store, err := storage.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	browserClient := client.New(cfg.WorkerURL())
	workerProcess := browserprocess.New(
		cfg.NodePath,
		cfg.WorkerEntryPath,
		cfg.WorkerPort,
		filepath.Join(cfg.LogsDir, "browser-worker.log"),
		browserClient,
	)
	runtimeManager := runtimemanager.New(
		cfg.NodePath, cfg.WorkerEntryPath, cfg.RuntimeDir, cfg.OCRExecutable, workerProcess,
	)
	cloudClient := cloud.New(cfg.CloudURL)
	aiClient := ai.New()
	ocrClient := ocr.New(cfg.OCRExecutable)
	profiles := profile.New(cfg.ProfilesDir)
	powerGuard := &power.Guard{}
	notifier := &notification.Notifier{}
	logger := lifecycle.NewTaskLogger(store, shared.StandardLogger{})
	workerProcess.SetLogSink(logger.WorkerLine)
	checker := &preflight.Checker{
		Cloud: cloudClient, Runtime: runtimeManager, Browser: browserClient,
		Storage: store, Profiles: profiles, AI: aiClient, OCR: ocrClient,
		Power:       powerGuard,
		Directories: []string{cfg.DataDir, cfg.ProfilesDir, cfg.DownloadsDir, cfg.ScreenshotsDir, cfg.LogsDir},
		Logger:      logger,
	}
	greetingFlow := &greeting.Flow{
		Browser: browserClient, AI: aiClient, OCR: ocrClient, Store: store,
		Cloud: cloudClient, Notifier: notifier, Logger: logger, ScreenshotsDir: cfg.ScreenshotsDir,
		DownloadsDir: cfg.DownloadsDir,
	}
	replyFlow := &auto_reply.Flow{
		Browser: browserClient, AI: aiClient, Store: store, Cloud: cloudClient,
		Logger: logger, DownloadsDir: cfg.DownloadsDir,
	}
	runner := lifecycle.New(checker, greetingFlow, replyFlow, store, profiles, powerGuard, cloudClient, notifier, logger)
	downloads := &downloadflow.Monitor{Browser: browserClient, Store: store, Notifier: notifier}
	appUpdater := updater.New(cfg.DataDir, version.Value)
	server := api.NewServer(cfg, api.Dependencies{
		Runner: runner, Runtime: runtimeManager, Browser: browserClient,
		Downloads: downloads, Store: store, Profiles: profiles,
		OCR: ocrClient, Updater: appUpdater,
	})
	return &Application{
		cfg: cfg, server: server, runner: runner, runtime: runtimeManager,
		browser: browserClient, store: store, downloads: downloads, ocr: ocrClient,
	}, nil
}

// cleanupLocalFiles 清理崩溃遗留截图、组件压缩包和旧更新包，失败只记日志不阻断启动。
func cleanupLocalFiles(cfg config.Config) {
	targets := []struct {
		path      string
		retention time.Duration
	}{
		{path: cfg.ScreenshotsDir, retention: 24 * time.Hour},
		{path: filepath.Join(cfg.RuntimeDir, "downloads"), retention: 24 * time.Hour},
		{path: filepath.Join(cfg.DataDir, "app-updates"), retention: 7 * 24 * time.Hour},
	}
	for _, target := range targets {
		if _, err := systemfiles.RemoveOlderThan(target.path, time.Now().Add(-target.retention)); err != nil {
			log.Printf("过期临时文件没有清理完 path=%s err=%v", target.path, err)
		}
	}
}

// Run 启动本地 HTTP 服务并在上下文取消后按顺序收尾。
func (a *Application) Run(ctx context.Context) error {
	downloadCtx, stopDownloads := context.WithCancel(ctx)
	downloadDone := make(chan struct{})
	go func() {
		defer close(downloadDone)
		a.downloads.Run(downloadCtx)
	}()
	serverError := make(chan error, 1)
	go func() {
		serverError <- a.server.ListenAndServe()
	}()
	if a.cfg.AutoOpenConsole {
		go func() {
			healthURL := fmt.Sprintf("http://%s/health", a.cfg.Address())
			consoleURL := strings.TrimRight(a.cfg.CloudURL, "/") + "/admin/"
			if err := console.OpenWhenReady(ctx, healthURL, consoleURL, a.cfg.Port); err != nil && ctx.Err() == nil {
				log.Printf("自动打开 GoodHR 控制台失败：%v", err)
			}
		}()
	}
	var runErr error
	select {
	case err := <-serverError:
		runErr = err
	case <-ctx.Done():
	}
	taskCtx, cancelTasks := context.WithTimeout(context.Background(), 45*time.Second)
	a.runner.StopAll(taskCtx)
	cancelTasks()
	serverCtx, cancelServer := context.WithTimeout(context.Background(), 5*time.Second)
	if err := a.server.Shutdown(serverCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("关闭本地接口失败：%w", err)
	}
	cancelServer()
	browserCtx, cancelBrowser := context.WithTimeout(context.Background(), 60*time.Second)
	if _, err := a.browser.StopBrowser(browserCtx); err != nil {
		log.Printf("关闭浏览器时有点磨蹭，我继续保存下载记录：%v", err)
	}
	cancelBrowser()
	syncCtx, cancelSync := context.WithTimeout(context.Background(), 10*time.Second)
	if err := a.downloads.Sync(syncCtx); err != nil {
		log.Printf("退出前最后同步下载记录没有完成：%v", err)
	}
	cancelSync()
	stopDownloads()
	<-downloadDone
	_ = a.runtime.StopWorker()
	a.ocr.Close()
	if err := a.store.Close(); err != nil && runErr == nil {
		runErr = fmt.Errorf("关闭 SQLite 失败：%w", err)
	}
	return runErr
}
