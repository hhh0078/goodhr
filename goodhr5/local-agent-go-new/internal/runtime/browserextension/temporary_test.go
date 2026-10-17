// 文件作用：验证临时 Firefox 通道的同源校验、离线完整性、版本保护和并发更新，不访问真实发布服务。
package browserextension

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// temporaryTransport 用可控的本机内存响应验证下载完成和发布之间的取消竞态。
type temporaryTransport func(*http.Request) (*http.Response, error)

// RoundTrip 实现测试请求传输器，不请求外部网络。
func (transport temporaryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// cancelAtEOF 在下载全部字节时取消原始操作，模拟服务端回完后用户已经放弃的边界。
type cancelAtEOF struct {
	io.Reader
	cancel context.CancelFunc
}

// Read 保留全部下载字节，仅在流结束时触发调用方取消。
func (reader cancelAtEOF) Read(buffer []byte) (int, error) {
	n, err := reader.Reader.Read(buffer)
	if err == io.EOF {
		reader.cancel()
	}
	return n, err
}

// temporaryFixture 创建独立同源清单和无签名插件包，允许测试切换发布失败及过期版本。
func temporaryFixture(t *testing.T) (Options, *atomic.Bool, *atomic.Value, *atomic.Int32) {
	t.Helper()
	archive := buildFirefoxXPI(t, "1.0.16", expectedFirefoxID, "synthetic-background", false)
	digest := sha256.Sum256(archive)
	failed, published, downloads := &atomic.Bool{}, &atomic.Value{}, &atomic.Int32{}
	published.Store(temporaryRelease{Version: "1.0.16", URL: "/plugin.zip", SHA256: hex.EncodeToString(digest[:]), ExtensionID: expectedFirefoxID, Channel: temporaryChannel})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case temporaryReleasePath:
			if failed.Load() {
				http.Error(w, "synthetic offline", http.StatusServiceUnavailable)
				return
			}
			_ = json.NewEncoder(w).Encode(published.Load().(temporaryRelease))
		case "/plugin.zip":
			downloads.Add(1)
			_, _ = w.Write(archive)
		default:
			t.Errorf("临时通道请求了非约定地址：%s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return Options{CloudBaseURL: server.URL, DestinationDir: filepath.Join(t.TempDir(), managedFirefoxDirName), HTTPClient: server.Client()}, failed, published, downloads
}

// TestTemporaryInstallAndConcurrentIdempotence 验证无签名包安全安装，所有并发调用只下载一次且不保留旧代码。
func TestTemporaryInstallAndConcurrentIdempotence(t *testing.T) {
	opts, _, _, downloads := temporaryFixture(t)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := PrepareFirefoxTemporary(context.Background(), opts)
			if err != nil || result.Version != "1.0.16" || result.UsedCached {
				t.Errorf("临时安装失败：%+v %v", result, err)
			}
		}()
	}
	workers.Wait()
	if downloads.Load() != 1 {
		t.Fatalf("并发重复下载：%d", downloads.Load())
	}
	receipt, err := readReceipt(filepath.Join(opts.DestinationDir, receiptFilename))
	if err != nil || receipt.Channel != temporaryChannel {
		t.Fatalf("临时收据错误：%+v %v", receipt, err)
	}
	for _, target := range []string{filepath.Join(opts.DestinationDir, signedFirefoxXPIFilename), opts.DestinationDir + ".previous"} {
		if _, err = os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("不应保留签名安装器或旧来源：%s %v", target, err)
		}
	}
}

