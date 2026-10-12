// Package browserextension 文件作用：验证 Chrome 插件下载、校验、安全解压和原子切换边界。
package browserextension

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

// TestResolveDownloadURL 验证同站相对地址和 HTTPS 地址可用，普通 HTTP 绝对地址被拒绝。
func TestResolveDownloadURL(t *testing.T) {
	base, err := url.Parse("https://goodhr.example/app")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := resolveDownloadURL(base, "/downloads/plugin.zip")
	if err != nil || relative != "https://goodhr.example/downloads/plugin.zip" {
		t.Fatalf("相对地址解析错误：url=%q err=%v", relative, err)
	}
	absolute, err := resolveDownloadURL(base, "https://oss.example/plugin.zip")
	if err != nil || absolute != "https://oss.example/plugin.zip" {
		t.Fatalf("HTTPS 地址解析错误：url=%q err=%v", absolute, err)
	}
	for _, value := range []string{"http://oss.example/plugin.zip", "//oss.example/plugin.zip", "/\\evil.zip"} {
		if _, err = resolveDownloadURL(base, value); err == nil {
			t.Fatalf("不安全地址未被拒绝：%s", value)
		}
	}
}

// TestPrepareDownloadsAndIsIdempotent 验证真实下载、摘要校验、解压以及同版本幂等跳过下载。
func TestPrepareDownloadsAndIsIdempotent(t *testing.T) {
	archive := buildZip(t, []zipEntry{
		{name: "manifest.json", body: chromeManifest(expectedExtensionName, "1.2.3")},
		{name: "src/background.js", body: "console.log('goodhr')"},
	})
	digest := sha256.Sum256(archive)
	var downloadCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{Version: "1.2.3", URLChrome: "/plugin.zip", SHA256Chrome: hex.EncodeToString(digest[:])}})
		case "/plugin.zip":
			downloadCount.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "chrome")
	result, err := Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.2.3" || result.Directory != destination || result.AlreadyCurrent || downloadCount.Load() != 1 {
		t.Fatalf("首次准备结果不正确：%+v downloads=%d", result, downloadCount.Load())
	}
	if _, err = os.Stat(filepath.Join(destination, "src", "background.js")); err != nil {
		t.Fatal(err)
	}
	result, err = Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination})
	if err != nil || !result.AlreadyCurrent || downloadCount.Load() != 1 {
		t.Fatalf("同版本没有幂等跳过：%+v downloads=%d err=%v", result, downloadCount.Load(), err)
	}
}

// TestPrepareFirefoxDownloadsSignedXPIAndIsIdempotent 验证签名 Firefox XPI 会解压到稳定目录且重复启动不重复下载。
func TestPrepareFirefoxDownloadsSignedXPIAndIsIdempotent(t *testing.T) {
	archive := buildFirefoxXPI(t, "1.2.3", expectedFirefoxID, "console.log('goodhr')", true)
	digest := sha256.Sum256(archive)
	var downloadCount atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{
				Version: "1.2.3", URLFirefox: server.URL + "/plugin.xpi",
				SHA256Firefox: hex.EncodeToString(digest[:]), FirefoxSigned: true,
			}})
		case "/plugin.xpi":
			downloadCount.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), managedFirefoxDirName)
	options := Options{CloudBaseURL: server.URL, DestinationDir: destination, HTTPClient: server.Client()}
	result, err := PrepareFirefox(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.2.3" || result.Directory != destination || result.AlreadyCurrent || downloadCount.Load() != 1 {
		t.Fatalf("首次准备 Firefox 插件结果不正确：%+v downloads=%d", result, downloadCount.Load())
	}
	if _, err = os.Stat(filepath.Join(destination, "META-INF", "mozilla.rsa")); err != nil {
		t.Fatalf("签名信封没有保留：%v", err)
	}
	if _, err = os.Stat(filepath.Join(destination, "META-INF", "cose.sig")); err != nil {
		t.Fatalf("COSE 签名信封没有保留：%v", err)
	}
	result, err = PrepareFirefox(context.Background(), options)
	if err != nil || !result.AlreadyCurrent || downloadCount.Load() != 1 {
		t.Fatalf("Firefox 同版本没有幂等跳过：%+v downloads=%d err=%v", result, downloadCount.Load(), err)
	}
}

