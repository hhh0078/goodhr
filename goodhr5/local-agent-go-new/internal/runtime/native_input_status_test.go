// Package runtime 文件作用：验证输入保护按真实浏览器位置检查、缺失与旧版本不会假成功、资源指纹可追踪。
package runtime

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInputStatusArchive 在测试目录内创建最小浏览器资源包，不下载或修改用户浏览器。
func writeInputStatusArchive(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	stream, err := writer.Create(mouseHandlerResource)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestNativeInputStatusUsesActualExecutable 验证嵌套 Windows 可执行文件与 macOS 应用包均检查其实际资源。
func TestNativeInputStatusUsesActualExecutable(t *testing.T) {
	for _, kind := range []string{"windows", "macos"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "nested", "camoufox.exe")
			archive := filepath.Join(root, "nested", "omni.ja")
			if kind == "macos" {
				binary = filepath.Join(root, "Camoufox.app", "Contents", "MacOS", "camoufox")
				archive = filepath.Join(root, "Camoufox.app", "Contents", "Resources", "omni.ja")
			}
			writeInputStatusArchive(t, archive, mouseSafetyMarker+"\n"+string(nativeInputSafety)+string(wheelSafetyEvents))
			if err := os.MkdirAll(filepath.Dir(binary), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte("test executable"), 0o600); err != nil {
				t.Fatal(err)
			}
			status := InspectCamoufoxMouseSafety(binary)
			if !status.Ready || status.InstalledVersion != "v7" || status.ArchivePath != archive || len(status.ResourceSHA256) != 64 || status.Error != "" {
				t.Fatalf("没有检查实际浏览器资源：%+v", status)
			}
		})
	}
}

// TestNativeInputStatusReportsLegacyAndMissing 验证旧资源、缺失和只有版本标记的资源不会被当成保护就绪。
func TestNativeInputStatusReportsLegacyAndMissing(t *testing.T) {
	root := t.TempDir()
	if err := EnsureCamoufoxMouseSafety(root); err == nil {
		t.Fatal("缺失资源被忽略")
	}
	if status := InspectCamoufoxMouseSafety(root); status.Ready || status.Error == "" {
		t.Fatalf("缺失状态不明确：%+v", status)
	}
	archive := filepath.Join(root, "omni.ja")
	for _, content := range []string{"// GoodHR native mouse safety v6\nold resource", mouseSafetyMarker + "\ntruncated resource"} {
		writeInputStatusArchive(t, archive, content)
		status := InspectCamoufoxMouseSafety(root)
		if status.Ready || status.Error == "" || len(status.ResourceSHA256) != 64 {
			t.Fatalf("旧或不完整资源被当成就绪：%+v", status)
		}
		if strings.Contains(content, "v6") && status.InstalledVersion != "v6" {
			t.Fatalf("旧版本没有保留：%+v", status)
		}
	}
}
