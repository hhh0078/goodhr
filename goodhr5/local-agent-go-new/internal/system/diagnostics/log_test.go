// 文件作用说明：验证日志持久化、凭据脱敏与轮转，确保异常退出证据可供排查。
package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogRedactionAndRotation 验证敏感字段不落盘且轮转后仍能保存新日志。
func TestLogRedactionAndRotation(t *testing.T) {
	directory := t.TempDir()
	writer, err := Open(directory, "worker.log")
	if err != nil {
		t.Fatal(err)
	}
	writer.limit = 300
	line := []byte(`{"token":"private-value","api_key":"api-secret-value","nested":{"password":"secret-value"},"url":"https://example.test/a?access=hidden-value","error":"Bearer raw-value"}`)
	if _, err = writer.Write(line); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(directory, "worker.log"))
	for _, secret := range []string{"private-value", "secret-value", "hidden-value", "raw-value", "api-secret-value"} {
		if strings.Contains(string(first), secret) {
			t.Fatalf("敏感内容未脱敏：%s", secret)
		}
	}
	if _, err = writer.Write([]byte(strings.Repeat("x", 200))); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	if _, err = os.Stat(filepath.Join(directory, "worker.log.1")); err != nil {
		t.Fatal(err)
	}
}

// TestWorkerStderrWithoutTrace 保留没有任务编号的启动日志和未换行的崩溃信息。
func TestWorkerStderrWithoutTrace(t *testing.T) {
	writer, err := Open(t.TempDir(), "worker.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	forwarded := false
	sink := WorkerLine(writer, func([]byte) { forwarded = true })
	sink([]byte("Error: startup failed password=super-secret"))
	data, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if forwarded || !strings.Contains(string(data), "startup failed") || strings.Contains(string(data), "super-secret") {
		t.Fatal("崩溃诊断未保存或未脱敏")
	}
}