// TestPrepareFirefoxRejectsUnsignedOrWrongIdentity 验证未签名配置、错误扩展编号和缺失签名信封都不会进入稳定目录。
func TestPrepareFirefoxRejectsUnsignedOrWrongIdentity(t *testing.T) {
	for _, item := range []struct {
		name      string
		signed    bool
		firefoxID string
		signature bool
		wantError string
	}{
		{name: "unsigned-config", signed: false, firefoxID: expectedFirefoxID, signature: true, wantError: "尚未完成 Mozilla 签名"},
		{name: "wrong-id", signed: true, firefoxID: "other@goodhr.test", signature: true, wantError: "Firefox 扩展编号不正确"},
		{name: "missing-signature-envelope", signed: true, firefoxID: expectedFirefoxID, signature: false, wantError: "Mozilla 签名信封"},
	} {
		t.Run(item.name, func(t *testing.T) {
			archive := buildFirefoxXPI(t, "1.2.3", item.firefoxID, "background", item.signature)
			digest := sha256.Sum256(archive)
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case releasePath:
					_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{
						Version: "1.2.3", URLFirefox: server.URL + "/plugin.xpi",
						SHA256Firefox: hex.EncodeToString(digest[:]), FirefoxSigned: item.signed,
					}})
				case "/plugin.xpi":
					_, _ = w.Write(archive)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			destination := filepath.Join(t.TempDir(), managedFirefoxDirName)
			_, err := PrepareFirefox(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination, HTTPClient: server.Client()})
			if err == nil || !strings.Contains(err.Error(), item.wantError) {
				t.Fatalf("Firefox 无效发布没有被拒绝：%v", err)
			}
			if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("无效 Firefox 发布不应生成稳定目录：%v", statErr)
			}
		})
	}
}

// TestPrepareFirefoxFailurePreservesPrevious 验证新版 XPI 校验失败时旧版扩展目录保持完整。
func TestPrepareFirefoxFailurePreservesPrevious(t *testing.T) {
	destination := filepath.Join(t.TempDir(), managedFirefoxDirName)
	firstArchive := buildFirefoxXPI(t, "1.2.3", expectedFirefoxID, "old-background", true)
	firstDigest := sha256.Sum256(firstArchive)
	firstServer := newFirefoxReleaseServer(t, firstArchive, "1.2.3", hex.EncodeToString(firstDigest[:]), true)
	if _, err := PrepareFirefox(context.Background(), Options{CloudBaseURL: firstServer.URL, DestinationDir: destination, HTTPClient: firstServer.Client()}); err != nil {
		firstServer.Close()
		t.Fatal(err)
	}
	firstServer.Close()

	brokenArchive := buildFirefoxXPI(t, "1.2.4", expectedFirefoxID, "new-background", false)
	brokenDigest := sha256.Sum256(brokenArchive)
	brokenServer := newFirefoxReleaseServer(t, brokenArchive, "1.2.4", hex.EncodeToString(brokenDigest[:]), true)
	defer brokenServer.Close()
	if _, err := PrepareFirefox(context.Background(), Options{CloudBaseURL: brokenServer.URL, DestinationDir: destination, HTTPClient: brokenServer.Client()}); err == nil {
		t.Fatal("缺少签名信封的新版 Firefox 插件没有被拒绝")
	}
	content, err := os.ReadFile(filepath.Join(destination, "src", "background.js"))
	if err != nil || string(content) != "old-background" {
		t.Fatalf("失败后旧版 Firefox 插件没有保留：content=%q err=%v", content, err)
	}
}

// TestManagedFirefoxDestination 验证托管插件位于扩展根目录的固定一级子目录。
func TestManagedFirefoxDestination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "extensions")
	destination, err := ManagedFirefoxDestination(root)
	if err != nil || destination != filepath.Join(root, managedFirefoxDirName) {
		t.Fatalf("Firefox 托管目录不正确：%q err=%v", destination, err)
	}
	if _, err = ManagedFirefoxDestination("  "); err == nil {
		t.Fatal("空扩展根目录没有被拒绝")
	}
}

