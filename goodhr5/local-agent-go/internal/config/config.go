// Package config 负责管理 Go 版本本地程序的路径和启动配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultHost 是本地程序默认监听地址。
	DefaultHost = "127.0.0.1"
	// DefaultPort 是本地程序默认优先监听端口。
	DefaultPort = 95271
	// MaxPort 是本地程序端口自动探测的最大端口。
	MaxPort = 95279
	// AppName 是本地数据目录名称。
	AppName = "GoodHR"
	// DefaultRuntimeManifestURL 是运行组件下载清单默认地址。
	DefaultRuntimeManifestURL = "https://goodhr5.58it.cn/downloads/goodhr-local-runtime-manifest.json"
	// DefaultConsoleManifestURL 是控制台前端包下载清单默认地址。
	DefaultConsoleManifestURL = "https://goodhr5.58it.cn/downloads/goodhr-console-manifest.json"
	// DefaultCloudAPIBase 是本地程序默认访问的云端接口地址。
	DefaultCloudAPIBase = "https://goodhr5.58it.cn"
)

// Config 保存本地程序运行配置。
type Config struct {
	Host               string
	Port               int
	DataDir            string
	RuntimeDir         string
	OCRDir             string
	FrontendDir        string
	ProfilesDir        string
	DownloadsDir       string
	ScreenshotsDir     string
	ManifestURL        string
	ConsoleManifestURL string
	CloudAPIBase       string
	AutoOpenConsole    bool
}

// New 创建本地程序配置。
// host 为监听地址，port 为优先监听端口，返回完整配置。
func New(host string, port int) (*Config, error) {
	return NewWithDataDir(host, port, "")
}

// NewWithDataDir 创建本地程序配置。
// host 为监听地址，port 为优先监听端口，customDataDir 为用户指定数据目录。
func NewWithDataDir(host string, port int, customDataDir string) (*Config, error) {
	if host == "" {
		host = DefaultHost
	}
	if port <= 0 {
		port = DefaultPort
	}
	dataDir := strings.TrimSpace(customDataDir)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("GOODHR_DATA_DIR"))
	}
	if dataDir == "" {
		var err error
		dataDir, err = defaultDataDir()
		if err != nil {
			return nil, err
		}
	}
	cfg := &Config{
		Host:               host,
		Port:               port,
		DataDir:            dataDir,
		RuntimeDir:         filepath.Join(dataDir, "runtime"),
		OCRDir:             filepath.Join(dataDir, "runtime", "ocr"),
		FrontendDir:        filepath.Join(dataDir, "console"),
		ProfilesDir:        filepath.Join(dataDir, "profiles"),
		DownloadsDir:       defaultDownloadsDir(),
		ScreenshotsDir:     filepath.Join(dataDir, "screenshots"),
		ManifestURL:        envOrDefault("GOODHR_RUNTIME_MANIFEST_URL", DefaultRuntimeManifestURL),
		ConsoleManifestURL: envOrDefault("GOODHR_CONSOLE_MANIFEST_URL", DefaultConsoleManifestURL),
		CloudAPIBase:       envOrDefault("GOODHR_CLOUD_API_BASE", DefaultCloudAPIBase),
		AutoOpenConsole:    envOrDefault("GOODHR_AUTO_OPEN_CONSOLE", "true") != "false",
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// envOrDefault 读取环境变量，环境变量为空时返回默认值。
// key 为环境变量名，fallback 为默认值。
func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// EnsureDirs 创建本地程序需要的基础目录。
// 返回错误表示目录创建失败。
func (c *Config) EnsureDirs() error {
	for _, dir := range []string{c.DataDir, c.RuntimeDir, c.OCRDir, c.FrontendDir, c.ProfilesDir, c.DownloadsDir, c.ScreenshotsDir} {
		if dir == "" {
			return fmt.Errorf("本地目录为空")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败 %s：%w", dir, err)
		}
	}
	return nil
}

// Address 返回当前配置的监听地址。
// 返回值格式为 host:port。
func (c *Config) Address(port int) string {
	if port <= 0 {
		port = c.Port
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

// defaultDataDir 返回默认本地数据目录。
// macOS 下通常位于 ~/Library/Application Support/GoodHR。
func defaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("读取用户配置目录失败：%w", err)
	}
	return filepath.Join(base, AppName), nil
}

// defaultDownloadsDir 返回默认浏览器下载目录。
// 优先使用用户系统下载目录，读取失败时使用本地数据目录兜底。
func defaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "GoodHR", "Downloads")
	}
	return filepath.Join(home, "Downloads")
}
