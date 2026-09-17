// Package runtime 文件作用：测试内置中文语言包的安装、版本校验和重复安装行为。
package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVersionFile 在临时 Camoufox 目录写入 version.json。
func writeVersionFile(t *testing.T, dir string, version string) {
	t.Helper()
	content := `{"version":"` + version + `","release":"beta.30"}`
	if err := os.WriteFile(filepath.Join(dir, "version.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEnsureChineseLanguagePackInstallsOnce 验证语言包会写入应用级扩展目录且重复调用不报错。
func TestEnsureChineseLanguagePackInstallsOnce(t *testing.T) {
	dir := t.TempDir()
	writeVersionFile(t, dir, "152.0.4")

	if err := EnsureChineseLanguagePack(dir); err != nil {
		t.Fatalf("首次安装语言包失败：%v", err)
	}
	target := filepath.Join(dir, "browser", "extensions", langpackExtensionID+".xpi")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("语言包没有写入预期位置：%v", err)
	}
	if info.Size() != int64(len(langpackBinary)) || len(langpackBinary) == 0 {
		t.Fatalf("语言包大小 = %d, 期望 %d", info.Size(), len(langpackBinary))
	}
	if err := EnsureChineseLanguagePack(dir); err != nil {
		t.Fatalf("重复安装语言包不应报错：%v", err)
	}
}

// TestEnsureChineseLanguagePackSkipsMismatchedVersion 验证大版本不匹配时跳过安装。
func TestEnsureChineseLanguagePackSkipsMismatchedVersion(t *testing.T) {
	dir := t.TempDir()
	writeVersionFile(t, dir, "153.0.1")

	err := EnsureChineseLanguagePack(dir)
	if err == nil {
		t.Fatalf("大版本不匹配时应返回错误")
	}
	if !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("错误说明应包含不匹配提示：%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "browser", "extensions", langpackExtensionID+".xpi")); statErr == nil {
		t.Fatalf("版本不匹配时不应写入语言包")
	}
}

// TestEnsureChineseLanguagePackRejectsMissingVersion 验证缺少 version.json 时不写入语言包。
func TestEnsureChineseLanguagePackRejectsMissingVersion(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureChineseLanguagePack(dir); err == nil {
		t.Fatalf("缺少 version.json 时应返回错误")
	}
	if err := EnsureChineseLanguagePack("  "); err == nil {
		t.Fatalf("空目录参数应返回错误")
	}
}