// TestPrepareSameVersionChangedDigestDownloadsAgain 验证同版本换包时摘要收据会触发重新下载和替换。
func TestPrepareSameVersionChangedDigestDownloadsAgain(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "chrome")
	firstArchive := buildZip(t, []zipEntry{
		{name: "manifest.json", body: chromeManifest(expectedExtensionName, "1.2.3")},
		{name: "src/background.js", body: "first"},
	})
	firstDigest := sha256.Sum256(firstArchive)
	firstServer := newReleaseServer(t, firstArchive, "1.2.3", hex.EncodeToString(firstDigest[:]))
	if _, err := Prepare(context.Background(), Options{CloudBaseURL: firstServer.URL, DestinationDir: destination}); err != nil {
		firstServer.Close()
		t.Fatal(err)
	}
	firstServer.Close()

	secondArchive := buildZip(t, []zipEntry{
		{name: "manifest.json", body: chromeManifest(expectedExtensionName, "1.2.3")},
		{name: "src/background.js", body: "second"},
	})
	secondDigest := sha256.Sum256(secondArchive)
	secondServer := newReleaseServer(t, secondArchive, "1.2.3", hex.EncodeToString(secondDigest[:]))
	defer secondServer.Close()
	result, err := Prepare(context.Background(), Options{CloudBaseURL: secondServer.URL, DestinationDir: destination})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "src", "background.js"))
	if err != nil || result.AlreadyCurrent || string(content) != "second" {
		t.Fatalf("同版本新摘要没有替换：result=%+v content=%q err=%v", result, content, err)
	}
}

// TestPrepareRepairsChangedTree 验证已安装目录被删除、修改或增加文件后会重新下载并修复。
func TestPrepareRepairsChangedTree(t *testing.T) {
	for _, mutation := range []struct {
		name  string
		apply func(string) error
	}{
		{name: "delete", apply: func(root string) error { return os.Remove(filepath.Join(root, "src", "background.js")) }},
		{name: "modify", apply: func(root string) error {
			return os.WriteFile(filepath.Join(root, "src", "background.js"), []byte("changed"), 0o644)
		}},
		{name: "add", apply: func(root string) error { return os.WriteFile(filepath.Join(root, "extra.js"), []byte("extra"), 0o644) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			archive := buildZip(t, []zipEntry{
				{name: "manifest.json", body: chromeManifest(expectedExtensionName, "1.2.3")},
				{name: "src/background.js", body: "original"},
			})
			digest := sha256.Sum256(archive)
			var downloadCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case releasePath:
					_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{Version: "1.2.3", URLChrome: "/plugin.zip", SHA256Chrome: hex.EncodeToString(digest[:])}})
				case "/plugin.zip":
					downloadCount.Add(1)
					_, _ = w.Write(archive)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			destination := filepath.Join(t.TempDir(), "chrome")
			if _, err := Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination}); err != nil {
				t.Fatal(err)
			}
			if err := mutation.apply(destination); err != nil {
				t.Fatal(err)
			}
			result, err := Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination})
			if err != nil || result.AlreadyCurrent || downloadCount.Load() != 2 {
				t.Fatalf("损坏目录没有重新下载：result=%+v downloads=%d err=%v", result, downloadCount.Load(), err)
			}
			content, readErr := os.ReadFile(filepath.Join(destination, "src", "background.js"))
			if readErr != nil || string(content) != "original" {
				t.Fatalf("后台文件没有恢复：content=%q err=%v", content, readErr)
			}
			if _, statErr := os.Stat(filepath.Join(destination, "extra.js")); mutation.name == "add" && !os.IsNotExist(statErr) {
				t.Fatalf("多余文件没有清理：%v", statErr)
			}
		})
	}
}

// TestPrepareRejectsBadDigest 验证错误 SHA256 不会生成稳定插件目录。
func TestPrepareRejectsBadDigest(t *testing.T) {
	archive := buildZip(t, []zipEntry{{name: "manifest.json", body: chromeManifest(expectedExtensionName, "1.2.3")}})
	server := newReleaseServer(t, archive, "1.2.3", strings.Repeat("0", 64))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "chrome")
	if _, err := Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: destination}); err == nil || !strings.Contains(err.Error(), "SHA256 不一致") {
		t.Fatalf("错误摘要没有被拒绝：%v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("摘要失败后不应生成稳定目录：%v", err)
	}
}

