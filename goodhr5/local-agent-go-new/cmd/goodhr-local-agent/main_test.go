// 本文件负责验证本地程序异常退出时 JSON 行日志使用独立的异常退出类别。
package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestBrowserExtensionCommandReturnsBeforeAgentBootstrap 验证插件准备命令不会读取运行配置或启动正常 Agent。
func TestBrowserExtensionCommandReturnsBeforeAgentBootstrap(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	commandIndex := strings.Index(source, "if *prepareBrowserExtension")
	configIndex := strings.Index(source, "config.Load(")
	bootstrapIndex := strings.Index(source, "bootstrap.New(")
	if commandIndex < 0 || configIndex < 0 || bootstrapIndex < 0 || commandIndex >= configIndex || commandIndex >= bootstrapIndex {
		t.Fatal("插件准备命令没有在正常 Agent 配置和启动前返回")
	}
}

// TestNormalStartupPreparesFirefoxBeforeBootstrap 验证正常启动会先同步 Firefox 插件，失败只记录并继续组装 Agent。
func TestNormalStartupPreparesFirefoxBeforeBootstrap(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("main.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	prepareIndex := strings.Index(source, "extensionResult, extensionErr := prepareManagedFirefoxExtension(extensionCtx, cfg)")
	bootstrapIndex := strings.Index(source, "application, err := bootstrap.New(cfg")
	if prepareIndex < 0 || bootstrapIndex < 0 || prepareIndex >= bootstrapIndex {
		t.Fatal("正常启动没有在浏览器和业务服务组装前同步 Firefox 插件")
	}
	failureIndex := strings.Index(source[prepareIndex:bootstrapIndex], "if extensionErr != nil")
	logIndex := strings.Index(source[prepareIndex:bootstrapIndex], "event=firefox_extension_prepare_failed kept_previous=true")
	if failureIndex < 0 || logIndex < failureIndex {
		t.Fatal("Firefox 插件同步失败没有记录保留旧版的日志")
	}
	if strings.Contains(source[prepareIndex:bootstrapIndex], "return extensionErr") {
		t.Fatal("Firefox 插件同步失败不应阻断 Agent 启动")
	}
}