// TestTemporaryFailureUsesOnlyVerifiedCache 验证联网失败可复用完整临时包，篡改、签名旧收据或错误通道不能回退。
func TestTemporaryFailureUsesOnlyVerifiedCache(t *testing.T) {
	opts, failed, _, _ := temporaryFixture(t)
	if _, err := PrepareFirefoxTemporary(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	failed.Store(true)
	result, err := PrepareFirefoxTemporary(context.Background(), opts)
	if err != nil || !result.UsedCached || result.Version != "1.0.16" {
		t.Fatalf("没有回退完整包：%+v %v", result, err)
	}
	if err = os.WriteFile(filepath.Join(opts.DestinationDir, "src", "background.js"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = PrepareFirefoxTemporary(context.Background(), opts); err == nil {
		t.Fatal("被篡改目录仍被用作离线缓存")
	}
	failed.Store(false)
	if _, err = PrepareFirefoxTemporary(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	receipt, err := readReceipt(filepath.Join(opts.DestinationDir, receiptFilename))
	if err != nil {
		t.Fatal(err)
	}
	receipt.Channel = ""
	if err = writeReceipt(filepath.Join(opts.DestinationDir, receiptFilename), receipt); err != nil {
		t.Fatal(err)
	}
	failed.Store(true)
	if _, err = PrepareFirefoxTemporary(context.Background(), opts); err == nil {
		t.Fatal("签名旧收据被当作临时通道回退")
	}
}

// TestTemporaryStaleReleaseAndBadUpgradeKeepCurrent 验证过期元数据或损坏的新包不会把新版降回正式旧版。
func TestTemporaryStaleReleaseAndBadUpgradeKeepCurrent(t *testing.T) {
	opts, _, published, downloads := temporaryFixture(t)
	if _, err := PrepareFirefoxTemporary(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	meta := published.Load().(temporaryRelease)
	meta.Version = "1.0.9"
	published.Store(meta)
	result, err := PrepareFirefoxTemporary(context.Background(), opts)
	if err != nil || result.Version != "1.0.16" || !result.UsedCached || downloads.Load() != 1 {
		t.Fatalf("新版被降级：%+v %v downloads=%d", result, err, downloads.Load())
	}
	meta.Version, meta.SHA256 = "1.0.17", strings.Repeat("a", 64)
	published.Store(meta)
	result, err = PrepareFirefoxTemporary(context.Background(), opts)
	if err != nil || result.Version != "1.0.16" || !result.UsedCached {
		t.Fatalf("损坏升级未保留旧完整包：%+v %v", result, err)
	}
}

// TestTemporaryRejectsInvalidMetadata 验证身份、版本、通道、摘要及同源包地址在下载前被检查。
func TestTemporaryRejectsInvalidMetadata(t *testing.T) {
	changes := map[string]func(*temporaryRelease){
		"wrong-id":          func(m *temporaryRelease) { m.ExtensionID = "other@example.test" },
		"wrong-channel":     func(m *temporaryRelease) { m.Channel = "signed" },
		"bad-version":       func(m *temporaryRelease) { m.Version = "1.0.16-beta" },
		"leading-zero":      func(m *temporaryRelease) { m.Version = "1.00.16" },
		"missing-digest":    func(m *temporaryRelease) { m.SHA256 = "" },
		"upper-digest":      func(m *temporaryRelease) { m.SHA256 = strings.Repeat("A", 64) },
		"cross-origin":      func(m *temporaryRelease) { m.URL = "https://evil.example/plugin.zip" },
		"protocol-relative": func(m *temporaryRelease) { m.URL = "//evil.example/plugin.zip" },
		"relative-path":     func(m *temporaryRelease) { m.URL = "plugin.zip" },
		"non-archive":       func(m *temporaryRelease) { m.URL = "/plugin.js" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			opts, _, published, downloads := temporaryFixture(t)
			meta := published.Load().(temporaryRelease)
			change(&meta)
			published.Store(meta)
			if _, err := PrepareFirefoxTemporary(context.Background(), opts); err == nil {
				t.Fatal("错误清单被接受")
			}
			if downloads.Load() != 0 {
				t.Fatal("错误清单触发下载")
			}
		})
	}
}

// TestTemporaryRedirectAndArchiveIdentity 验证同源限制覆盖每次跳转，错误包身份不能进入稳定目录。
func TestTemporaryRedirectAndArchiveIdentity(t *testing.T) {
	for _, item := range []struct {
		name     string
		archive  []byte
		redirect bool
	}{
		{name: "wrong-id", archive: buildFirefoxXPI(t, "1.0.16", "wrong-id", "background", false)},
		{name: "wrong-version", archive: buildFirefoxXPI(t, "1.0.9", expectedFirefoxID, "background", false)},
		{name: "cross-origin-redirect", redirect: true},
		{name: "missing-background", archive: buildZip(t, []zipEntry{{name: "manifest.json", body: firefoxManifest(expectedExtensionName, "1.0.16", expectedFirefoxID)}})},
	} {
		t.Run(item.name, func(t *testing.T) {
			digest := sha256.Sum256(item.archive)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == temporaryReleasePath {
					_ = json.NewEncoder(w).Encode(temporaryRelease{Version: "1.0.16", URL: "/plugin.zip", SHA256: hex.EncodeToString(digest[:]), ExtensionID: expectedFirefoxID, Channel: temporaryChannel})
					return
				}
				if item.redirect {
					http.Redirect(w, r, "https://evil.example/plugin.zip", http.StatusFound)
					return
				}
				_, _ = w.Write(item.archive)
			}))
			defer server.Close()
			_, err := PrepareFirefoxTemporary(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: filepath.Join(t.TempDir(), managedFirefoxDirName), HTTPClient: server.Client()})
			if err == nil {
				t.Fatal("不安全插件被接受")
			}
		})
	}
}

