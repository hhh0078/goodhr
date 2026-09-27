// 文件作用说明：验证日志持久化、凭据脱敏与轮转，确保异常退出证据可供排查。
package diagnostics

import (
	"encoding/json"
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
	line := []byte(`{"token":"private-value","api_key":"api-secret-value","nested":{"password":"secret-value","candidateName":"张三"},"url":"https://example.test/a?access=hidden-value","error":"Bearer raw-value 13800138000 candidate@example.com"}`)
	if _, err = writer.Write(line); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(directory, "worker.log"))
	for _, secret := range []string{"private-value", "secret-value", "hidden-value", "raw-value", "api-secret-value", "13800138000", "candidate@example.com", "张三"} {
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

// TestLogAlwaysWritesJSONLines 验证普通 Go 输出与 Worker 输出都满足一行一个完整 JSON 对象。
func TestLogAlwaysWritesJSONLines(t *testing.T) {
	writer, err := Open(t.TempDir(), "goodhr.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("agent started")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(`{"action":"element.find","status":"success"}`)); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("invalid JSON line: %v", err)
		}
		for _, key := range []string{"timestamp", "category", "is_error", "error_message", "input", "output"} {
			if _, ok := item[key]; !ok {
				t.Fatalf("missing %s: %s", key, line)
			}
		}
		if item["category"] != "运行" {
			t.Fatalf("普通记录类别应为运行：%s", line)
		}
	}
	_ = writer.Close()
}

// TestNormalizeDistinguishesEmptyError 验证正常步骤的空 error 字段不会被误判为错误。
func TestNormalizeDistinguishesEmptyError(t *testing.T) {
	for _, sample := range []struct {
		line    string
		isError bool
	}{
		{`task_id=task flow=greeting step=open_detail status=success duration_ms=35 error=""`, false},
		{`task_id=task flow=greeting step=open_detail status=failed duration_ms=35 error="按钮没找到"`, true},
	} {
		var item map[string]any
		if err := json.Unmarshal(Normalize([]byte(sample.line)), &item); err != nil {
			t.Fatal(err)
		}
		if item["is_error"] != sample.isError {
			t.Fatalf("日志错误分类不正确：%v", item)
		}
	}
}

// TestNormalizeKeepsAbnormalExit 验证异常退出不会被通用错误分类覆盖。
func TestNormalizeKeepsAbnormalExit(t *testing.T) {
	var item map[string]any
	if err := json.Unmarshal(Normalize([]byte(`{"action":"browser.exit","category":"异常退出","is_error":true,"error_message":"浏览器连接断开"}`)), &item); err != nil {
		t.Fatal(err)
	}
	if item["category"] != "异常退出" || item["is_error"] != true {
		t.Fatalf("异常退出分类丢失：%v", item)
	}
}

// TestRunMarkerDetectsAbnormalExit 验证上次意外退出可提取致命崩溃原因。
func TestRunMarkerDetectsAbnormalExit(t *testing.T) {
	directory := t.TempDir()
	crashPath := filepath.Join(directory, "crash.log")
	if err := os.WriteFile(crashPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MarkRun(directory, crashPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crashPath, []byte("fatal error: test crash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	marker, previous, err := MarkRun(directory, crashPath, 24)
	if err != nil || previous == nil || !strings.Contains(previous.Reason, "fatal error: test crash") {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}
	_ = marker.Clear()
}

// TestResetLegacyLogsPreservesPreviousCrashReason 验证首次清空旧文件前仍能识别上次闪退，且新日志不会反复删除。
func TestResetLegacyLogsPreservesPreviousCrashReason(t *testing.T) {
	directory := t.TempDir()
	crashPath := filepath.Join(directory, "crash.log")
	if _, _, err := MarkRun(directory, crashPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crashPath, []byte("fatal error: old crash\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"goodhr.jsonl", "goodhr.jsonl.1"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("旧日志"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	previous := ReadPreviousRun(directory, crashPath)
	if previous == nil || !strings.Contains(previous.Reason, "fatal error: old crash") {
		t.Fatalf("清理前未能保留闪退原因：%+v", previous)
	}
	reset, err := ResetLegacyLogs(directory)
	if err != nil || !reset {
		t.Fatalf("首次清理旧日志失败：reset=%v err=%v", reset, err)
	}
	for _, name := range []string{"goodhr.jsonl", "goodhr.jsonl.1", "crash.log", "agent-running.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("旧文件仍存在：%s err=%v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "goodhr.jsonl"), []byte("新日志"), 0600); err != nil {
		t.Fatal(err)
	}
	reset, err = ResetLegacyLogs(directory)
	if err != nil || reset {
		t.Fatalf("升级标记未防止重复清理：reset=%v err=%v", reset, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "goodhr.jsonl"))
	if err != nil || string(content) != "新日志" {
		t.Fatalf("新日志被误删：%q err=%v", content, err)
	}
}

// TestWorkerStderrWithoutTrace 保留没有任务编号的启动日志和未换行的崩溃信息。
func TestWorkerStderrWithoutTrace(t *testing.T) {
	writer, err := Open(t.TempDir(), "worker.log")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var forwarded []byte
	sink := WorkerLine(writer, func(line []byte) { forwarded = append([]byte(nil), line...) })
	sink([]byte("Error: startup failed password=super-secret"))
	data, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(forwarded), `"action":"worker.stderr"`) || !strings.Contains(string(data), "startup failed") || strings.Contains(string(data), "super-secret") || strings.Contains(string(forwarded), "super-secret") {
		t.Fatal("崩溃诊断未保存或未脱敏")
	}
}
