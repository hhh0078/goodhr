//go:build darwin

// 本文件用独立应用包和假业务进程验证原生启动器响应重复打开、正常退出并等待子进程收尾，不访问用户数据。
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMacLauncherGracefulQuit 用真实 AppKit 入口验证正常退出等待业务完成，退出中重复打开不新建进程。
func TestMacLauncherGracefulQuit(t *testing.T) {
	if os.Getenv("GOODHR_MAC_LAUNCHER_TEST") != "1" {
		t.Skip("需在已登录的 Mac 桌面显式启用隔离原生启动器测试")
	}
	root := t.TempDir()
	app := filepath.Join(root, "GoodHR Launcher Fixture.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(macos, "goodhr-launcher")
	compile := exec.Command("xcrun", "clang", "-fobjc-arc", "-framework", "Cocoa", "../../packaging/macos/launcher.m", "-o", launcher)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("编译原生启动器失败：%v，%s", err, output)
	}
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>goodhr-launcher</string><key>CFBundleIdentifier</key><string>cn.58it.goodhr.launcher-fixture</string><key>CFBundlePackageType</key><string>APPL</string><key>LSUIElement</key><true/></dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "agent")
	quotedMarker := "'" + strings.ReplaceAll(marker, "'", "'\\''") + "'"
	fake := fmt.Sprintf("#!/bin/sh\n# 文件作用：记录隔离子进程启动和正常退出，模拟两秒业务收尾。\nmarker=%s\necho $$ >> \"$marker-starts\"\ntrap 'echo stopping > \"$marker-stopping\"; sleep 2; echo stopped > \"$marker-stopped\"; exit 0' TERM\nwhile :; do sleep 0.1; done\n", quotedMarker)
	if err := os.WriteFile(filepath.Join(macos, "goodhr-local-agent"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(launcher)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill() }()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	// 失败时也仅清理当前夹具的业务进程，不查找或停止用户的真实客户端。
	defer func() {
		if content, err := os.ReadFile(marker + "-starts"); err == nil {
			for _, value := range strings.Fields(string(content)) {
				if pid, err := strconv.Atoi(value); err == nil {
					_ = syscall.Kill(pid, syscall.SIGTERM)
				}
			}
		}
	}()
	deadline := time.Now().Add(12 * time.Second)
	for {
		if _, err := os.Stat(marker + "-starts"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("原生启动器没有启动隔离业务进程")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(marker + "-stopping"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("退出请求未传给业务进程")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("业务尚未收尾，原生入口却提前退出")
	default:
	}
	// 退出中重新打开必须及时收到系统回执；此时不会打开任何浏览器或控制台。
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "/usr/bin/open", "-a", app).CombinedOutput(); err != nil {
		t.Fatalf("原生入口没有及时响应重新打开：%v，%s", err, output)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		markerContent, markerErr := os.ReadFile(marker + "-stopped")
		t.Fatalf("业务完成后原生入口没有退出，业务收尾标记=%q，读取错误=%v", markerContent, markerErr)
	}
	if _, err := os.Stat(marker + "-stopped"); err != nil {
		t.Fatal("业务没有完成正常退出")
	}
	content, err := os.ReadFile(marker + "-starts")
	if err != nil || len(strings.Fields(string(content))) != 1 {
		t.Fatalf("重复打开创建了多余业务进程：%q，%v", content, err)
	}
}
