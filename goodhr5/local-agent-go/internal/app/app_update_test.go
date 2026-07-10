// Package app 测试本地程序自动更新包处理逻辑。
package app

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractAppUpdateZipFindsInstaller 验证 zip 更新包会解压并找到 exe 安装器。
func TestExtractAppUpdateZipFindsInstaller(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "update.zip")
	writeTestZip(t, archivePath, map[string]string{"GoodHR-LocalAgent-Setup.exe": "exe"})
	targetDir := filepath.Join(t.TempDir(), "extract")
	installerPath, err := extractAppUpdateZip(archivePath, targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(installerPath) != "GoodHR-LocalAgent-Setup.exe" {
		t.Fatalf("installerPath = %s", installerPath)
	}
	if _, err := os.Stat(installerPath); err != nil {
		t.Fatal(err)
	}
}

// TestExtractAppUpdateZipRejectsUnsafePath 验证 zip 更新包不会解压越界路径。
func TestExtractAppUpdateZipRejectsUnsafePath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "update.zip")
	writeTestZip(t, archivePath, map[string]string{"../bad.exe": "bad"})
	_, err := extractAppUpdateZip(archivePath, filepath.Join(t.TempDir(), "extract"))
	if err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("err = %v", err)
	}
}

// TestIsNewerAppVersion 验证只有目标版本更高时才需要更新。
func TestIsNewerAppVersion(t *testing.T) {
	cases := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{name: "目标版本更高", current: "5.1.3", target: "5.1.4", want: true},
		{name: "版本相等", current: "5.1.3", target: "5.1.3", want: false},
		{name: "当前版本更高", current: "5.1.4", target: "5.1.3", want: false},
		{name: "多位数字版本", current: "5.1.2", target: "5.1.10", want: true},
		{name: "短版本等于补零版本", current: "5.1", target: "5.1.0", want: false},
		{name: "支持 v 前缀", current: "v5.1.3", target: "5.1.4", want: true},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got := isNewerAppVersion(item.current, item.target)
			if got != item.want {
				t.Fatalf("isNewerAppVersion(%q, %q) = %v, want %v", item.current, item.target, got, item.want)
			}
		})
	}
}

// writeTestZip 写入测试 zip 文件。
// files 为 zip 内文件名和内容。
func writeTestZip(t *testing.T, archivePath string, files map[string]string) {
	t.Helper()
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
