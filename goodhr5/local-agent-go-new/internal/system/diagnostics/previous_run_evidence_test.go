// Package diagnostics 文件作用：验证大崩溃文件仍保留致命原因，并恢复主程序捕获异常的调用栈。
package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPreviousRunKeepsFatalHeader 验证超大 Go 调用栈不会让文件开头的致命原因丢失。
func TestPreviousRunKeepsFatalHeader(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "crash.log")
	if _, _, err := MarkRun(directory, path, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fatal error: out of memory\ngoroutine 1 [running]:\n"+strings.Repeat("other goroutine stack\n", 3000)), 0600); err != nil {
		t.Fatal(err)
	}
	previous := ReadPreviousRun(directory, path)
	if previous == nil || !strings.Contains(previous.Reason, "out of memory") || !strings.Contains(previous.CrashStack, "goroutine 1") {
		t.Fatalf("致命原因或主栈丢失：%+v", previous)
	}
}

// TestPreviousRunRecoversCaughtPanic 验证 recover 的异常也能在下次启动补传，并保留最近操作。
func TestPreviousRunRecoversCaughtPanic(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "crash.log")
	if _, _, err := MarkRun(directory, path, 0); err != nil {
		t.Fatal(err)
	}
	data := `{"action":"element.click","step":"mouse.down","status":"start"}` + "\n" +
		`{"action":"agent.panic","is_error":true,"output":{"event":"agent_panic","exit_reason":"panic: original failure token=private","stack":"goroutine 1 [running]:\n main.run"}}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, "goodhr.jsonl"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	previous := ReadPreviousRun(directory, path)
	if previous == nil || !strings.Contains(previous.Reason, "original failure") || !strings.Contains(previous.CrashStack, "main.run") || len(previous.RecentOperations) != 2 {
		t.Fatalf("捕获异常证据丢失：%+v", previous)
	}
	if strings.Contains(previous.Reason, "private") {
		t.Fatal("崩溃凭据未脱敏")
	}
}
