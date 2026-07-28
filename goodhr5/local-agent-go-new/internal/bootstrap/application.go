// Package bootstrap 负责组装新本地程序依赖、启动服务和按顺序清理资源。
package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"goodhr5/local-agent-go-new/internal/api"
	"goodhr5/local-agent-go-new/internal/browser/client"
	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
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
	"goodhr5/local-agent-go-new/internal/system/power"
)

// Application 保存程序运行期间需要清理的顶层组件。
type Application struct {
	server  *api.Server
	runner  *lifecycle.Runner
	runtime *runtimemanager.Manager
	store   *storage.Store
}

// New 按依赖顺序创建 SQLite、客户端、流程和本地 HTTP 服务。
func New(cfg config.Config) (*Application, error) {
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
	runtimeManager := runtimemanager.New(cfg.NodePath, cfg.WorkerEntryPath, workerProcess)
	cloudClient := cloud.New(cfg.CloudURL)
	aiClient := ai.New()
	ocrClient := ocr.New(cfg.OCRExecutable)
	profiles := profile.New(cfg.ProfilesDir)
	powerGuard := &power.Guard{}
	logger := lifecycle.NewTaskLogger(store, shared.StandardLogger{})
	checker := &preflight.Checker{
		Cloud: cloudClient, Runtime: runtimeManager, Browser: browserClient,
		Storage: store, Profiles: profiles, AI: aiClient, OCR: ocrClient,
		Power:       powerGuard,
		Directories: []string{cfg.DataDir, cfg.ProfilesDir, cfg.DownloadsDir, cfg.ScreenshotsDir, cfg.LogsDir},
		Logger:      logger,
	}
	greetingFlow := &greeting.Flow{
		Browser: browserClient, AI: aiClient, OCR: ocrClient, Store: store,
		Cloud: cloudClient, Logger: logger, ScreenshotsDir: cfg.ScreenshotsDir,
		DownloadsDir: cfg.DownloadsDir,
	}
	replyFlow := &auto_reply.Flow{
		Browser: browserClient, AI: aiClient, Store: store, Cloud: cloudClient,
		Logger: logger, DownloadsDir: cfg.DownloadsDir,
	}
	runner := lifecycle.New(checker, greetingFlow, replyFlow, store, profiles, powerGuard, logger)
	server := api.NewServer(cfg.Address(), runner, runtimeManager, browserClient)
	return &Application{server: server, runner: runner, runtime: runtimeManager, store: store}, nil
}

// Run 启动本地 HTTP 服务并在上下文取消后按顺序收尾。
func (a *Application) Run(ctx context.Context) error {
	serverError := make(chan error, 1)
	go func() {
		serverError <- a.server.ListenAndServe()
	}()
	var runErr error
	select {
	case err := <-serverError:
		runErr = err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.runner.StopAll(shutdownCtx)
	_ = a.server.Shutdown(shutdownCtx)
	_ = a.runtime.StopWorker()
	if err := a.store.Close(); err != nil && runErr == nil {
		runErr = fmt.Errorf("关闭 SQLite 失败：%w", err)
	}
	return runErr
}
