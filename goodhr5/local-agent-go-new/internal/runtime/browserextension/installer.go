// Package browserextension 文件作用：安全下载、校验并原子准备 GoodHR Chrome 与 Firefox 浏览器插件目录。
package browserextension

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	releasePath              = "/api/system/browser-extension"
	expectedExtensionName    = "GoodHR 简历助手"
	expectedFirefoxID        = "resume-assistant@goodhr.58it.cn"
	managedFirefoxDirName    = "goodhr-resume-assistant"
	defaultMaxArchiveBytes   = int64(100 << 20)
	defaultMaxExtractedBytes = int64(256 << 20)
	defaultMaxArchiveEntries = 5000
	maxReleaseBytes          = int64(1 << 20)
	receiptFilename          = ".goodhr-release.json"
	loadRequiredFilename     = ".goodhr-load-required"
	signedFirefoxXPIFilename = ".goodhr-signed.xpi"
)

type browserKind string

const (
	browserChrome           browserKind = "chrome"
	browserFirefox          browserKind = "firefox"
	browserFirefoxTemporary browserKind = "firefox-temporary"
)

// ManagedFirefoxDestination 返回托管 Firefox 插件的稳定代码目录，不包含 Profile 或登录数据。
func ManagedFirefoxDestination(extensionsDir string) (string, error) {
	extensionsDir = strings.TrimSpace(extensionsDir)
	if extensionsDir == "" {
		return "", fmt.Errorf("Firefox 扩展根目录不能为空")
	}
	return filepath.Join(extensionsDir, managedFirefoxDirName), nil
}

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
	UsedCached     bool
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
	return prepareForBrowser(ctx, options, browserChrome)
}

// PrepareFirefox 下载并校验已签名 Firefox 插件，同时保留原始 XPI 供持久 Profile 安装。
func PrepareFirefox(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.DestinationDir) == "" {
		return Result{}, fmt.Errorf("Firefox 插件稳定目录不能为空")
	}
	return prepareForBrowser(ctx, options, browserFirefox)
}

