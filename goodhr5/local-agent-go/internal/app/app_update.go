// Package app 提供本地程序安装包下载和更新启动接口。
package app

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"goodhr5/local-agent-go/internal/response"
	"goodhr5/local-agent-go/internal/version"
)

var appUpdateState = &appUpdateProgress{
	Stage:   "idle",
	Message: "等待更新",
}

// appUpdateProgress 表示本地程序更新进度。
type appUpdateProgress struct {
	mu             sync.Mutex
	Running        bool   `json:"running"`
	Stage          string `json:"stage"`
	Message        string `json:"message"`
	Percent        int    `json:"percent"`
	Received       int64  `json:"received"`
	Total          int64  `json:"total"`
	URL            string `json:"url"`
	TargetVersion  string `json:"target_version"`
	CurrentVersion string `json:"current_version"`
	ReleaseNote    string `json:"release_note"`
	PackagePath    string `json:"package_path"`
	UpdatedAt      string `json:"updated_at"`
}

// snapshot 返回当前更新进度快照。
// 返回值用于前端轮询展示。
func (p *appUpdateProgress) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"running":         p.Running,
		"stage":           p.Stage,
		"message":         p.Message,
		"percent":         p.Percent,
		"received":        p.Received,
		"total":           p.Total,
		"url":             p.URL,
		"target_version":  p.TargetVersion,
		"current_version": version.Value,
		"release_note":    p.ReleaseNote,
		"package_path":    p.PackagePath,
		"updated_at":      p.UpdatedAt,
	}
}

// set 更新本地程序更新进度。
// progress 为需要覆盖的进度字段。
func (p *appUpdateProgress) set(progress appUpdateProgress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if progress.Stage != "" {
		p.Stage = progress.Stage
	}
	if progress.Message != "" {
		p.Message = progress.Message
	}
	if progress.Percent >= 0 {
		p.Percent = progress.Percent
	}
	if progress.Received >= 0 {
		p.Received = progress.Received
	}
	if progress.Total >= 0 {
		p.Total = progress.Total
	}
	if progress.URL != "" {
		p.URL = progress.URL
	}
	if progress.TargetVersion != "" {
		p.TargetVersion = progress.TargetVersion
	}
	if progress.ReleaseNote != "" {
		p.ReleaseNote = progress.ReleaseNote
	}
	if progress.PackagePath != "" {
		p.PackagePath = progress.PackagePath
	}
	p.Running = progress.Running
	p.CurrentVersion = version.Value
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

// handleAppUpdateStatus 返回本地程序更新状态。
// w 为响应对象，r 为请求对象。
func (s *Server) handleAppUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	response.Success(w, appUpdateState.snapshot())
}

// handleAppUpdateStart 开始下载并启动本地程序更新。
// w 为响应对象，r 为请求对象。
func (s *Server) handleAppUpdateStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "请求方法不支持")
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	downloadURL := strings.TrimSpace(stringValue(payload["url"]))
	if downloadURL == "" {
		response.Error(w, http.StatusBadRequest, "本地程序更新包下载地址为空")
		return
	}
	targetVersion := strings.TrimSpace(stringValue(payload["target_version"]))
	releaseNote := strings.TrimSpace(stringValue(payload["release_note"]))
	if appUpdateRunning() {
		response.Success(w, appUpdateState.snapshot())
		return
	}
	appUpdateState.set(appUpdateProgress{
		Running:       true,
		Stage:         "queued",
		Message:       "准备下载本地程序更新包",
		Percent:       1,
		URL:           downloadURL,
		TargetVersion: targetVersion,
		ReleaseNote:   releaseNote,
	})
	go s.runAppUpdate(context.Background(), downloadURL, targetVersion)
	response.Success(w, appUpdateState.snapshot())
}

// appUpdateRunning 判断当前是否正在更新。
// 返回 true 表示已有更新任务在执行。
func appUpdateRunning() bool {
	status := appUpdateState.snapshot()
	running, _ := status["running"].(bool)
	return running
}

// runAppUpdate 下载更新包并启动安装器。
// ctx 为上下文，downloadURL 为安装包地址，targetVersion 为目标版本。
func (s *Server) runAppUpdate(ctx context.Context, downloadURL string, targetVersion string) {
	packagePath, err := s.downloadAppUpdatePackage(ctx, downloadURL, targetVersion)
	if err != nil {
		appUpdateState.set(appUpdateProgress{Running: false, Stage: "failed", Message: err.Error(), Percent: 0})
		return
	}
	installerPath, err := prepareAppInstaller(packagePath)
	if err != nil {
		appUpdateState.set(appUpdateProgress{Running: false, Stage: "failed", Message: err.Error(), Percent: 100, PackagePath: packagePath})
		return
	}
	appUpdateState.set(appUpdateProgress{
		Running:     true,
		Stage:       "install",
		Message:     "下载完成，正在启动安装更新",
		Percent:     100,
		PackagePath: installerPath,
	})
	if err := startAppInstaller(installerPath); err != nil {
		appUpdateState.set(appUpdateProgress{Running: false, Stage: "failed", Message: err.Error(), Percent: 100, PackagePath: installerPath})
		return
	}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		os.Exit(0)
	}()
}

