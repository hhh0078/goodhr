// 文件作用：验证统一冷启动准备器实际读取临时通道，并保持 Profile 登录文件及自定义、书签扩展。
package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/runtime/browserextension"
)

// TestFirefoxBeforeStartPreservesProfileAndCanonicalPaths 验证首装、同版本检查以及后续扫描保持相同的启动扩展列表。
func TestFirefoxBeforeStartPreservesProfileAndCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{ExtensionsDir: filepath.Join(root, "extensions"), ProfilesDir: filepath.Join(root, "profiles")}
	profileDir := filepath.Join(cfg.ProfilesDir, "synthetic-profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(profileDir, "synthetic-login-sentinel")
	if err := os.WriteFile(sentinel, []byte("keep-profile-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range []struct{ name, body string }{
		{"manifest.json", `{"name":"GoodHR 简历助手","version":"1.0.16","manifest_version":3,"background":{"scripts":["src/background.js"]},"browser_specific_settings":{"gecko":{"id":"resume-assistant@goodhr.58it.cn"}}}`},
		{"src/background.js", "synthetic-background"},
	} {
		file, err := writer.Create(entry.name)
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
	archive := buffer.Bytes()
	digest := sha256.Sum256(archive)
	var downloads, metadata atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zip/goodhr-resume-firefox-latest.json":
			metadata.Add(1)
			_ = json.NewEncoder(w).Encode(struct {
				Version     string `json:"version"`
				URL         string `json:"url"`
				SHA256      string `json:"sha256"`
				ExtensionID string `json:"extension_id"`
				Channel     string `json:"channel"`
			}{"1.0.16", "/zip/synthetic.zip", hex.EncodeToString(digest[:]), "resume-assistant@goodhr.58it.cn", "temporary"})
		case "/zip/synthetic.zip":
			downloads.Add(1)
			_, _ = w.Write(archive)
		default:
			t.Errorf("不应请求正式签名/兼容通道：%s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg.CloudURL = server.URL
	custom, bookmark := filepath.Join(root, "custom"), filepath.Join(profileDir, "bookmark")
	request := contract.BrowserStartRequest{UserDataDir: profileDir, ExtensionPaths: []string{custom, bookmark}}
	prepared, err := firefoxBeforeStart(cfg)(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	managed, _ := browserextension.ManagedFirefoxDestination(cfg.ExtensionsDir)
	if !reflect.DeepEqual(prepared.ExtensionPaths, []string{managed, custom, bookmark}) {
		t.Fatalf("首装扩展丢失或顺序错误：%v", prepared.ExtensionPaths)
	}
	second, err := firefoxBeforeStart(cfg)(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.ExtensionPaths, prepared.ExtensionPaths) || downloads.Load() != 1 || metadata.Load() != 2 {
		t.Fatalf("每次冷启动检查或幂等失效：%v downloads=%d metadata=%d", second.ExtensionPaths, downloads.Load(), metadata.Load())
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "keep-profile-data" {
		t.Fatal("冷启动同步改动了Profile数据")
	}
}
