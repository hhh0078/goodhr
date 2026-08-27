// Package runtime 文件作用：通过国内 npm 镜像安装包装器，并用用户 Key 从官方安装和验证最新版 Chromium。
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const npmMirrorRegistry = "https://registry.npmmirror.com"

const officialInstallMaxAttempts = 3

var officialDownloadProgress = regexp.MustCompile(`Download progress:\s+(\d+)%\s+\((\d+)/(\d+)\s+MB\)`)

// cloakBrowserDiagnostics 表示官方 info 命令返回的必要安装诊断字段。
type cloakBrowserDiagnostics struct {
	Binary struct {
		Version   string `json:"version"`
		Path      string `json:"path"`
		Installed bool   `json:"installed"`
	} `json:"binary"`
	License struct {
		Tier  string `json:"tier"`
		Valid *bool  `json:"valid"`
		Error string `json:"error"`
	} `json:"license"`
}

// nodePackage 表示 package.json 中当前安装流程需要读取的依赖版本。
type nodePackage struct {
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

// installWorkerDependencies 使用锁文件和国内镜像安装 Worker 生产依赖。
func (m *Manager) installWorkerDependencies(ctx context.Context) error {
	if m.workerDependenciesReady() {
		m.setInstallProgress(InstallProgress{
			Running: true, Component: "cloakbrowser_wrapper", Stage: "skipped",
			Message: "CloakBrowser 控制组件已经是当前版本", Percent: 34,
		})
		return nil
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser_wrapper", Stage: "install_dependency",
		Message: "正在通过国内镜像安装 CloakBrowser 控制组件", Percent: 25,
	})
	command, err := m.npmInstallCommand(ctx)
	if err != nil {
		return err
	}
	command.Dir = m.workerRoot()
	command.Env = overrideEnvironment(os.Environ(), []string{
		"npm_config_registry=" + npmMirrorRegistry,
		"npm_config_audit=false",
		"npm_config_fund=false",
		"NO_UPDATE_NOTIFIER=1",
	})
	output, runErr := runStreamingCommand(command, m.cloakBrowserLicenseKey(), func(line string) {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "added ") || strings.Contains(lower, "up to date") {
			m.setInstallProgress(InstallProgress{
				Running: true, Component: "cloakbrowser_wrapper", Stage: "install_dependency",
				Message: "CloakBrowser 控制组件依赖已经下载，正在整理", Percent: 32,
			})
		}
	})
	if runErr != nil {
		return fmt.Errorf("安装 CloakBrowser 控制组件失败：%s", commandFailure(output, runErr))
	}
	if !m.workerDependenciesReady() {
		return fmt.Errorf("CloakBrowser 控制组件安装结束，但必要依赖仍不完整")
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser_wrapper", Stage: "installed",
		Message: "CloakBrowser 控制组件安装完成", Percent: 34,
	})
	return nil
}

// validateOfficialLicense 通过官方接口验证用户 Key，拒绝静默退回旧版免费内核。
func (m *Manager) validateOfficialLicense(ctx context.Context, licenseKey string) error {
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "validate_license",
		Message: "正在通过 CloakBrowser 官方校验 Key", Percent: 36,
	})
	info, err := m.cloakBrowserInfo(ctx, licenseKey)
	if err != nil {
		return fmt.Errorf("CloakBrowser Key 校验失败：%w", err)
	}
	if info.License.Valid == nil {
		return fmt.Errorf("CloakBrowser 官方暂时没有确认这个 Key，请检查网络后重试")
	}
	if !*info.License.Valid {
		return fmt.Errorf("CloakBrowser Key 无效或已过期，请重新获取后再试")
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "validate_license",
		Message: "CloakBrowser Key 校验通过", Percent: 40,
	})
	return nil
}

// installOfficialCloakBrowser 强制刷新 Stable 最新版本并解析官方十等分下载进度。
func (m *Manager) installOfficialCloakBrowser(ctx context.Context, licenseKey string) (cloakBrowserDiagnostics, error) {
	if err := m.clearOfficialVersionCheckCache(); err != nil {
		return cloakBrowserDiagnostics{}, err
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "resolve_version",
		Message: "正在向 CloakBrowser 官方确认 Stable 最新版", Percent: 41,
	})
	output, runErr := m.runOfficialInstallWithRetry(ctx, licenseKey)
	if runErr != nil {
		return cloakBrowserDiagnostics{}, fmt.Errorf("下载最新版 CloakBrowser 失败：%s", commandFailure(output, runErr))
	}
	latestVersion, err := m.freshOfficialVersion()
	if err != nil {
		return cloakBrowserDiagnostics{}, err
	}
	info, err := m.cloakBrowserInfo(ctx, licenseKey)
	if err != nil {
		return cloakBrowserDiagnostics{}, fmt.Errorf("读取官方浏览器安装结果失败：%w", err)
	}
	if !info.Binary.Installed || !fileExists(info.Binary.Path) {
		return cloakBrowserDiagnostics{}, fmt.Errorf("官方安装命令已结束，但没有找到可运行的 Chromium")
	}
	if strings.TrimSpace(info.Binary.Version) != latestVersion {
		return cloakBrowserDiagnostics{}, fmt.Errorf("官方最新版是 %s，但本机准备启动的是 %s，已经停止使用旧版", latestVersion, info.Binary.Version)
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "installed",
		Message: "官方最新版 Chromium 已安装，正在做启动检查", Percent: 91,
	})
	return info, nil
}

