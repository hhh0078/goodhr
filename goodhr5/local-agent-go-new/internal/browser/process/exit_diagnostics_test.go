// Package process 文件作用：验证进程退出保存原始错误、正确区分主动停止并避免重复记录。
package process

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestProcessExitPreservesCrashEvidence 验证分段输出和最后未换行的栈信息能进入退出事件。
func TestProcessExitPreservesCrashEvidence(t *testing.T) {
	w := &lineSinkWriter{diagnostics: processDiagnostics{startedAt: time.Now()}}
	_, _ = w.Write([]byte(`{"action":"element.click","step":"mouse.down","status":"failed","is_error":true,"error_message":"原始鼠标超时","stack":"Error: APZ timeout\n at Mouse.down"}` + "\n"))
	_, _ = w.Write([]byte("native fatal: process aborted"))
	w.Flush()
	data := w.ExitEvent(123, 1, errors.New("exit status 1"), "exit status 1")
	var item map[string]any
	if json.Unmarshal(data, &item) != nil || item["category"] != "异常退出" || item["exit_code"] != float64(1) {
		t.Fatalf("退出类别或退出码丢失：%s", data)
	}
	for _, text := range []string{"原始鼠标超时", "APZ timeout", "native fatal", "0x00000001"} {
		if !strings.Contains(string(data), text) {
			t.Fatalf("原始诊断丢失 %s：%s", text, data)
		}
	}
}

// TestRequestedStopDoesNotHideUnrequestedWindowsCode 验证相同 Windows 退出码按实际停止意图区分。
func TestRequestedStopDoesNotHideUnrequestedWindowsCode(t *testing.T) {
	for _, expected := range []bool{true, false} {
		w := &lineSinkWriter{diagnostics: processDiagnostics{startedAt: time.Now()}}
		if expected {
			w.MarkStopping("安装更新前主动退出")
			w.MarkStopping("通用停止")
		}
		data := w.ExitEvent(123, 1073807364, errors.New("exit status 1073807364"), "")
		var item map[string]any
		_ = json.Unmarshal(data, &item)
		if item["is_error"] != !expected {
			t.Fatalf("停止意图分类错误：%s", data)
		}
		if expected && !strings.Contains(string(data), "安装更新前主动退出") {
			t.Fatal("原停止原因被覆盖")
		}
		if !strings.Contains(string(data), "0x40010004") {
			t.Fatal("Windows 十六进制退出码丢失")
		}
	}
}

// TestNodeExitDoesNotGenerateDuplicateEvent 验证原始 Node 退出日志存在时不再生成通用重复事件。
func TestNodeExitDoesNotGenerateDuplicateEvent(t *testing.T) {
	w := &lineSinkWriter{diagnostics: processDiagnostics{startedAt: time.Now()}}
	_, _ = w.Write([]byte(`{"action":"worker.exit","step":"exit","exit_code":1}` + "\n"))
	if data := w.ExitEvent(123, 1, nil, ""); len(data) != 0 {
		t.Fatalf("退出被重复记录：%s", data)
	}
}