// TestExtractArchiveRejectsUnsafeEntries 验证 Zip Slip、符号链接和非常规文件都会被拒绝。
func TestExtractArchiveRejectsUnsafeEntries(t *testing.T) {
	for _, item := range []struct {
		name  string
		entry zipEntry
	}{
		{name: "zip-slip", entry: zipEntry{name: "../outside.txt", body: "bad"}},
		{name: "backslash-slip", entry: zipEntry{name: `..\outside.txt`, body: "bad"}},
		{name: "symlink", entry: zipEntry{name: "link", body: "target", mode: os.ModeSymlink | 0o777}},
		{name: "named-pipe", entry: zipEntry{name: "pipe", body: "bad", mode: os.ModeNamedPipe | 0o600}},
		{name: "alternate-stream", entry: zipEntry{name: "dir/file.txt:stream", body: "bad"}},
		{name: "device-name", entry: zipEntry{name: "AUX.txt", body: "bad"}},
		{name: "trailing-dot", entry: zipEntry{name: "dir./file.txt", body: "bad"}},
		{name: "trailing-space", entry: zipEntry{name: "dir /file.txt", body: "bad"}},
	} {
		t.Run(item.name, func(t *testing.T) {
			archive := buildZip(t, []zipEntry{item.entry})
			archivePath := filepath.Join(t.TempDir(), "plugin.zip")
			if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := extractArchive(archivePath, filepath.Join(t.TempDir(), "target"), defaultMaxExtractedBytes); err == nil {
				t.Fatal("不安全条目没有被拒绝")
			}
		})
	}
}

// TestPrepareValidatesManifestIdentityAndVersion 验证压缩包身份和版本必须与云端配置一致。
func TestPrepareValidatesManifestIdentityAndVersion(t *testing.T) {
	for _, item := range []struct {
		name, manifest, want string
	}{
		{name: "identity", manifest: chromeManifest("Unknown Assistant", "1.2.3"), want: "身份校验失败"},
		{name: "version", manifest: chromeManifest(expectedExtensionName, "9.9.9"), want: "版本校验失败"},
		{name: "manifest-version", manifest: `{"manifest_version":2,"name":"GoodHR 简历助手","version":"1.2.3","background":{"service_worker":"src/background.js"}}`, want: "格式校验失败"},
		{name: "worker", manifest: `{"manifest_version":3,"name":"GoodHR 简历助手","version":"1.2.3","background":{}}`, want: "格式校验失败"},
	} {
		t.Run(item.name, func(t *testing.T) {
			archive := buildZip(t, []zipEntry{{name: "manifest.json", body: item.manifest}, {name: "src/background.js", body: "worker"}})
			digest := sha256.Sum256(archive)
			server := newReleaseServer(t, archive, "1.2.3", hex.EncodeToString(digest[:]))
			defer server.Close()
			if _, err := Prepare(context.Background(), Options{CloudBaseURL: server.URL, DestinationDir: filepath.Join(t.TempDir(), "chrome")}); err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("manifest 校验结果不正确：%v", err)
			}
		})
	}
}

// TestFetchReleaseRejectsUnsafeRedirects 验证配置接口拒绝 HTTPS 降级和开发环境跨站重定向。
func TestFetchReleaseRejectsUnsafeRedirects(t *testing.T) {
	plainTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) }))
	defer plainTarget.Close()
	tlsSource := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plainTarget.URL, http.StatusFound) }))
	defer tlsSource.Close()
	if _, _, err := fetchRelease(context.Background(), tlsSource.Client(), tlsSource.URL); err == nil || !strings.Contains(err.Error(), "HTTPS 降级") {
		t.Fatalf("配置接口 HTTPS 降级没有被拒绝：%v", err)
	}

	crossHostSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plainTarget.URL, http.StatusFound) }))
	defer crossHostSource.Close()
	if _, _, err := fetchRelease(context.Background(), crossHostSource.Client(), crossHostSource.URL); err == nil || !strings.Contains(err.Error(), "站外") {
		t.Fatalf("开发配置接口跨站重定向没有被拒绝：%v", err)
	}
}