// runOfficialInstallWithRetry 在官方版本接口或下载网络短暂波动时自动重试安装。
func (m *Manager) runOfficialInstallWithRetry(ctx context.Context, licenseKey string) (string, error) {
	var output string
	var runErr error
	for attempt := 1; attempt <= officialInstallMaxAttempts; attempt++ {
		command := exec.CommandContext(ctx, m.NodePath(), m.cloakBrowserCLIPath(), "install")
		command.Dir = m.workerRoot()
		command.Env = m.cloakBrowserEnvironment(licenseKey, true)
		output, runErr = runStreamingCommand(command, licenseKey, m.updateOfficialInstallProgress)
		if runErr == nil || !retryableOfficialInstallError(output) || attempt == officialInstallMaxAttempts {
			return output, runErr
		}
		delay := time.Duration(attempt*2) * time.Second
		m.setInstallProgress(InstallProgress{
			Running: true, Component: "cloakbrowser", Stage: "resolve_version",
			Message: fmt.Sprintf("CloakBrowser 官方刚才有点忙，%d 秒后自动重试（%d/%d）", int(delay.Seconds()), attempt+1, officialInstallMaxAttempts),
			Percent: 41,
		})
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return output, ctx.Err()
		case <-timer.C:
		}
	}
	return output, runErr
}

// retryableOfficialInstallError 判断官方安装失败是否属于可安全重试的网络或服务波动。
func retryableOfficialInstallError(output string) bool {
	lower := strings.ToLower(output)
	for _, fragment := range []string{
		"could not determine latest pro version",
		"pro binary unavailable",
		"fetch failed",
		"econnreset",
		"econnrefused",
		"etimedout",
		"socket hang up",
		"network error",
		"http 429",
		"http 500",
		"http 502",
		"http 503",
		"http 504",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// smokeTestCloakBrowser 无界面启动官方浏览器并打开本地测试页，确认完整 Playwright 链路可用。
func (m *Manager) smokeTestCloakBrowser(ctx context.Context, licenseKey string) error {
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "smoke_test",
		Message: "正在试启动 Chromium，这一步通常只要几秒", Percent: 93,
	})
	smokePath := filepath.Join(m.workerRoot(), "dist", "runtime", "cloakbrowser-smoke-test.js")
	if !fileExists(smokePath) {
		return fmt.Errorf("浏览器启动检查脚本不存在：%s", smokePath)
	}
	testCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(testCtx, m.NodePath(), smokePath)
	command.Dir = m.workerRoot()
	command.Env = m.cloakBrowserEnvironment(licenseKey, false)
	output, err := runStreamingCommand(command, licenseKey, nil)
	if err != nil {
		return fmt.Errorf("最新版 Chromium 试启动失败：%s", commandFailure(output, err))
	}
	m.setInstallProgress(InstallProgress{
		Running: true, Component: "cloakbrowser", Stage: "smoke_test",
		Message: "最新版 Chromium 启动检查通过", Percent: 95,
	})
	return nil
}

// updateOfficialInstallProgress 把官方日志中的下载、校验和解压阶段转换为前端进度。
func (m *Manager) updateOfficialInstallProgress(line string) {
	if match := officialDownloadProgress.FindStringSubmatch(line); len(match) == 4 {
		percent, _ := strconv.Atoi(match[1])
		receivedMB, _ := strconv.ParseInt(match[2], 10, 64)
		totalMB, _ := strconv.ParseInt(match[3], 10, 64)
		m.setInstallProgress(InstallProgress{
			Running: true, Component: "cloakbrowser", Stage: "download",
			Message:  fmt.Sprintf("正在从 CloakBrowser 官方下载最新版 Chromium（%d%%）", percent),
			Percent:  42 + min(max(percent, 0), 100)*44/100,
			Received: receivedMB * 1024 * 1024, Total: totalMB * 1024 * 1024,
		})
		return
	}
	switch {
	case strings.Contains(line, "Downloading from"):
		m.setInstallProgress(InstallProgress{Running: true, Component: "cloakbrowser", Stage: "download", Message: "已连接 CloakBrowser 官方，开始下载最新版 Chromium", Percent: 42})
	case strings.Contains(line, "Checksum verified"):
		m.setInstallProgress(InstallProgress{Running: true, Component: "cloakbrowser", Stage: "verify", Message: "官方签名和 SHA256 校验通过", Percent: 88})
	case strings.Contains(line, "Extracting to"):
		m.setInstallProgress(InstallProgress{Running: true, Component: "cloakbrowser", Stage: "extract", Message: "正在解压最新版 Chromium", Percent: 89})
	case strings.Contains(line, "Binary ready"):
		m.setInstallProgress(InstallProgress{Running: true, Component: "cloakbrowser", Stage: "verify", Message: "Chromium 文件已就位，正在核对版本", Percent: 90})
	}
}

