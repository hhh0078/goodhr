//go:build darwin

// 本文件负责用隔离假进程验证 Mac 升级脚本只停止目标 Agent，并等待本地端口释放。
package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMacPreinstallListenerHelper 仅在独立测试进程中模拟占用本地端口的旧客户端，收到正常退出信号后释放端口。
func TestMacPreinstallListenerHelper(t *testing.T) {
	if os.Getenv("GOODHR_PREINSTALL_HELPER") != "1" {
		return
	}
	if port := os.Getenv("GOODHR_PREINSTALL_PORT"); port != "" {
		listener, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	if err := os.WriteFile(os.Getenv("GOODHR_PREINSTALL_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	<-ctx.Done()
}

// TestMacPreinstallChecksReleaseAppIdentity 验证默认端口上的 release 旧版被正常退出，而相同文件名但不同应用身份的进程保留。
func TestMacPreinstallChecksReleaseAppIdentity(t *testing.T) {
	for _, bundleID := range []string{"cn.58it.goodhr.localagent", "example.unrelated.agent"} {
		t.Run(bundleID, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			bundle := filepath.Join(root, "release", "GoodHR Local Agent.app")
			executable := filepath.Join(bundle, "Contents", "MacOS", "goodhr-local-agent")
			if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
				t.Fatal(err)
			}
			current, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			binary, err := os.ReadFile(current)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(executable, binary, 0700); err != nil {
				t.Fatal(err)
			}
			plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>` + bundleID + `</string></dict></plist>`
			if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			_ = listener.Close()
			ready := filepath.Join(root, "ready")
			child := exec.Command(executable, "-test.run=^TestMacPreinstallListenerHelper$")
			child.Env = append(os.Environ(), "GOODHR_PREINSTALL_HELPER=1", "GOODHR_PREINSTALL_PORT="+strconv.Itoa(port), "GOODHR_PREINSTALL_READY="+ready)
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("隔离旧客户端没有监听测试端口")
				}
				time.Sleep(50 * time.Millisecond)
			}
			content, err := os.ReadFile("../../packaging/macos/preinstall")
			if err != nil {
				t.Fatal(err)
			}
			script := strings.ReplaceAll(string(content), "health_port=43129", "health_port="+strconv.Itoa(port))
			script = strings.ReplaceAll(script, "console_user=$(/usr/bin/stat -f %Su /dev/console)", "console_user=$(/usr/bin/id -un)")
			// 隔离测试不能匹配真实 /Applications 客户端，只从本次临时端口发现假进程。
			script = strings.ReplaceAll(script, "/Applications/GoodHR", root+"/not-installed/GoodHR")
			path := filepath.Join(root, "preinstall-test")
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if output, err := exec.CommandContext(ctx, "/bin/sh", path).CombinedOutput(); err != nil {
				t.Fatalf("安装前检查失败：%v，%s", err, output)
			}
			connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
			if connection != nil {
				_ = connection.Close()
			}
			if bundleID == "cn.58it.goodhr.localagent" && err == nil {
				t.Fatal("同身份 release 旧版仍然占用本地端口")
			}
			if bundleID != "cn.58it.goodhr.localagent" && err != nil {
				t.Fatal("不同应用身份的进程被误停")
			}
		})
	}
}

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
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sleepBinary, err := os.ReadFile(testExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeExecutable, sleepBinary, 0700); err != nil {
		t.Fatal(err)
	}
	target := exec.Command(fakeExecutable, "-test.run=^TestMacPreinstallListenerHelper$")
	target.Env = append(os.Environ(), "GOODHR_PREINSTALL_HELPER=1", "GOODHR_PREINSTALL_PORT=", "GOODHR_PREINSTALL_READY="+filepath.Join(tempDir, "target-ready"))
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
	unrelated := exec.Command(unrelatedExecutable, "-test.run=^TestMacPreinstallListenerHelper$")
	unrelated.Env = append(os.Environ(), "GOODHR_PREINSTALL_HELPER=1", "GOODHR_PREINSTALL_PORT=", "GOODHR_PREINSTALL_READY="+filepath.Join(tempDir, "unrelated-ready"))
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for _, marker := range []string{"target-ready", "unrelated-ready"} {
		for {
			if _, err := os.Stat(filepath.Join(tempDir, marker)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("隔离进程没有准备好")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
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