// TestFetchReleaseLimitsRedirectCount 验证自循环重定向会按默认上限停止，不会一直等到总超时。
func TestFetchReleaseLimitsRedirectCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer server.Close()
	if _, _, err := fetchRelease(context.Background(), server.Client(), server.URL); err == nil || !strings.Contains(err.Error(), "重定向次数过多") {
		t.Fatalf("循环重定向没有按上限停止：%v", err)
	}
}

// TestDownloadArchiveRejectsHTTPSDowngrade 验证插件包下载的每一跳都不能从 HTTPS 降级。
func TestDownloadArchiveRejectsHTTPSDowngrade(t *testing.T) {
	plainTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("zip")) }))
	defer plainTarget.Close()
	tlsSource := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, plainTarget.URL, http.StatusFound) }))
	defer tlsSource.Close()
	_, err := downloadArchive(context.Background(), tlsSource.Client(), tlsSource.URL, strings.Repeat("0", 64), t.TempDir(), 1024)
	if err == nil || !strings.Contains(err.Error(), "HTTPS 降级") {
		t.Fatalf("插件包 HTTPS 降级没有被拒绝：%v", err)
	}
}

// chromeManifest 构造带固定 Manifest V3 后台入口的测试清单。
func chromeManifest(name string, version string) string {
	return fmt.Sprintf(`{"manifest_version":3,"name":%q,"version":%q,"background":{"service_worker":"src/background.js"}}`, name, version)
}

// firefoxManifest 构造带固定 Firefox 扩展编号和后台脚本的测试清单。
func firefoxManifest(name string, version string, firefoxID string) string {
	return fmt.Sprintf(`{"manifest_version":3,"name":%q,"version":%q,"background":{"scripts":["src/background.js"]},"browser_specific_settings":{"gecko":{"id":%q}}}`, name, version, firefoxID)
}

// buildFirefoxXPI 构造可测试的 Firefox XPI；签名信封只用于验证文件边界，不模拟 Mozilla 加密签名。
func buildFirefoxXPI(t *testing.T, version string, firefoxID string, background string, includeSignatureEnvelope bool) []byte {
	t.Helper()
	entries := []zipEntry{
		{name: "manifest.json", body: firefoxManifest(expectedExtensionName, version, firefoxID)},
		{name: "src/background.js", body: background},
	}
	if includeSignatureEnvelope {
		entries = append(entries,
			zipEntry{name: "META-INF/manifest.mf", body: "manifest"},
			zipEntry{name: "META-INF/mozilla.sf", body: "signature-file"},
			zipEntry{name: "META-INF/mozilla.rsa", body: "signature-block"},
			zipEntry{name: "META-INF/cose.manifest", body: "cose-manifest"},
			zipEntry{name: "META-INF/cose.sig", body: "cose-signature"},
		)
	}
	return buildZip(t, entries)
}

// TestReplaceDirectoryRollsBack 验证新目录切换失败时旧插件会恢复到稳定目录。
func TestReplaceDirectoryRollsBack(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "chrome")
	staging := filepath.Join(root, "stage")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	var calls int
	rename := func(oldPath string, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("synthetic switch failure")
		}
		return os.Rename(oldPath, newPath)
	}
	if err := replaceDirectory(target, staging, rename); err == nil {
		t.Fatal("模拟切换失败没有返回错误")
	}
	content, err := os.ReadFile(filepath.Join(target, "old.txt"))
	if err != nil || string(content) != "old" || calls != 3 {
		t.Fatalf("旧目录没有恢复：content=%q calls=%d err=%v", content, calls, err)
	}
}

// newReleaseServer 创建同时提供公开配置和插件包的测试服务。
func newReleaseServer(t *testing.T, archive []byte, version string, digest string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{Version: version, URLChrome: "/plugin.zip", SHA256Chrome: digest}})
		case "/plugin.zip":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

// newFirefoxReleaseServer 创建 HTTPS 公开配置和签名 XPI 测试服务。
func newFirefoxReleaseServer(t *testing.T, archive []byte, version string, digest string, signed bool) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case releasePath:
			_ = json.NewEncoder(w).Encode(releaseResponse{OK: true, Release: release{
				Version: version, URLFirefox: server.URL + "/plugin.xpi",
				SHA256Firefox: digest, FirefoxSigned: signed,
			}})
		case "/plugin.xpi":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

// buildZip 构造包含指定条目和文件类型的测试压缩包。
func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		} else {
			header.SetMode(0o644)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
