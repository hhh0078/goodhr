// Package browserextension 文件作用：安全下载、校验并原子准备 GoodHR Chrome 浏览器插件目录。
package browserextension

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	releasePath              = "/api/system/browser-extension"
	expectedExtensionName    = "GoodHR 简历助手"
	defaultMaxArchiveBytes   = int64(100 << 20)
	defaultMaxExtractedBytes = int64(256 << 20)
	defaultMaxArchiveEntries = 5000
	maxReleaseBytes          = int64(1 << 20)
	receiptFilename          = ".goodhr-release.json"
	loadRequiredFilename     = ".goodhr-load-required"
)

// Options 定义插件准备流程的云端、目录和下载限制。
type Options struct {
	CloudBaseURL     string
	DestinationDir   string
	HTTPClient       *http.Client
	MaxArchiveBytes  int64
	MaxExtractedSize int64
}

// Result 返回本次准备好的插件版本和稳定目录。
type Result struct {
	Version        string
	Directory      string
	AlreadyCurrent bool
}

// DefaultDestinationDir 返回当前用户专用的 Chrome 插件稳定目录。
func DefaultDestinationDir() (string, error) {
	base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("读取当前用户本地目录失败：%w", err)
		}
	}
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("当前用户本地目录不能为空")
	}
	return filepath.Join(base, "GoodHRLocalAgent", "browser-extension", "chrome"), nil
}

// Prepare 读取云端最新 Chrome 插件配置，完成下载、校验、安全解压和原子替换。
func Prepare(ctx context.Context, options Options) (Result, error) {
	destination := strings.TrimSpace(options.DestinationDir)
	if destination == "" {
		var err error
		destination, err = DefaultDestinationDir()
		if err != nil {
			return Result{}, err
		}
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return Result{}, fmt.Errorf("解析插件目录失败：%w", err)
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	maxArchiveBytes := options.MaxArchiveBytes
	if maxArchiveBytes <= 0 {
		maxArchiveBytes = defaultMaxArchiveBytes
	}
	maxExtractedBytes := options.MaxExtractedSize
	if maxExtractedBytes <= 0 {
		maxExtractedBytes = defaultMaxExtractedBytes
	}

	release, downloadURL, err := fetchRelease(ctx, client, options.CloudBaseURL)
	if err != nil {
		return Result{}, err
	}
	current, manifestErr := readManifest(filepath.Join(destination, "manifest.json"))
	receipt, receiptErr := readReceipt(filepath.Join(destination, receiptFilename))
	currentTreeSHA256, treeErr := directoryTreeSHA256(destination)
	if manifestErr == nil && receiptErr == nil && treeErr == nil && current.Version == release.Version && validateChromeManifest(destination, current) == nil && receipt.Version == release.Version && receipt.SHA256 == release.SHA256Chrome && receipt.TreeSHA256 == currentTreeSHA256 {
		return Result{Version: release.Version, Directory: destination, AlreadyCurrent: true}, nil
	}

	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建插件目录失败：%w", err)
	}
	archivePath, err := downloadArchive(ctx, client, downloadURL, release.SHA256Chrome, parent, maxArchiveBytes)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(archivePath)

	staging, err := os.MkdirTemp(parent, ".chrome-stage-")
	if err != nil {
		return Result{}, fmt.Errorf("创建插件临时目录失败：%w", err)
	}
	defer os.RemoveAll(staging)
	if err = extractArchive(archivePath, staging, maxExtractedBytes); err != nil {
		return Result{}, err
	}
	manifest, err := readManifest(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return Result{}, fmt.Errorf("读取插件 manifest.json 失败：%w", err)
	}
	if err = validateChromeManifest(staging, manifest); err != nil {
		return Result{}, err
	}
	if manifest.Version != release.Version {
		return Result{}, fmt.Errorf("插件版本校验失败：配置为 %s，压缩包为 %s", release.Version, manifest.Version)
	}
	treeSHA256, err := directoryTreeSHA256(staging)
	if err != nil {
		return Result{}, fmt.Errorf("计算插件目录摘要失败：%w", err)
	}
	newReceipt := releaseReceipt{Version: release.Version, SHA256: release.SHA256Chrome, TreeSHA256: treeSHA256}
	if err = writeReceipt(filepath.Join(staging, receiptFilename), newReceipt); err != nil {
		return Result{}, err
	}
	if err = writeReceipt(filepath.Join(staging, loadRequiredFilename), newReceipt); err != nil {
		return Result{}, err
	}
	if err = replaceDirectory(destination, staging, os.Rename); err != nil {
		return Result{}, err
	}
	return Result{Version: release.Version, Directory: destination}, nil
}
