//go:build darwin

// 本文件负责用隔离假进程验证 Mac 升级脚本只停止目标 Agent，并等待本地端口释放。
package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMacPreinstallStopsOnlyInstalledAgent 验证升级前只停目标进程，且端口释放前不会继续安装。
func TestMacPreinstallStopsOnlyInstalledAgent(t *testing.T) {
	tempDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fakeExecutable := filepath.Join(tempDir, "GoodHR Test Agent.app", "Contents", "MacOS", "goodhr-local-agent")
	if err := os.MkdirAll(filepath.Dir(fakeExecutable), 0700); err != nil {
		t.Fatal(err)
	}
	sleepBinary, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeExecutable, sleepBinary, 0700); err != nil {
		t.Fatal(err)
	}
	target := exec.Command(fakeExecutable, "30")
	if err := target.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = target.Process.Kill() }()
	targetDone := make(chan error, 1)
	go func() { targetDone <- target.Wait() }()
	unrelatedExecutable := filepath.Join(tempDir, "Other Agent.app", "Contents", "MacOS", "goodhr-local-agent")
	if err := os.MkdirAll(filepath.Dir(unrelatedExecutable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelatedExecutable, sleepBinary, 0700); err != nil {
		t.Fatal(err)
	}
	unrelated := exec.Command(unrelatedExecutable, "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	healthPort := listener.Addr().(*net.TCPAddr).Port

	preinstallPath := filepath.Join("..", "..", "packaging", "macos", "preinstall")
	content, err := os.ReadFile(preinstallPath)
	if err != nil {
		t.Fatal(err)
	}
	script := string(content)
	replacements := map[string]string{
		"agent_executable='/Applications/GoodHR Local Agent.app/Contents/MacOS/goodhr-local-agent'":                "agent_executable='" + fakeExecutable + "'",
		"agent_pattern='^/Applications/GoodHR Local Agent[.]app/Contents/MacOS/goodhr-local-agent($|[[:space:]])'": "agent_pattern='^" + regexp.QuoteMeta(fakeExecutable) + "($|[[:space:]])'",
		"health_port=43129": "health_port=" + strconv.Itoa(healthPort),
		"console_user=$(/usr/bin/stat -f %Su /dev/console)": "console_user=$(/usr/bin/id -un)",
	}
	for before, after := range replacements {
		if !strings.Contains(script, before) {
			t.Fatalf("升级脚本缺少预期配置：%s", before)
		}
		script = strings.Replace(script, before, after, 1)
	}
	testScript := filepath.Join(tempDir, "preinstall-test")
	if err := os.WriteFile(testScript, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	install := exec.CommandContext(ctx, "/bin/sh", testScript)
	output := new(strings.Builder)
	install.Stdout = output
	install.Stderr = output
	if err := install.Start(); err != nil {
		t.Fatal(err)
	}
	installDone := make(chan error, 1)
	go func() { installDone <- install.Wait() }()
	select {
	case err := <-installDone:
		t.Fatalf("端口仍被占用，升级脚本却提前结束：%v，输出：%s", err, output.String())
	case <-time.After(1200 * time.Millisecond):
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-installDone:
		if err != nil {
			t.Fatalf("隔离升级脚本失败：%v，输出：%s", err, output.String())
		}
	case <-ctx.Done():
		t.Fatalf("隔离升级脚本超时：%v，输出：%s", ctx.Err(), output.String())
	}
	if err := syscall.Kill(unrelated.Process.Pid, 0); err != nil {
		t.Fatalf("无关进程被误停：%v", err)
	}
	select {
	case <-targetDone:
	case <-time.After(2 * time.Second):
		t.Fatal("目标旧版 Agent 没有退出")
	}
	unrelatedListener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(healthPort))
	if err != nil {
		t.Fatal(err)
	}
	defer unrelatedListener.Close()
	freshCtx, cancelFresh := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelFresh()
	if freshOutput, err := exec.CommandContext(freshCtx, "/bin/sh", testScript).CombinedOutput(); err != nil {
		t.Fatalf("没有旧 Agent 时不应因端口占用阻断安装：%v，输出：%s", err, freshOutput)
	}
}
