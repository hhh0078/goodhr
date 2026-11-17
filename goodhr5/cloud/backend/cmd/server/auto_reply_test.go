// 本文件验证同进程自动回复自动配置、外部服务兼容及私有存储和监听地址边界。
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedReplyEnvironment 验证无需额外配置即可准备私有目录和内部凭证，不覆盖已提供的凭证。
func TestEmbeddedReplyEnvironment(t *testing.T) {
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", "")
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "")
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", "")
	logPath := filepath.Join(t.TempDir(), "backend.log")
	on, err := prepareAutoReplyEnvironment(logPath)
	if err != nil || !on {
		t.Fatalf("同进程配置未准备好：%v", err)
	}
	secret, files := os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"), os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR")
	if len(secret) != 64 || !strings.HasPrefix(files, filepath.Dir(logPath)+string(os.PathSeparator)) {
		t.Fatal("凭证或私有存储配置不正确")
	}
	if info, err := os.Stat(files); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("私有附件目录权限不正确")
	}
	if _, err := prepareAutoReplyEnvironment(logPath); err != nil || os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN") != secret {
		t.Fatal("已有内部凭证被替换")
	}
	runtime, err := embeddedAutoReply(":8086")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Close()
}

// TestEmbeddedReplyKeepsExternalDeployment 验证已显式配置独立执行服务时不会偷偷启用第二套流程。
func TestEmbeddedReplyKeepsExternalDeployment(t *testing.T) {
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", "http://127.0.0.1:18646")
	t.Setenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN", "existing-private-service")
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", "")
	on, err := prepareAutoReplyEnvironment(filepath.Join(t.TempDir(), "backend.log"))
	if err != nil || on || os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN") != "existing-private-service" || os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR") != "" {
		t.Fatal("独立部署配置被修改")
	}
}

// TestEmbeddedReplyRejectsInvalidStorageAndAddress 验证相对目录及无效地址不会生成看似可用的运行服务。
func TestEmbeddedReplyRejectsInvalidStorageAndAddress(t *testing.T) {
	t.Setenv("GOODHR_AUTO_REPLY_RUNTIME_URL", "")
	t.Setenv("GOODHR_AUTO_REPLY_FILES_DIR", "relative/files")
	if _, err := prepareAutoReplyEnvironment(filepath.Join(t.TempDir(), "backend.log")); err == nil {
		t.Fatal("相对附件目录被接受")
	}
	for addr, expected := range map[string]string{":8086": "http://127.0.0.1:8086", "0.0.0.0:8084": "http://127.0.0.1:8084", "[::]:8084": "http://[::1]:8084"} {
		if value, err := autoReplyBackendURL(addr); err != nil || value != expected {
			t.Fatalf("内部事务地址错误：%s %v", value, err)
		}
	}
	if _, err := autoReplyBackendURL("invalid"); err == nil {
		t.Fatal("无端口地址被接受")
	}
}
