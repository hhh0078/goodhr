// Package runtime 文件作用：显式验证隔离 Camoufox 副本能否使用现有客户端资源适配，失败时阻止把新版当成可发布组件。
package runtime

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCamoufoxUpgradeCompatibilityProbe 仅对显式指定的隔离副本逐项执行真实适配，不启动浏览器或访问网站。
// GOODHR_UPGRADE_PROBE_DIR 必须指向 camoufox-upgrade 目录内的副本，不能指向用户正在使用的运行组件。
func TestCamoufoxUpgradeCompatibilityProbe(t *testing.T) {
	directory := strings.TrimSpace(os.Getenv("GOODHR_UPGRADE_PROBE_DIR"))
	if directory == "" {
		t.Skip("设置 GOODHR_UPGRADE_PROBE_DIR 后运行隔离浏览器兼容探针")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("探针必须使用已存在的隔离目录：%s，err=%v", root, err)
	}
	isolated := false
	for _, component := range strings.Split(filepath.ToSlash(root), "/") {
		if component == "camoufox-upgrade" {
			isolated = true
		}
	}
	if !isolated || strings.Contains(strings.ToLower(filepath.ToSlash(root)), "/library/application support/goodhr/") {
		t.Fatal("探针只能修改 camoufox-upgrade 目录内的隔离副本，禁止使用用户运行组件")
	}
	if err := rejectUpgradeProbeExternalLinks(root); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeInputArchivePath(root); err != nil {
		t.Fatalf("隔离目录没有可检查的浏览器归档：%v", err)
	}
	major, versionErr := camoufoxMajorVersion(root)
	t.Logf("UPGRADE_PROBE_ROOT root=%s firefox_major=%s version_error=%v", root, major, versionErr)
	checks := []struct {
		name string
		run  func(string) error
	}{
		{name: "bundled_ubo", run: EnsureBundledUBO},
		{name: "font_rendering", run: EnsureCamoufoxFontRendering},
		{name: "mouse_safety", run: EnsureCamoufoxMouseSafety},
		{name: "tab_interaction", run: EnsureCamoufoxTabInteraction},
		{name: "bookmark_toolbar", run: EnsureCamoufoxBookmarkToolbar},
		{name: "bookmark_widget", run: EnsureCamoufoxBookmarkWidget},
		{name: "native_downloads", run: EnsureCamoufoxNativeDownloads},
		{name: "chinese_language_pack", run: EnsureChineseLanguagePack},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.run(root)
			message := ""
			if err != nil {
				message = err.Error()
			}
			encoded, _ := json.Marshal(struct {
				Check   string `json:"check"`
				Success bool   `json:"success"`
				Error   string `json:"error,omitempty"`
			}{Check: check.name, Success: err == nil, Error: message})
			t.Logf("UPGRADE_PROBE_RESULT %s", encoded)
			if err != nil {
				t.Errorf("现有客户端适配未通过：%v", err)
			}
		})
	}
	t.Run("inspect_native_input", func(t *testing.T) {
		status := InspectCamoufoxMouseSafety(root)
		encoded, _ := json.Marshal(status)
		t.Logf("UPGRADE_PROBE_INPUT_STATUS %s", encoded)
		if !status.Ready {
			t.Errorf("现有原生输入资源检查未通过：%s", status.Error)
		}
	})
}

// rejectUpgradeProbeExternalLinks 拒绝指向隔离目录外的软链接，避免副本中的资源实际仍指向用户浏览器。
func rejectUpgradeProbeExternalLinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("隔离副本软链接无法解析：%s：%w", path, err)
		}
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("隔离副本软链接指向目录外，已停止：%s", path)
		}
		return nil
	})
}
