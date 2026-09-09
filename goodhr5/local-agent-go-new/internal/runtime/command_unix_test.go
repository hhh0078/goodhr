//go:build !windows

// Package runtime 文件作用：用真实子进程验证 Key 切换和安装超时能正确结束进程。
package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	browserprocess "goodhr5/local-agent-go-new/internal/browser/process"
)

// TestSameKeyKeepsWorkerAndChangedKeyStopsIt 验证重复同步不关闭 Worker，换 Key 会等待旧进程退出。
func TestSameKeyKeepsWorkerAndChangedKeyStopsIt(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "worker.sh")
	pidFile := filepath.Join(root, "pid")
	t.Setenv("GOODHR_TEST_PID_FILE", pidFile)
	if err := os.WriteFile(entry, []byte("echo $$ > \"$GOODHR_TEST_PID_FILE\"\nexec sleep 60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker := browserprocess.New("/bin/sh", entry, 0, nil)
	defer worker.Stop()
	manager := &Manager{runtimeDir: filepath.Join(root, "runtime"), worker: worker}
	key := "cb_test_unchanged_123456"
	if err := manager.SaveCloakBrowserLicenseKey(key); err != nil {
		t.Fatal(err)
	}
	worker.SetExecutable("/bin/sh")
	if err := worker.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	var pid int
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		data, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if pid > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("测试子进程没有就绪")
	}
	if err := manager.SaveCloakBrowserLicenseKey(key); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("相同 Key 停止了 Worker：%v", err)
	}
	if err := manager.SaveCloakBrowserLicenseKey(""); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("清除 Key 后旧 Worker 仍在运行")
	}
}

// TestStreamingCommandTimeoutKeepsOutput 验证超时不会丢失已输出的错误上下文，也不会无限等待管道。
func TestStreamingCommandTimeoutKeepsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sh", "-c", "echo preparing; exec sleep 30")
	started := time.Now()
	output, err := runStreamingCommand(command, "", nil)
	if err == nil || !strings.Contains(output, "preparing") || time.Since(started) > 3*time.Second {
		t.Fatalf("超时处理不正确：output=%q err=%v", output, err)
	}
	message := commandFailure(output, ctx.Err())
	if !strings.Contains(message, "preparing") || !strings.Contains(message, "deadline exceeded") {
		t.Fatalf("错误信息丢失：%s", message)
	}
}
