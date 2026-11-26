// Package sessionfile 文件作用：验证真实会话文件的权限、更新、目标隔离、损坏与链接拒绝及退出删除。
package sessionfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSessionFileRestartAndReplace 验证规范化地址恢复、完整替换及 Unix 真机权限，不产生额外凭证副本。
func TestSessionFileRestartAndReplace(t *testing.T) {
	root := t.TempDir()
	for _, token := range []string{"synthetic-first", "synthetic-second"} {
		if err := Save(root, "HTTPS://GOODHR.EXAMPLE:443/", "wss://WS.EXAMPLE:443/", token); err != nil {
			t.Fatal(err)
		}
		got, err := Load(root, "https://goodhr.example", "https://ws.example")
		if err != nil || got != token {
			t.Fatal("重启读取或原子替换没有保留本次会话", err)
		}
	}
	directory := filepath.Join(root, "cloud-session")
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "session.json" {
		t.Fatal("会话目录中留下了多余凭证文件", err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{directory: 0700, filepath.Join(directory, "session.json"): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("私有会话文件权限不正确", err)
			}
		}
	}
	if Save(root, "https://goodhr.example", "https://ws.example", strings.Repeat("x", maxSessionBytes)) == nil {
		t.Fatal("过大的会话记录不应覆盖旧会话")
	}
	if token, _ := Load(root, "https://goodhr.example", "https://ws.example"); token != "synthetic-second" {
		t.Fatal("失败的更新破坏了已保存会话")
	}
}

// TestSessionFileEndpointIsolation 验证开发/正式环境、API 前缀、协议、端口及 WS 地址变化均不能借用旧凭证。
func TestSessionFileEndpointIsolation(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, "https://api.example/prefix", "https://ws.example", "synthetic-private"); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range [][2]string{
		{"http://api.example/prefix", "https://ws.example"},
		{"https://api.example:444/prefix", "https://ws.example"},
		{"https://other.example/prefix", "https://ws.example"},
		{"https://api.example/other", "https://ws.example"},
		{"https://api.example/prefix", "https://other.example"},
	} {
		if token, err := Load(root, endpoint[0], endpoint[1]); err != nil || token != "" {
			t.Fatal("目标变化后仍恢复了其他环境的会话", err)
		}
	}
	for _, invalid := range []string{"file:///tmp/test", "https://user:synthetic-private@api.example", "https://api.example?token=synthetic-private", ""} {
		if err := Save(root, invalid, "https://ws.example", "synthetic-private"); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("无效地址未拒绝，或错误暴露凭证")
		}
	}
}

// TestSessionFileRejectsUnsafeFiles 验证损坏、超限和符号链接不会被读取或覆盖，错误不回显文件内容。
func TestSessionFileRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"corrupt", "oversize", "symlink-file", "symlink-directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := Save(root, "https://api.example", "", "synthetic-private"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "cloud-session", "session.json")
			switch kind {
			case "corrupt":
				if err := os.WriteFile(path, []byte("synthetic-private"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.WriteFile(path, []byte(strings.Repeat("x", maxSessionBytes+1)), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if kind == "symlink-directory" {
					path = filepath.Dir(path)
				}
				target := path + "-original"
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skip("当前系统不允许创建测试链接")
				}
				if Save(root, "https://api.example", "", "replacement") == nil {
					t.Fatal("保存不应跟随符号链接")
				}
			}
			if token, err := Load(root, "https://api.example", ""); token != "" || err == nil || strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("不安全记录未拒绝或错误泄露凭证")
			}
		})
	}
}

// TestSessionFileDelete 验证不存在、正常退出和重复退出都不会恢复旧会话。
func TestSessionFileDelete(t *testing.T) {
	root := t.TempDir()
	if err := Delete(root); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, "https://api.example", "", "synthetic-private"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := Delete(root); err != nil {
			t.Fatal(err)
		}
	}
	if token, err := Load(root, "https://api.example", ""); err != nil || token != "" {
		t.Fatal("退出后仍恢复了旧凭证", err)
	}
}