// cloakBrowserInfo 读取官方快速诊断结果，不启动浏览器也不触发大文件下载。
func (m *Manager) cloakBrowserInfo(ctx context.Context, licenseKey string) (cloakBrowserDiagnostics, error) {
	infoCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(infoCtx, m.NodePath(), m.cloakBrowserCLIPath(), "info", "--quick", "--json")
	command.Dir = m.workerRoot()
	command.Env = m.cloakBrowserEnvironment(licenseKey, false)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		output := sanitizeSensitive(stderr.String()+"\n"+stdout.String(), licenseKey)
		return cloakBrowserDiagnostics{}, fmt.Errorf("%s", commandFailure(output, err))
	}
	var info cloakBrowserDiagnostics
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return cloakBrowserDiagnostics{}, fmt.Errorf("官方诊断结果格式不正确：%w", err)
	}
	return info, nil
}

// clearOfficialVersionCheckCache 清除一小时检查缓存，确保本轮重新向官方确认最新版本。
func (m *Manager) clearOfficialVersionCheckCache() error {
	root := filepath.Join(m.runtimeDir, "cloakbrowser")
	for _, pattern := range []string{".last_pro_version_check_*", ".last_pro_version_resolution_*"} {
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, match := range matches {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("刷新 CloakBrowser 官方版本缓存失败：%w", err)
			}
		}
	}
	return nil
}

// freshOfficialVersion 读取本轮官方检查写入的 Stable 最新版本。
func (m *Manager) freshOfficialVersion() (string, error) {
	matches, _ := filepath.Glob(filepath.Join(m.runtimeDir, "cloakbrowser", ".last_pro_version_check_*"))
	for _, match := range matches {
		content, err := os.ReadFile(match)
		if err == nil && strings.TrimSpace(string(content)) != "" {
			return strings.TrimSpace(string(content)), nil
		}
	}
	return "", fmt.Errorf("没有从 CloakBrowser 官方确认到最新版，已停止使用旧缓存，请检查网络后重试")
}

// cloakBrowserEnvironment 返回官方安装和日常启动使用的干净环境变量。
func (m *Manager) cloakBrowserEnvironment(licenseKey string, allowUpdate bool) []string {
	autoUpdate := "false"
	if allowUpdate {
		autoUpdate = "true"
	}
	return overrideEnvironment(os.Environ(), []string{
		"CLOAKBROWSER_LICENSE_KEY=" + strings.TrimSpace(licenseKey),
		"CLOAKBROWSER_CACHE_DIR=" + filepath.Join(m.runtimeDir, "cloakbrowser"),
		"CLOAKBROWSER_RELEASE_CHANNEL=stable",
		"CLOAKBROWSER_AUTO_UPDATE=" + autoUpdate,
		"CLOAKBROWSER_BINARY_PATH=",
		"CLOAKBROWSER_DOWNLOAD_URL=",
		"CLOAKBROWSER_VERSION=",
		"CLOAKBROWSER_SKIP_CHECKSUM=false",
		"npm_config_registry=" + npmMirrorRegistry,
	})
}

// cloakBrowserCLIPath 返回当前 Worker 安装的官方 CloakBrowser 命令入口。
func (m *Manager) cloakBrowserCLIPath() string {
	return filepath.Join(filepath.Dir(m.WorkerDependencyPath()), "dist", "cli.js")
}

// workerRoot 返回 Worker 的 package.json、dist 和 node_modules 所在目录。
func (m *Manager) workerRoot() string {
	return filepath.Dir(filepath.Dir(m.entryPath))
}

// cloakBrowserWrapperReady 判断已安装包装器是否与发布包锁定版本一致。
func (m *Manager) cloakBrowserWrapperReady() bool {
	expected, err := readNodePackage(filepath.Join(m.workerRoot(), "package.json"))
	if err != nil {
		return false
	}
	installed, err := readNodePackage(m.WorkerDependencyPath())
	return err == nil && strings.TrimSpace(installed.Version) == strings.TrimSpace(expected.Dependencies["cloakbrowser"])
}