// TestManagedPathsRemoveOnlyDuplicateIdentityAndOwnBackup 验证首装补路径、托管残留备份和同 ID 旧来源被排除，其它扩展保留。
func TestManagedPathsRemoveOnlyDuplicateIdentityAndOwnBackup(t *testing.T) {
	root := t.TempDir()
	managed := filepath.Join(root, managedFirefoxDirName)
	old := filepath.Join(root, "manual-old")
	if err := os.MkdirAll(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "manifest.json"), []byte(firefoxManifest(expectedExtensionName, "1.0.9", expectedFirefoxID)), 0o600); err != nil {
		t.Fatal(err)
	}
	custom, bookmark := filepath.Join(root, "custom.previous"), filepath.Join(root, "profile", "bookmark")
	paths := WithManagedFirefoxPath([]string{managed + ".previous", old, custom, bookmark, custom}, managed)
	if len(paths) != 3 || paths[0] != managed || paths[1] != custom || paths[2] != bookmark {
		t.Fatalf("扩展来源不正确：%v", paths)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("不应删除用户旧扩展目录")
	}
}

// TestDestinationLockWaitIsCancellable 验证等待其它更新时可及时取消，不在调用结束后偷偷继续写文件。
func TestDestinationLockWaitIsCancellable(t *testing.T) {
	destination := t.TempDir()
	unlock, err := lockDestination(context.Background(), destination)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = lockDestination(ctx, destination); err == nil {
		t.Fatal("等待中的更新未取消")
	}
}

// TestCancelledDownloadedUpgradeDoesNotPublish 验证下载结束后取消不会替换目录，也不会把旧缓存伪装为本次成功。
func TestCancelledDownloadedUpgradeDoesNotPublish(t *testing.T) {
	opts, _, _, _ := temporaryFixture(t)
	if _, err := PrepareFirefoxTemporary(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	archive := buildFirefoxXPI(t, "1.0.17", expectedFirefoxID, "new-background", false)
	digest := sha256.Sum256(archive)
	metadata, _ := json.Marshal(temporaryRelease{Version: "1.0.17", URL: "/plugin.zip", SHA256: hex.EncodeToString(digest[:]), ExtensionID: expectedFirefoxID, Channel: temporaryChannel})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts.HTTPClient = &http.Client{Transport: temporaryTransport(func(request *http.Request) (*http.Response, error) {
		var body io.Reader = bytes.NewReader(metadata)
		if request.URL.Path == "/plugin.zip" {
			body = cancelAtEOF{Reader: bytes.NewReader(archive), cancel: cancel}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body), Request: request}, nil
	})}
	if _, err := PrepareFirefoxTemporary(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后仍报告成功：%v", err)
	}
	manifest, err := readManifest(filepath.Join(opts.DestinationDir, "manifest.json"))
	if err != nil || manifest.Version != "1.0.16" {
		t.Fatalf("取消后仍发布了新目录：%+v %v", manifest, err)
	}
}

// TestTemporaryInterruptedBackupIsRecovered 验证上次目录切换中断后，成功准备会恢复稳定路径并清理自己的备份。
func TestTemporaryInterruptedBackupIsRecovered(t *testing.T) {
	opts, _, _, _ := temporaryFixture(t)
	if _, err := PrepareFirefoxTemporary(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(opts.DestinationDir, opts.DestinationDir+".previous"); err != nil {
		t.Fatal(err)
	}
	result, err := PrepareFirefoxTemporary(context.Background(), opts)
	if err != nil || result.Version != "1.0.16" {
		t.Fatalf("中断后的插件来源没有恢复：%+v %v", result, err)
	}
	if _, err = os.Stat(opts.DestinationDir + ".previous"); !os.IsNotExist(err) {
		t.Fatal("成功恢复后仍遗留托管备份")
	}
	if _, err = validatedTemporaryCache(opts.DestinationDir); err != nil {
		t.Fatal(err)
	}
}
