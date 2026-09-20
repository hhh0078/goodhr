// Package runtime 文件作用：验证内置 UBO 来源、离线修复、备份保留和跨平台目录边界。
package runtime

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"
)

// TestBundledUBOAsset 校验内置资源与 Mozilla 公布的固定版本 SHA256 和扩展身份一致。
func TestBundledUBOAsset(t *testing.T) {
	digest := sha256.Sum256(bundledUBO)
	if hex.EncodeToString(digest[:]) != "5b74415860456370644bd80f16125e865b0e6c356bb5dfcfb84069967eaa5287" {
		t.Fatal("内置扩展与 Mozilla 固定版本校验值不一致")
	}
	reader, err := zip.NewReader(bytes.NewReader(bundledUBO), int64(len(bundledUBO)))
	if err != nil {
		t.Fatal(err)
	}
	file, err := reader.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var manifest struct {
		Version  string `json:"version"`
		Settings struct {
			Gecko struct {
				ID string `json:"id"`
			} `json:"gecko"`
		} `json:"browser_specific_settings"`
	}
	if err = json.NewDecoder(file).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "1.75.0" || manifest.Settings.Gecko.ID != "uBlock0@raymondhill.net" {
		t.Fatalf("扩展身份不正确：%+v", manifest)
	}
}

// TestEnsureBundledUBOOfflineRepair 验证空目录故障、同大小内容损坏、缺失文件和重复启动。
func TestEnsureBundledUBOOfflineRepair(t *testing.T) {
	root := t.TempDir()
	target := bundledUBOPath(root, goruntime.GOOS)
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "user-backup.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(target), "other-extension.txt")
	if err := os.WriteFile(sibling, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBundledUBO(root); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(target), "UBO-backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("旧目录没有唯一备份：%v %v", backups, err)
	}
	if data, err := os.ReadFile(filepath.Join(backups[0], "user-backup.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("原文件没有保留：%v", err)
	}
	manifest := filepath.Join(target, "manifest.json")
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(manifest, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBundledUBO(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifest)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatal("完整扩展不应重复写入")
	}
	// 保持文件大小不变，确认不是只检查 manifest 存在或文件大小。
	script := filepath.Join(target, "js", "vapi.js")
	original, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	broken := append([]byte(nil), original...)
	broken[0] ^= 1
	if err = os.WriteFile(script, broken, 0o644); err != nil {
		t.Fatal(err)
	}
	if err = EnsureBundledUBO(root); err != nil {
		t.Fatal(err)
	}
	if repaired, err := os.ReadFile(script); err != nil || !bytes.Equal(repaired, original) {
		t.Fatal("同大小损坏文件没有恢复")
	}
	if err = os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if err = EnsureBundledUBO(root); err != nil {
		t.Fatal(err)
	}
	if repaired, err := os.ReadFile(script); err != nil || !bytes.Equal(repaired, original) {
		t.Fatal("缺失脚本没有恢复")
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "unchanged" {
		t.Fatal("其他扩展被修改")
	}
	stages, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".UBO-install-*"))
	if len(stages) != 0 {
		t.Fatalf("遗留临时解压目录：%v", stages)
	}
}

// TestEnsureBundledUBOFreshInstall 验证全新目录无需网络即可安装，并且所有 ZIP 条目校验通过。
func TestEnsureBundledUBOFreshInstall(t *testing.T) {
	root := t.TempDir()
	if err := EnsureBundledUBO(root); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(bundledUBO), int64(len(bundledUBO)))
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyBundledUBO(bundledUBOPath(root, goruntime.GOOS), reader); err != nil {
		t.Fatal(err)
	}
	license, err := reader.Open("LICENSE.txt")
	if err != nil {
		t.Fatal("内置扩展必须保留许可证")
	}
	defer license.Close()
	if data, err := io.ReadAll(license); err != nil || len(data) == 0 {
		t.Fatal("许可证为空")
	}
}

// TestBundledUBOPaths 验证 Windows/Linux/macOS 与依赖的扩展资源路径一致。
func TestBundledUBOPaths(t *testing.T) {
	root := t.TempDir()
	for _, platform := range []string{"windows", "linux", "darwin"} {
		want := filepath.Join(root, "addons", "UBO")
		if platform == "darwin" {
			want = filepath.Join(root, "Camoufox.app", "Contents", "Resources", "addons", "UBO")
		}
		if got := bundledUBOPath(root, platform); got != want {
			t.Fatalf("%s: %s != %s", platform, got, want)
		}
	}
	if err := EnsureBundledUBO(""); err == nil {
		t.Fatal("空目录应报错")
	}
	parent := filepath.Dir(bundledUBOPath(root, goruntime.GOOS))
	if err := os.MkdirAll(filepath.Dir(parent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureBundledUBO(root); err == nil {
		t.Fatal("父目录是文件时应报错")
	}
	if data, err := os.ReadFile(parent); err != nil || string(data) != "keep" {
		t.Fatal("路径冲突不应改动原文件")
	}
}

// TestBundledUBORejectsLinkedParent 验证 addons 父目录是链接时不能写入外部目录。
func TestBundledUBORejectsLinkedParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	parent := filepath.Dir(bundledUBOPath(root, goruntime.GOOS))
	if err := os.MkdirAll(filepath.Dir(parent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Skipf("当前环境不允许创建链接：%v", err)
	}
	if err := EnsureBundledUBO(root); err == nil {
		t.Fatal("不应接受链接父目录")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("外部目录被修改")
	}
}
