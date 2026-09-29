// Package bootstrap 负责组装新本地程序依赖、启动服务和按顺序清理资源。
package bootstrap

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
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
	"goodhr5/local-agent-go-new/internal/system/device"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
	systemfiles "goodhr5/local-agent-go-new/internal/system/files"
	"goodhr5/local-agent-go-new/internal/system/notification"
	"goodhr5/local-agent-go-new/internal/system/power"
	"goodhr5/local-agent-go-new/internal/updater"
	"goodhr5/local-agent-go-new/internal/version"
)

// Application 保存程序运行期间需要清理的顶层组件。
type Application struct {
	cfg       config.Config
	shutdown  chan struct{}
	stopOnce  sync.Once
	server    *api.Server
	runner    *lifecycle.Runner
	runtime   *runtimemanager.Manager
	browser   *client.Client
	store     *storage.Store
	downloads *downloadflow.Monitor
	ocr       *ocr.Client
	incidents *diagnostics.BrowserIncidentRecorder
}

// New 按依赖顺序创建 SQLite、客户端、流程和本地 HTTP 服务。
func New(cfg config.Config, runLog *diagnostics.Writer, previousRuns ...*diagnostics.PreviousRun) (*Application, error) {
	cleanupLocalFiles(cfg)
	previousExitReason := []string{}
	var previous *diagnostics.PreviousRun
	if len(previousRuns) > 0 && previousRuns[0] != nil {
		previous = previousRuns[0]
		previousExitReason = append(previousExitReason, previous.Reason)
	}
	store, err := storage.Open(cfg.DatabasePath, previousExitReason...)
	if err != nil {
		return nil, err
	}
	browserClient := client.New(cfg.WorkerURL())
	workerProcess := browserprocess.New(
		cfg.NodePath,
		cfg.WorkerEntryPath,
		cfg.WorkerPort,
		browserClient,
	)
	runtimeManager := runtimemanager.New(
		cfg.NodePath, cfg.WorkerEntryPath, cfg.RuntimeDir, cfg.OCRExecutable, workerProcess,
	)
	cloudClient := cloud.New(cfg.CloudURL)
	machineID, deviceErr := device.Current()
	if deviceErr != nil {
		log.Printf("稳定设备编号暂时不可用：%v", deviceErr)
	}
	agentBind := cloud.AgentBindRequest{
		MachineID: machineID, AgentVersion: version.Value, LocalPort: cfg.Port,
	}
	aiClient := ai.New()
	profiles := profile.New(cfg.ProfilesDir)
	powerGuard := &power.Guard{}
	notifier := &notification.Notifier{}
	logger := lifecycle.NewTaskLogger(store, shared.StandardLogger{})
	incidents := diagnostics.NewBrowserIncidentRecorder(store, cloudClient)
	runtimeLog := diagnostics.WorkerLine(runLog, func(line []byte) {
		logger.WorkerLine(line)
		incidents.WorkerLine(line)
	})
	ocrClient := ocr.New(cfg.OCRExecutable, runtimeLog)
	incidents.RecordLifecycle("agent.start", "本地程序已启动，版本 "+version.Value, false)
	if previous != nil {
		incidents.RecordLifecycleDetails("agent.previous_exit", "检测到上次异常退出："+previous.Reason, true, previous.Diagnostics())
	}
	workerProcess.SetLogSink(runtimeLog)
	browserClient.SetRecovery(workerProcess.Stop)
	checker := &preflight.Checker{
		Cloud: cloudClient, Runtime: runtimeManager, Browser: browserClient,
		Storage: store, Profiles: profiles, AI: aiClient, OCR: ocrClient,
		Power:       powerGuard,
		Directories: []string{cfg.DataDir, cfg.ProfilesDir, cfg.ExtensionsDir, cfg.DownloadsDir, cfg.ScreenshotsDir, cfg.LogsDir},
		Logger:      logger, AgentBind: agentBind,
	}
	greetingFlow := &greeting.Flow{
		Browser: browserClient, AI: aiClient, OCR: ocrClient, Store: store,
		Cloud: cloudClient, Notifier: notifier, Logger: logger, ScreenshotsDir: cfg.ScreenshotsDir,
		DownloadsDir: cfg.DownloadsDir, ExtensionPaths: cfg.ExtensionPaths,
	}
	replyFlow := &auto_reply.Flow{
		Browser: browserClient, AI: aiClient, Store: store, Cloud: cloudClient,
		Logger: logger, DownloadsDir: cfg.DownloadsDir, ExtensionPaths: cfg.ExtensionPaths,
	}
	runner := lifecycle.New(checker, greetingFlow, replyFlow, store, profiles, powerGuard, cloudClient, notifier, logger)
	downloads := &downloadflow.Monitor{Browser: browserClient, Store: store, Notifier: notifier}
	application := &Application{
		cfg: cfg, runner: runner, runtime: runtimeManager,
		browser: browserClient, store: store, downloads: downloads, ocr: ocrClient,
		shutdown: make(chan struct{}), incidents: incidents,
	}
	appUpdater := updater.New(cfg.DataDir, version.Value, application.RequestShutdown)
	server := api.NewServer(cfg, api.Dependencies{
		Runner: runner, Runtime: runtimeManager, Browser: browserClient,
		Downloads: downloads, Store: store, Profiles: profiles,
		OCR: ocrClient, Updater: appUpdater,
		Cloud: cloudClient, DeviceID: device.Current, Incidents: incidents,
	})
	application.server = server
	return application, nil
}