// prepareForBrowser 复用统一下载与原子切换流程，并按浏览器执行发布字段和清单校验。
func prepareForBrowser(ctx context.Context, options Options, browser browserKind) (result Result, resultErr error) {
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
	unlock, err := lockDestination(ctx, destination)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	requestCtx := ctx
	if browser == browserFirefoxTemporary {
		defer func() {
			if requestCtx.Err() != nil {
				result, resultErr = Result{}, requestCtx.Err()
				return
			}
			if resultErr == nil {
				return
			}
			cached, cacheErr := validatedTemporaryCache(destination)
			if cacheErr == nil && requestCtx.Err() == nil {
				cached.UsedCached = true
				log.Printf("event=firefox_extension_update_deferred version=%q kept_verified=true reason=%q", cached.Version, fmt.Sprintf("%.180s", resultErr))
				result, resultErr = cached, nil
			}
		}()
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
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

	var published release
	var downloadURL string
	if browser == browserFirefoxTemporary {
		published, downloadURL, err = fetchTemporaryFirefoxRelease(ctx, client, options.CloudBaseURL)
		if err == nil {
			client, err = sameOriginDownloadClient(client, options.CloudBaseURL)
		}
	} else if browser == browserFirefox {
		published, downloadURL, err = fetchFirefoxRelease(ctx, client, options.CloudBaseURL)
	} else {
		published, downloadURL, err = fetchRelease(ctx, client, options.CloudBaseURL)
	}
	if err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	if browser == browserFirefoxTemporary {
		if cached, cacheErr := validatedTemporaryCache(destination); cacheErr == nil && olderTemporaryVersion(published.Version, cached.Version) {
			cached.UsedCached = true
			log.Printf("event=firefox_extension_update_deferred version=%q downgrade_rejected=true", cached.Version)
			return cached, nil
		}
	}
	releaseSHA256 := published.SHA256Chrome
	if browser != browserChrome {
		releaseSHA256 = published.SHA256Firefox
	}
	current, manifestErr := readManifest(filepath.Join(destination, "manifest.json"))
	receipt, receiptErr := readReceipt(filepath.Join(destination, receiptFilename))
	currentTreeSHA256, treeErr := directoryTreeSHA256(destination)
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	signedXPIReady := browser != browserFirefox
	if browser == browserFirefox {
		actualSHA256, hashErr := fileSHA256(filepath.Join(destination, signedFirefoxXPIFilename))
		signedXPIReady = hashErr == nil && actualSHA256 == releaseSHA256
	}
	channelReady := browser != browserFirefoxTemporary || receipt.Channel == temporaryChannel
	if browser == browserFirefox {
		channelReady = receipt.Channel == ""
	}
	if manifestErr == nil && receiptErr == nil && treeErr == nil && signedXPIReady && channelReady && current.Version == published.Version && validateManifest(browser, destination, current) == nil && receipt.Version == published.Version && receipt.SHA256 == releaseSHA256 && receipt.TreeSHA256 == currentTreeSHA256 {
		return Result{Version: published.Version, Directory: destination, AlreadyCurrent: true}, nil
	}

	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0o755); err != nil {
		return Result{}, fmt.Errorf("创建插件目录失败：%w", err)
	}
	archivePath, err := downloadArchive(ctx, client, downloadURL, releaseSHA256, parent, maxArchiveBytes)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(archivePath)
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}

	staging, err := os.MkdirTemp(parent, "."+string(browser)+"-stage-")
	if err != nil {
		return Result{}, fmt.Errorf("创建插件临时目录失败：%w", err)
	}
	defer os.RemoveAll(staging)
	if err = extractArchive(archivePath, staging, maxExtractedBytes); err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	manifest, err := readManifest(filepath.Join(staging, "manifest.json"))
	if err != nil {
		return Result{}, fmt.Errorf("读取插件 manifest.json 失败：%w", err)
	}
	if err = validateManifest(browser, staging, manifest); err != nil {
		return Result{}, err
	}
	if manifest.Version != published.Version {
		return Result{}, fmt.Errorf("插件版本校验失败：配置为 %s，压缩包为 %s", published.Version, manifest.Version)
	}
	if browser == browserFirefoxTemporary {
		if _, statErr := os.Lstat(filepath.Join(staging, signedFirefoxXPIFilename)); !os.IsNotExist(statErr) {
			return Result{}, fmt.Errorf("临时 Firefox 包不能包含签名安装器保留文件")
		}
	}
	if browser == browserFirefox {
		signedXPIPath := filepath.Join(staging, signedFirefoxXPIFilename)
		if _, statErr := os.Lstat(signedXPIPath); !os.IsNotExist(statErr) {
			return Result{}, fmt.Errorf("Firefox 插件压缩包包含保留文件名或无法检查：%v", statErr)
		}
		if err = os.Rename(archivePath, signedXPIPath); err != nil {
			return Result{}, fmt.Errorf("保存 Mozilla 签名原包失败：%w", err)
		}
	}
	treeSHA256, err := directoryTreeSHA256(staging)
	if err != nil {
		return Result{}, fmt.Errorf("计算插件目录摘要失败：%w", err)
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	newReceipt := releaseReceipt{Version: published.Version, SHA256: releaseSHA256, TreeSHA256: treeSHA256}
	if browser == browserFirefoxTemporary {
		newReceipt.Channel = temporaryChannel
	}
	if err = writeReceipt(filepath.Join(staging, receiptFilename), newReceipt); err != nil {
		return Result{}, err
	}
	if err = writeReceipt(filepath.Join(staging, loadRequiredFilename), newReceipt); err != nil {
		return Result{}, err
	}
	if err = ctx.Err(); err != nil {
		return Result{}, err
	}
	if err = replaceDirectory(destination, staging, os.Rename); err != nil {
		return Result{}, err
	}
	return Result{Version: published.Version, Directory: destination}, nil
}

// validateManifest 按目标浏览器核对固定产品身份、后台入口和 Firefox 扩展编号。
func validateManifest(browser browserKind, root string, manifest extensionManifest) error {
	if browser == browserFirefoxTemporary {
		return validateFirefoxIdentity(root, manifest)
	}
	if browser == browserFirefox {
		return validateFirefoxManifest(root, manifest)
	}
	return validateChromeManifest(root, manifest)
}