// workerDependenciesReady 检查 Worker 运行所需的三个生产依赖是否完整。
func (m *Manager) workerDependenciesReady() bool {
	if !m.cloakBrowserWrapperReady() {
		return false
	}
	for _, name := range []string{"playwright-core", "mmdb-lib"} {
		if !fileExists(filepath.Join(m.workerRoot(), "node_modules", name, "package.json")) {
			return false
		}
	}
	return true
}

// npmInstallCommand 使用 Node 自带 npm-cli.js，避免 Windows 弹出命令行窗口。
func (m *Manager) npmInstallCommand(ctx context.Context) (*exec.Cmd, error) {
	cli := m.npmCLIPath()
	if cli == "" {
		if goruntime.GOOS == "windows" {
			return nil, fmt.Errorf("Node 已安装，但没有找到 npm-cli.js")
		}
		npmPath, err := exec.LookPath("npm")
		if err != nil {
			return nil, fmt.Errorf("Node 已安装，但没有找到 npm：%w", err)
		}
		return exec.CommandContext(ctx, npmPath, m.npmInstallArguments()...), nil
	}
	arguments := append([]string{cli}, m.npmInstallArguments()...)
	return exec.CommandContext(ctx, m.NodePath(), arguments...), nil
}

// npmInstallArguments 返回开发目录和发布目录各自安全的锁文件安装参数。
func (m *Manager) npmInstallArguments() []string {
	arguments := []string{"ci", "--registry=" + npmMirrorRegistry, "--no-audit", "--no-fund"}
	if !fileExists(filepath.Join(m.workerRoot(), "tsconfig.json")) {
		arguments = append(arguments, "--omit=dev")
	}
	return arguments
}

// npmCLIPath 查找打包 Node 或常见系统 Node 目录里的 npm-cli.js。
func (m *Manager) npmCLIPath() string {
	if found := findFile(filepath.Join(m.runtimeDir, "node"), "npm-cli.js"); found != "" {
		return found
	}
	nodeDir := filepath.Dir(m.NodePath())
	for _, candidate := range []string{
		filepath.Join(nodeDir, "node_modules", "npm", "bin", "npm-cli.js"),
		filepath.Join(filepath.Dir(nodeDir), "lib", "node_modules", "npm", "bin", "npm-cli.js"),
	} {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// readNodePackage 读取 package.json 的版本和生产依赖。
func readNodePackage(path string) (nodePackage, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nodePackage{}, err
	}
	var result nodePackage
	if err = json.Unmarshal(content, &result); err != nil {
		return nodePackage{}, err
	}
	return result, nil
}

// overrideEnvironment 合并命令环境并让后写入变量覆盖系统遗留值。
func overrideEnvironment(base []string, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, value := range overrides {
		if key, _, ok := strings.Cut(value, "="); ok {
			keys[strings.ToUpper(key)] = struct{}{}
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, value := range base {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		if _, replaced := keys[strings.ToUpper(key)]; !replaced {
			result = append(result, value)
		}
	}
	return append(result, overrides...)
}

// runStreamingCommand 同时读取命令标准输出和错误输出，并逐行回调安装进度。
func runStreamingCommand(command *exec.Cmd, secret string, onLine func(string)) (string, error) {
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return "", err
	}
	if err = command.Start(); err != nil {
		return "", err
	}
	var wait sync.WaitGroup
	var outputMu sync.Mutex
	lines := make([]string, 0, 32)
	scan := func(reader io.Reader) {
		defer wait.Done()
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(sanitizeSensitive(scanner.Text(), secret))
			if line == "" {
				continue
			}
			outputMu.Lock()
			lines = append(lines, line)
			if len(lines) > 200 {
				lines = append([]string(nil), lines[len(lines)-200:]...)
			}
			outputMu.Unlock()
			if onLine != nil {
				onLine(line)
			}
		}
	}
	wait.Add(2)
	go scan(stdout)
	go scan(stderr)
	wait.Wait()
	runErr := command.Wait()
	outputMu.Lock()
	defer outputMu.Unlock()
	return strings.Join(lines, "\n"), runErr
}

// sanitizeSensitive 隐藏命令输出里意外出现的用户 Key。
func sanitizeSensitive(value string, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[Key 已隐藏]")
}

// commandFailure 返回命令最后一行可执行错误，避免把整段 npm 日志塞进页面。
func commandFailure(output string, runErr error) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	if runErr != nil {
		return runErr.Error()
	}
	return "未知错误"
}