// RequestShutdown 请求主程序走完整收尾流程，确保 Worker 退出后更新安装器才覆盖文件。
func (a *Application) RequestShutdown() {
	if a == nil || a.shutdown == nil {
		return
	}
	a.stopOnce.Do(func() { close(a.shutdown) })
}

// ExitReason 返回主程序正常收尾的具体触发原因。
func (a *Application) ExitReason() string {
	if a == nil {
		return "正常退出"
	}
	select {
	case <-a.shutdown:
		return "安装更新前主动退出"
	default:
		return "正常退出"
	}
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
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go a.incidents.Run(runCtx)
	go func() {
		select {
		case <-a.shutdown:
			log.Printf("event=agent_shutdown_requested reason=update")
			cancelRun()
		case <-runCtx.Done():
		}
	}()
	downloadCtx, stopDownloads := context.WithCancel(runCtx)
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
			if err := console.OpenWhenReady(runCtx, healthURL, a.cfg.ConsolePageURL(), a.cfg.Port); err != nil && runCtx.Err() == nil {
				log.Printf("自动打开 GoodHR 控制台失败：%v", err)
			}
		}()
	}
	var runErr error
	select {
	case err := <-serverError:
		runErr = err
	case <-runCtx.Done():
	}
	shutdownReason := "收到系统退出信号"
	select {
	case <-a.shutdown:
		shutdownReason = "安装更新前主动退出"
	default:
		if runErr != nil {
			shutdownReason = "本地接口退出：" + runErr.Error()
		}
	}
	log.Printf("event=agent_shutdown exit_reason=%q", shutdownReason)
	a.runtime.PrepareWorkerStop(shutdownReason)
	shutdownStartedAt := time.Now()
	shutdownErrors := map[string]any{}
	taskCtx, cancelTasks := context.WithTimeout(context.Background(), 45*time.Second)
	a.runner.StopAll(taskCtx, shutdownReason)
	if taskCtx.Err() != nil {
		log.Printf("event=agent_shutdown_task_timeout exit_reason=%q", taskCtx.Err())
		shutdownErrors["tasks"] = taskCtx.Err().Error()
		if runErr == nil {
			runErr = fmt.Errorf("等待岗位任务停止超时：%w", taskCtx.Err())
		}
	}
	cancelTasks()
	serverCtx, cancelServer := context.WithTimeout(context.Background(), 5*time.Second)
	if err := a.server.Shutdown(serverCtx); err != nil {
		shutdownErrors["local_server"] = err.Error()
		if runErr == nil {
			runErr = fmt.Errorf("关闭本地接口失败：%w", err)
		}
	}
	cancelServer()
	browserCtx, cancelBrowser := context.WithTimeout(context.Background(), 60*time.Second)
	if _, err := a.browser.StopBrowser(browserCtx); err != nil {
		shutdownErrors["browser"] = err.Error()
		log.Printf("关闭浏览器时有点磨蹭，我继续保存下载记录：%v", err)
	}
	cancelBrowser()
	syncCtx, cancelSync := context.WithTimeout(context.Background(), 10*time.Second)
	if err := a.downloads.Sync(syncCtx); err != nil {
		shutdownErrors["downloads"] = err.Error()
		log.Printf("退出前最后同步下载记录没有完成：%v", err)
	}
	cancelSync()
	stopDownloads()
	<-downloadDone
	if err := a.runtime.StopWorker(); err != nil {
		shutdownErrors["worker"] = err.Error()
		if runErr == nil {
			runErr = fmt.Errorf("关闭操作进程失败：%w", err)
		}
	}
	a.ocr.Close()
	finalReason := shutdownReason
	if runErr != nil {
		finalReason += "：" + runErr.Error()
	}
	a.incidents.RecordLifecycleDetails("agent.exit", finalReason, runErr != nil, map[string]any{
		"agent_version": version.Value, "shutdown_duration_ms": time.Since(shutdownStartedAt).Milliseconds(), "shutdown_errors": shutdownErrors,
	})
	incidentCtx, cancelIncidents := context.WithTimeout(context.Background(), 5*time.Second)
	a.incidents.Flush(incidentCtx)
	cancelIncidents()
	if err := a.store.Close(); err != nil && runErr == nil {
		runErr = fmt.Errorf("关闭 SQLite 失败：%w", err)
	}
	return runErr
}
