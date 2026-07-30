// Package config 负责读取新本地程序的启动配置并生成统一数据目录。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
)

const (
	// DefaultHost 是 Go 本地服务默认监听地址。
	DefaultHost = "127.0.0.1"
	// DefaultPort 是 Go 本地服务默认端口。
	DefaultPort = 43129
	// DefaultWorkerPort 是 TypeScript Worker 默认端口。
	DefaultWorkerPort = 39881
)

// DefaultCloudURL 是开发环境默认云端地址，正式打包时通过 ldflags 固定为线上地址。
var DefaultCloudURL = "http://127.0.0.1:8084"

// DefaultConsoleURL 是开发环境默认前端地址，正式打包时通过 ldflags 固定为线上地址。
var DefaultConsoleURL = "http://localhost:5173"

// Config 保存本地程序全部基础配置。
type Config struct {
	Host            string
	Port            int
	WorkerHost      string
	WorkerPort      int
	CloudURL        string
	ConsoleURL      string
	DataDir         string
	ProfilesDir     string
	ExtensionsDir   string
	DownloadsDir    string
	ScreenshotsDir  string
	LogsDir         string
	RuntimeDir      string
	DatabasePath    string
	WorkerEntryPath string
	NodePath        string
	OCRExecutable   string
	AutoOpenConsole bool
}

// Load 从启动参数和环境变量加载配置并创建必要目录。
func Load(host string, port int, dataDir string) (Config, error) {
	resolvedDataDir, err := resolveDataDir(dataDir)
	if err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(host) == "" {
		host = DefaultHost
	}
	if port <= 0 {
		port = DefaultPort
	}
	workerPort := envInt("GOODHR_WORKER_PORT", DefaultWorkerPort)
	runtimeDir := filepath.Join(resolvedDataDir, "runtime")
	cfg := Config{
		Host:            host,
		Port:            port,
		WorkerHost:      DefaultHost,
		WorkerPort:      workerPort,
		CloudURL:        envString("GOODHR_CLOUD_API_BASE", DefaultCloudURL),
		ConsoleURL:      envString("GOODHR_CONSOLE_URL", DefaultConsoleURL),
		DataDir:         resolvedDataDir,
		ProfilesDir:     filepath.Join(resolvedDataDir, "profiles"),
		ExtensionsDir:   filepath.Join(resolvedDataDir, "extensions"),
		DownloadsDir:    defaultDownloadsDir(),
		ScreenshotsDir:  filepath.Join(resolvedDataDir, "screenshots"),
		LogsDir:         filepath.Join(resolvedDataDir, "logs"),
		RuntimeDir:      runtimeDir,
		DatabasePath:    filepath.Join(resolvedDataDir, "goodhr-local.db"),
		WorkerEntryPath: defaultWorkerEntry(),
		NodePath:        defaultNodePath(runtimeDir),
		OCRExecutable:   envString("GOODHR_OCR_EXECUTABLE", filepath.Join(runtimeDir, "ocr", "RapidOCR-json")),
		AutoOpenConsole: envBool("GOODHR_AUTO_OPEN_CONSOLE", true),
	}
	if err := cfg.EnsureDirectories(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// defaultWorkerEntry 按环境变量、安装目录和源码目录查找 Worker 编译入口。
func defaultWorkerEntry() string {
	if value := strings.TrimSpace(os.Getenv("GOODHR_WORKER_ENTRY")); value != "" {
		return filepath.Clean(value)
	}
	candidates := make([]string, 0, 4)
	if executable, err := os.Executable(); err == nil {
		base := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(base, "worker", "dist", "main.js"),
			filepath.Join(base, "..", "worker", "dist", "main.js"),
		)
	}
	if _, source, _, ok := goruntime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(source), "..", "..", "worker", "dist", "main.js"))
	}
	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(workingDir, "worker", "dist", "main.js"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate)
		}
	}
	if len(candidates) > 0 {
		return filepath.Clean(candidates[0])
	}
	return filepath.Join("worker", "dist", "main.js")
}

// defaultNodePath 优先使用配置和随程序打包的 Node.js，否则使用系统 PATH。
func defaultNodePath(runtimeDir string) string {
	if value := strings.TrimSpace(os.Getenv("GOODHR_NODE_PATH")); value != "" {
		return value
	}
	packaged := filepath.Join(runtimeDir, "node", "bin", "node")
	if info, err := os.Stat(packaged); err == nil && !info.IsDir() {
		return packaged
	}
	if systemNode, err := exec.LookPath("node"); err == nil {
		return systemNode
	}
	return packaged
}

// EnsureDirectories 创建程序运行所需目录。
func (c Config) EnsureDirectories() error {
	directories := []string{
		c.DataDir,
		c.ProfilesDir,
		c.ExtensionsDir,
		c.DownloadsDir,
		c.ScreenshotsDir,
		c.LogsDir,
		c.RuntimeDir,
	}
	for _, directory := range directories {
		if strings.TrimSpace(directory) == "" {
			return fmt.Errorf("本地目录不能为空")
		}
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败：%w", directory, err)
		}
	}
	return nil
}

// ExtensionPaths 返回扩展目录下可以交给 CloakBrowser 加载的一级子目录。
// 无效扩展目录会被忽略，避免一个放错位置的文件阻断浏览器启动。
func (c Config) ExtensionPaths() []string {
	entries, err := os.ReadDir(c.ExtensionsDir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(c.ExtensionsDir, entry.Name())
		content, err := os.ReadFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			continue
		}
		var manifest struct {
			Name            string `json:"name"`
			Version         string `json:"version"`
			ManifestVersion int    `json:"manifest_version"`
		}
		if json.Unmarshal(content, &manifest) != nil ||
			strings.TrimSpace(manifest.Name) == "" ||
			strings.TrimSpace(manifest.Version) == "" ||
			(manifest.ManifestVersion != 2 && manifest.ManifestVersion != 3) {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// Address 返回 Go 本地服务监听地址。
func (c Config) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// WorkerURL 返回 Browser Worker 基础地址。
func (c Config) WorkerURL() string {
	return fmt.Sprintf("http://%s:%d", c.WorkerHost, c.WorkerPort)
}

// ConsolePageURL 返回前端管理页面地址，供全部自动打开入口统一使用。
func (c Config) ConsolePageURL() string {
	return strings.TrimRight(c.ConsoleURL, "/") + "/admin/"
}

// resolveDataDir 返回用户配置目录中的 GoodHR 新本地程序目录。
func resolveDataDir(value string) (string, error) {
	if resolved := strings.TrimSpace(value); resolved != "" {
		return filepath.Clean(resolved), nil
	}
	if resolved := strings.TrimSpace(os.Getenv("GOODHR_DATA_DIR")); resolved != "" {
		return filepath.Clean(resolved), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("读取用户配置目录失败：%w", err)
	}
	return filepath.Join(base, "GoodHR", "local-agent-new"), nil
}

// defaultDownloadsDir 返回 macOS 用户下载目录。
func defaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "GoodHR", "Downloads")
	}
	return filepath.Join(home, "Downloads")
}

// envString 读取非空环境变量，否则使用默认值。
func envString(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// envInt 读取合法端口环境变量，否则使用默认值。
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 || value > 65535 {
		return fallback
	}
	return value
}

// envBool 读取常见真假环境变量，否则使用默认值。
func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