// downloadAppUpdatePackage 下载本地程序安装包。
// ctx 为上下文，downloadURL 为下载地址，targetVersion 为目标版本。
func (s *Server) downloadAppUpdatePackage(ctx context.Context, downloadURL string, targetVersion string) (string, error) {
	updatesDir := filepath.Join(s.cfg.DataDir, "app-updates")
	if err := os.MkdirAll(updatesDir, 0o755); err != nil {
		return "", fmt.Errorf("创建更新下载目录失败：%w", err)
	}
	packagePath := filepath.Join(updatesDir, appUpdatePackageName(downloadURL, targetVersion))
	appUpdateState.set(appUpdateProgress{Running: true, Stage: "download", Message: "正在下载本地程序更新包", Percent: 5, PackagePath: packagePath})
	if err := downloadAppUpdateFile(ctx, downloadURL, packagePath, func(received int64, total int64) {
		percent := 10
		if total > 0 {
			percent = 10 + int(received*80/total)
		}
		if percent > 95 {
			percent = 95
		}
		appUpdateState.set(appUpdateProgress{
			Running:  true,
			Stage:    "download",
			Message:  "正在下载本地程序更新包",
			Percent:  percent,
			Received: received,
			Total:    total,
		})
	}); err != nil {
		return "", fmt.Errorf("下载本地程序更新包失败：%w", err)
	}
	return packagePath, nil
}

// downloadAppUpdateFile 下载文件并回调进度。
// ctx 为上下文，url 为下载地址，targetPath 为保存路径，onProgress 为下载进度回调。
func downloadAppUpdateFile(ctx context.Context, url string, targetPath string, onProgress func(received int64, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(url), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("下载失败，状态码：%d", resp.StatusCode)
	}
	tmpPath := targetPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	reader := &appUpdateProgressReader{reader: resp.Body, total: resp.ContentLength, onProgress: onProgress}
	if _, err := io.Copy(out, reader); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(targetPath)
	return os.Rename(tmpPath, targetPath)
}

// appUpdateProgressReader 在读取下载内容时更新进度。
type appUpdateProgressReader struct {
	reader     io.Reader
	received   int64
	total      int64
	onProgress func(received int64, total int64)
}

// Read 读取下载内容并触发进度回调。
// p 为目标缓冲区。
func (r *appUpdateProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.received += int64(n)
		if r.onProgress != nil {
			r.onProgress(r.received, r.total)
		}
	}
	return n, err
}

// appUpdatePackageName 生成本地程序更新包文件名。
// downloadURL 为下载地址，targetVersion 为目标版本。
func appUpdatePackageName(downloadURL string, targetVersion string) string {
	ext := strings.ToLower(filepath.Ext(strings.Split(downloadURL, "?")[0]))
	if ext == "" {
		if runtime.GOOS == "windows" {
			ext = ".exe"
		} else {
			ext = ".pkg"
		}
	}
	versionText := strings.TrimSpace(targetVersion)
	if versionText == "" {
		versionText = fmt.Sprintf("%d", time.Now().Unix())
	}
	versionText = strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(versionText)
	return "goodhr-local-agent-update-" + versionText + ext
}

// prepareAppInstaller 准备可启动的安装器路径。
// packagePath 为下载后的文件，zip 包会先解压并查找 exe 安装器。
func prepareAppInstaller(packagePath string) (string, error) {
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Ext(packagePath), ".zip") {
		return packagePath, nil
	}
	targetDir := strings.TrimSuffix(packagePath, filepath.Ext(packagePath))
	if err := os.RemoveAll(targetDir); err != nil {
		return "", fmt.Errorf("清理更新解压目录失败：%w", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建更新解压目录失败：%w", err)
	}
	installerPath, err := extractAppUpdateZip(packagePath, targetDir)
	if err != nil {
		return "", err
	}
	return installerPath, nil
}

// extractAppUpdateZip 解压自动更新 zip，并返回其中的 exe 安装器路径。
// archivePath 为 zip 包路径，targetDir 为安全解压目录。
func extractAppUpdateZip(archivePath string, targetDir string) (string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", fmt.Errorf("打开更新压缩包失败：%w", err)
	}
	defer reader.Close()
	installerPath := ""
	for _, file := range reader.File {
		targetPath, err := safeAppUpdatePath(targetDir, file.Name)
		if err != nil {
			return "", err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		dst, err := os.Create(targetPath)
		if err != nil {
			_ = src.Close()
			return "", err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		_ = src.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if installerPath == "" && strings.EqualFold(filepath.Ext(targetPath), ".exe") {
			installerPath = targetPath
		}
	}
	if installerPath == "" {
		return "", fmt.Errorf("更新压缩包内未找到 exe 安装器")
	}
	return installerPath, nil
}

// safeAppUpdatePath 返回 zip 条目的安全落盘路径。
// targetDir 为解压根目录，name 为 zip 内部文件名。
func safeAppUpdatePath(targetDir string, name string) (string, error) {
	cleanName := filepath.Clean(name)
	if filepath.IsAbs(cleanName) || cleanName == "." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) || cleanName == ".." {
		return "", fmt.Errorf("更新压缩包包含非法路径：%s", name)
	}
	targetPath := filepath.Join(targetDir, cleanName)
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return "", err
	}
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", err
	}
	if absTargetPath != absTargetDir && !strings.HasPrefix(absTargetPath, absTargetDir+string(os.PathSeparator)) {
		return "", fmt.Errorf("更新压缩包包含越界路径：%s", name)
	}
	return targetPath, nil
}

// startAppInstaller 启动本地程序安装器。
// packagePath 为已经下载好的安装包路径。
func startAppInstaller(packagePath string) error {
	switch runtime.GOOS {
	case "windows":
		script := fmt.Sprintf(
			"Start-Sleep -Seconds 2; Start-Process -FilePath '%s' -ArgumentList '/SILENT','/NORESTART'",
			strings.ReplaceAll(packagePath, "'", "''"),
		)
		cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
		hideCommandWindow(cmd)
		return cmd.Start()
	case "darwin":
		if strings.EqualFold(filepath.Ext(packagePath), ".pkg") {
			return exec.Command("open", packagePath).Start()
		}
		return exec.Command("open", packagePath).Start()
	default:
		return fmt.Errorf("当前系统暂不支持自动启动本地程序安装器")
	}
}
