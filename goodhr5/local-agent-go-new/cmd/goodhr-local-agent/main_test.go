// 本文件负责验证本地程序入口、Mac 应用包模式和异常退出日志。
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMacAppRunsInBackground 验证 Mac 安装包声明为后台应用，避免无窗口的 Agent 在 Dock 持续跳动。
func TestMacAppRunsInBackground(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("仅在 macOS 上验证应用包配置")
	}
	plist := filepath.Join("..", "..", "packaging", "macos", "Info.plist")
	output, err := exec.Command("plutil", "-extract", "LSBackgroundOnly", "raw", "-o", "-", plist).Output()
	if err != nil {
		t.Fatalf("读取 Mac 应用后台配置失败：%v", err)
	}
	if strings.TrimSpace(string(output)) != "true" {
		t.Fatalf("Mac 应用必须声明 LSBackgroundOnly=true，当前为 %q", strings.TrimSpace(string(output)))
	}
}

// TestAgentExitCategory 验证异常退出原因仍可读，且日志类型为异常退出。
func TestAgentExitCategory(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&output)
	log.SetFlags(0)
	defer log.SetOutput(previous)
	defer log.SetFlags(previousFlags)
	logAgentLifecycle("agent_panic", "本地程序发生未捕获异常", "测试崩溃", true, nil)
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event["category"] != "异常退出" || event["error_message"] != "测试崩溃" {
		t.Fatalf("异常退出日志内容不正确：%v", event)
	}
}

// TestNormalStartupDoesNotPrepareChromeExtension 验证正常程序不再保留 Chrome 插件准备命令或扩展管理页入口。
func TestNormalStartupDoesNotPrepareChromeExtension(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{"prepare-browser-extension", "browser-extension-dir", "prepareLatestBrowserExtension", "准备 Chrome 浏览器插件"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("本地程序仍包含 Chrome 插件安装入口：%s", forbidden)
		}
	}
}

// TestNormalStartupDefersFirefoxPreparation 验证启动 Agent 不再同步旧签名插件，准备步骤统一交给浏览器冷启动。
func TestNormalStartupDefersFirefoxPreparation(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{"prepareManagedFirefoxExtension", "browserextension.PrepareFirefox(", "extensionCtx"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Agent 启动仍会覆盖浏览器插件：%s", forbidden)
		}
	}
}
