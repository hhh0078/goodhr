// Package updater 文件作用：下载本地程序更新包、记录进度并启动系统安装器。
package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Request 表示控制台提交的本地程序更新参数。
type Request struct {
	URL           string `json:"url"`
	TargetVersion string `json:"target_version"`
	ReleaseNote   string `json:"release_note"`
}

// Progress 表示可供控制台轮询的本地程序更新进度。
type Progress struct {
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

// Manager 管理当前唯一应用更新任务。
type Manager struct {
	mu             sync.Mutex
	dataDir        string
	currentVersion string
	progress       Progress
	http           *http.Client
}

// New 创建本地程序更新管理器。
func New(dataDir string, currentVersion string) *Manager {
	return &Manager{
		dataDir:        dataDir,
		currentVersion: strings.TrimSpace(currentVersion),
		http:           &http.Client{Timeout: 30 * time.Minute},
		progress: Progress{
			Stage:          "idle",
			Message:        "等待更新",
			CurrentVersion: strings.TrimSpace(currentVersion),
		},
	}
}

// Progress 返回当前更新进度快照。
func (m *Manager) Progress() Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.progress
}

// Start 校验版本和下载地址，并异步启动更新。
func (m *Manager) Start(request Request) (Progress, error) {
	request.URL = strings.TrimSpace(request.URL)
	request.TargetVersion = strings.TrimSpace(request.TargetVersion)
	request.ReleaseNote = strings.TrimSpace(request.ReleaseNote)
	if err := validateDownloadURL(request.URL); err != nil {
		return m.Progress(), err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.progress.Running {
		return m.progress, nil
	}
	if request.TargetVersion != "" && compareVersion(request.TargetVersion, m.currentVersion) <= 0 {
		m.progress = Progress{
			Stage: "idle", Message: "当前版本不低于最新版，先不折腾你更新了",
			URL: request.URL, TargetVersion: request.TargetVersion,
			CurrentVersion: m.currentVersion, ReleaseNote: request.ReleaseNote,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		return m.progress, nil
	}
	m.progress = Progress{
		Running: true, Stage: "queued", Message: "准备下载本地程序更新包", Percent: 1,
		URL: request.URL, TargetVersion: request.TargetVersion,
		CurrentVersion: m.currentVersion, ReleaseNote: request.ReleaseNote,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	go m.run(context.Background(), request)
	return m.progress, nil
}

// run 下载更新包并启动系统安装器。
func (m *Manager) run(ctx context.Context, request Request) {
	packagePath, err := m.download(ctx, request)
	if err != nil {
		m.fail(err)
		return
	}
	installerPath, err := prepareInstaller(packagePath)
	if err != nil {
		m.fail(err)
		return
	}
	m.update(func(progress *Progress) {
		progress.Running = true
		progress.Stage = "install"
		progress.Message = "下载完成，正在启动安装更新"
		progress.Percent = 100
		progress.PackagePath = installerPath
	})
	if err = startInstaller(installerPath); err != nil {
		m.fail(err)
		return
	}
	go func() {
		time.Sleep(1200 * time.Millisecond)
		os.Exit(0)
	}()
}

// download 把更新包保存到本地数据目录，并持续更新下载进度。
func (m *Manager) download(ctx context.Context, request Request) (string, error) {
	updatesDir := filepath.Join(m.dataDir, "app-updates")
	if err := os.MkdirAll(updatesDir, 0o755); err != nil {
		return "", fmt.Errorf("创建更新下载目录失败：%w", err)
	}
	packagePath := filepath.Join(updatesDir, packageName(request.URL, request.TargetVersion))
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return "", fmt.Errorf("创建更新下载请求失败：%w", err)
	}
	response, err := m.http.Do(httpRequest)
	if err != nil {
		return "", fmt.Errorf("下载本地程序更新包失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("下载本地程序更新包失败，状态码：%d", response.StatusCode)
	}
	tempPath := packagePath + ".tmp"
	file, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("创建更新包文件失败：%w", err)
	}
	reader := &progressReader{
		reader: response.Body, total: response.ContentLength,
		onProgress: func(received int64, total int64) {
			m.update(func(progress *Progress) {
				progress.Stage = "download"
				progress.Message = "正在下载本地程序更新包"
				progress.Received = received
				progress.Total = total
				progress.PackagePath = packagePath
				progress.Percent = downloadPercent(received, total)
			})
		},
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		if copyErr != nil {
			return "", fmt.Errorf("保存更新包失败：%w", copyErr)
		}
		return "", fmt.Errorf("关闭更新包失败：%w", closeErr)
	}
	_ = os.Remove(packagePath)
	if err = os.Rename(tempPath, packagePath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("完成更新包下载失败：%w", err)
	}
	return packagePath, nil
}

// fail 保存更新失败进度，供控制台展示和再次重试。
func (m *Manager) fail(err error) {
	m.update(func(progress *Progress) {
		progress.Running = false
		progress.Stage = "failed"
		progress.Message = err.Error()
	})
}

// update 在锁内修改更新进度并刷新更新时间。
func (m *Manager) update(change func(*Progress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	change(&m.progress)
	m.progress.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

// progressReader 在下载过程中累计字节数并回调进度。
type progressReader struct {
	reader     io.Reader
	received   int64
	total      int64
	onProgress func(int64, int64)
}

// Read 读取网络响应并报告累计进度。
func (r *progressReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	if count > 0 {
		r.received += int64(count)
		if r.onProgress != nil {
			r.onProgress(r.received, r.total)
		}
	}
	return count, err
}

// validateDownloadURL 只允许从 HTTP 或 HTTPS 地址下载更新包。
func validateDownloadURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("本地程序更新包下载地址不正确")
	}
	return nil
}

// packageName 生成不含路径字符的更新包文件名。
func packageName(downloadURL string, targetVersion string) string {
	parsed, _ := url.Parse(downloadURL)
	extension := strings.ToLower(filepath.Ext(parsed.Path))
	if extension == "" {
		if goruntime.GOOS == "windows" {
			extension = ".exe"
		} else {
			extension = ".pkg"
		}
	}
	versionText := strings.TrimSpace(targetVersion)
	if versionText == "" {
		versionText = strconv.FormatInt(time.Now().Unix(), 10)
	}
	versionText = strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-").Replace(versionText)
	return "goodhr-local-agent-update-" + versionText + extension
}

// downloadPercent 把下载字节数转换为 5 到 95 的进度。
func downloadPercent(received int64, total int64) int {
	if total <= 0 {
		return 10
	}
	return min(95, 5+int(received*90/total))
}

// compareVersion 比较点分版本，左侧更高返回 1，右侧更高返回 -1。
func compareVersion(left string, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	for index := 0; index < max(len(leftParts), len(rightParts)); index++ {
		leftValue := partAt(leftParts, index)
		rightValue := partAt(rightParts, index)
		if leftValue > rightValue {
			return 1
		}
		if leftValue < rightValue {
			return -1
		}
	}
	return 0
}

// versionParts 把版本号解析为可比较的数字片段。
func versionParts(value string) []int {
	rawParts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
	result := make([]int, 0, len(rawParts))
	for _, raw := range rawParts {
		end := 0
		for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
			end++
		}
		number, _ := strconv.Atoi(raw[:end])
		result = append(result, number)
	}
	return result
}

// partAt 安全返回版本片段，不存在时返回零。
func partAt(parts []int, index int) int {
	if index < 0 || index >= len(parts) {
		return 0
	}
	return parts[index]
}

// startInstaller 使用当前系统默认安装方式打开更新包。
func startInstaller(packagePath string) error {
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", packagePath).Start()
	case "windows":
		script := fmt.Sprintf(
			"Start-Sleep -Seconds 2; Start-Process -FilePath '%s' -ArgumentList '/SILENT','/NORESTART'",
			strings.ReplaceAll(packagePath, "'", "''"),
		)
		return exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Start()
	default:
		return fmt.Errorf("当前系统暂不支持自动启动本地程序安装器")
	}
}
