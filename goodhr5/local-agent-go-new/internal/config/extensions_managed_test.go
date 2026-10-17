// 文件作用：验证托管简历插件与手动旧版不会重复加载，中断更新留下的备份不污染后续复用。
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/runtime/browserextension"
)

// TestManagedExtensionPathsStayCanonical 验证首次安装和后续扫描使用相同顺序，用户自定义来源不会被删除。
func TestManagedExtensionPathsStayCanonical(t *testing.T) {
	cfg := Config{ExtensionsDir: t.TempDir()}
	managed, err := browserextension.ManagedFirefoxDestination(cfg.ExtensionsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ name, id string }{
		{"custom.previous", "custom@example.test"}, {"manual-old", "resume-assistant@goodhr.58it.cn"}, {filepath.Base(managed) + ".previous", "resume-assistant@goodhr.58it.cn"},
	} {
		path := filepath.Join(cfg.ExtensionsDir, entry.name)
		if err = os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		body := `{"name":"test","version":"1.0.9","manifest_version":3,"browser_specific_settings":{"gecko":{"id":"` + entry.id + `"}}}`
		if err = os.WriteFile(filepath.Join(path, "manifest.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before := cfg.ExtensionPaths()
	bookmark := filepath.Join(t.TempDir(), "bookmark")
	first := browserextension.WithManagedFirefoxPath(append(before, bookmark), managed)
	if err = os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(managed, "manifest.json"), []byte(`{"name":"GoodHR 简历助手","version":"1.0.16","manifest_version":3,"browser_specific_settings":{"gecko":{"id":"resume-assistant@goodhr.58it.cn"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second := append(cfg.ExtensionPaths(), bookmark)
	if !reflect.DeepEqual(first, second) || len(second) != 3 || second[0] != managed || second[1] != filepath.Join(cfg.ExtensionsDir, "custom.previous") {
		t.Fatalf("首次安装和后续复用来源不同：first=%v second=%v", first, second)
	}
	if _, err = os.Stat(filepath.Join(cfg.ExtensionsDir, "manual-old")); err != nil {
		t.Fatal("旧手动目录不应被删除")
	}
}
