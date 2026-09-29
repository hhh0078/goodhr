// Package ocr 文件作用：保留 OCR 原始错误输出与退出码，区分主动回收和进程故障。
package ocr

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ocrProcessDiagnostics 只收集标准错误，不保存识别结果、截图或图片路径。
type ocrProcessDiagnostics struct {
	mu         sync.Mutex
	startedAt  time.Time
	stopReason string
	failure    string
	stderr     []byte
}

// Write 缓存最多四 KB 的 stderr 尾部，供退出事件使用。
func (d *ocrProcessDiagnostics) Write(data []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stderr = append(d.stderr, data...)
	if len(d.stderr) > 4096 {
		d.stderr = append([]byte(nil), d.stderr[len(d.stderr)-4096:]...)
	}
	return len(data), nil
}

// mark 保存主动停止原因或当前识别的原始错误。
func (d *ocrProcessDiagnostics) mark(reason string, failure error) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if reason != "" && d.stopReason == "" {
		d.stopReason = reason
	}
	if failure != nil {
		d.failure = failure.Error()
	}
}

// exitEvent 生成包含真实退出码、运行时间和 stderr 的结构化退出事件。
func (d *ocrProcessDiagnostics) exitEvent(pid, code int, err error) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	expected := d.stopReason != ""
	abnormal := !expected || d.failure != ""
	category, status := "运行", "finished"
	reason := d.stopReason
	if abnormal {
		category, status, reason = "异常退出", "failed", fmt.Sprintf("OCR 进程意外结束，退出码 %d", code)
	}
	waitError := ""
	if err != nil {
		waitError = err.Error()
	}
	errorMessage := ""
	if abnormal {
		errorMessage = reason
	}
	data, _ := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": category, "action": "ocr.exit",
		"step": "process_exit", "status": status, "is_error": abnormal, "error_message": errorMessage,
		"exit_reason": reason, "expected_stop": expected, "exit_code": code, "pid": pid,
		"input": map[string]any{}, "output": map[string]any{"wait_error": waitError,
			"request_error": d.failure, "stderr_tail": strings.ToValidUTF8(string(d.stderr), ""), "stop_reason": d.stopReason},
		"diagnostics": map[string]any{"platform": runtime.GOOS, "arch": runtime.GOARCH,
			"started_at": d.startedAt.UTC().Format(time.RFC3339Nano), "uptime_ms": time.Since(d.startedAt).Milliseconds()},
	})
	return data
}
