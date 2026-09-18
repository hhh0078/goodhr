// Package runtime 文件作用：验证书签栏资源修正保留其他资源与策略，重复启动不重复改写。
package runtime

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureCamoufoxBookmarkWidget 验证布局修改、其他压缩资源完整、原始备份及重复运行。
func TestEnsureCamoufoxBookmarkWidget(t *testing.T) {
	dir := t.TempDir()
	browserDir := filepath.Join(dir, "browser")
	if err := os.MkdirAll(browserDir, 0o700); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(browserDir, "omni.ja")
	var original bytes.Buffer
	writer := zip.NewWriter(&original)
	resources := map[string][]byte{
		browserLayoutResource: []byte(`<toolbaritem id="personal-bookmarks" removable="true"><child/></toolbaritem>`),
		"unchanged.bin":       bytes.Repeat([]byte{0, 1, 2, 255}, 500),
	}
	for name, content := range resources {
		stream, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, original.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCamoufoxBookmarkWidget(dir); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(updated), int64(len(updated)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		expected := resources[file.Name]
		if file.Name == browserLayoutResource {
			expected = bytes.Replace(expected, []byte(`removable="true"`), []byte(`removable="false"`), 1)
		}
		if !bytes.Equal(content, expected) {
			t.Fatalf("资源被意外改变：%s", file.Name)
		}
	}
	backup, err := os.ReadFile(archivePath + ".goodhr-original")
	if err != nil || !bytes.Equal(backup, original.Bytes()) {
		t.Fatal("没有保留原始资源备份", err)
	}
	if err := EnsureCamoufoxBookmarkWidget(dir); err != nil {
		t.Fatal(err)
	}
	repeated, err := os.ReadFile(archivePath)
	if err != nil || !bytes.Equal(repeated, updated) {
		t.Fatal("重复运行不应改写已修正的资源", err)
	}
}

// TestEnsureCamoufoxBookmarkToolbar 验证修正隐藏策略时保留未知策略与其他顶层配置。
func TestEnsureCamoufoxBookmarkToolbar(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "distribution"), 0o700); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(dir, "distribution", "policies.json")
	original := []byte(`{"metadata":{"keep":true},"policies":{"DisplayBookmarksToolbar":"never","DisableAppUpdate":true,"Preferences":{"custom.setting":{"Value":123,"Status":"locked"}}}}`)
	if err := os.WriteFile(policyPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCamoufoxBookmarkToolbar(dir); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Metadata struct{ Keep bool }
		Policies struct {
			DisplayBookmarksToolbar string
			DisableAppUpdate        bool
			Preferences             map[string]struct {
				Value  int
				Status string
			}
		}
	}
	if err := json.Unmarshal(updated, &document); err != nil {
		t.Fatal(err)
	}
	if !document.Metadata.Keep || !document.Policies.DisableAppUpdate || document.Policies.DisplayBookmarksToolbar != "always" || document.Policies.Preferences["custom.setting"].Value != 123 || document.Policies.Preferences["custom.setting"].Status != "locked" {
		t.Fatal("书签栏策略没有修正或其他策略丢失")
	}
	if err := EnsureCamoufoxBookmarkToolbar(dir); err != nil {
		t.Fatal(err)
	}
	repeated, err := os.ReadFile(policyPath)
	if err != nil || !bytes.Equal(repeated, updated) {
		t.Fatal("重复运行不应改写已修正的策略", err)
	}
}

// TestBookmarkRuntimeWithoutResources 验证缺少资源时可启动，空路径不意外修改当前目录。
func TestBookmarkRuntimeWithoutResources(t *testing.T) {
	for _, ensure := range []func(string) error{EnsureCamoufoxBookmarkWidget, EnsureCamoufoxBookmarkToolbar} {
		if err := ensure(t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if err := ensure(""); err == nil {
			t.Fatal("空目录应返回错误")
		}
	}
}
