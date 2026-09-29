// Package diagnostics 文件作用：验证浏览器原生退出码、信号、主动关闭及辅助进程过滤。
package diagnostics

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestNativeBrowserExitEvidence 验证不依赖页面脚本即可收集原生浏览器错误和信号。
func TestNativeBrowserExitEvidence(t *testing.T) {
	var output bytes.Buffer
	var events [][]byte
	sink := WorkerLine(&output, func(line []byte) { events = append(events, append([]byte(nil), line...)) })
	for _, line := range []string{
		"pw:browser <launching> C:/runtime/camoufox.exe -juggler-pipe",
		"pw:browser <launched> pid=123",
		"pw:browser [pid=123][err] Crash reason: memory fault token=private-secret",
		"pw:browser [pid=123] <process did exit: exitCode=null, signal=SIGSEGV>",
	} {
		sink([]byte(line))
	}
	var event map[string]any
	if len(events) != 4 || json.Unmarshal(events[3], &event) != nil {
		t.Fatalf("没有原生退出事件：%s", output.String())
	}
	if event["action"] != "browser.process_exit" || event["category"] != "异常退出" || event["signal"] != "SIGSEGV" || event["exit_code"] != nil {
		t.Fatalf("原生退出详情不完整：%s", events[3])
	}
	if !bytes.Contains(events[3], []byte("memory fault")) || strings.Contains(output.String(), "private-secret") {
		t.Fatal("原始错误丢失或敏感数据未脱敏")
	}
}

// TestNativeBrowserExpectedStopAndAuxiliaryFilter 验证主动关闭不误报且辅助进程不生成浏览器事件。
func TestNativeBrowserExpectedStopAndAuxiliaryFilter(t *testing.T) {
	trace := &browserProcessTrace{}
	trace.observe("pw:browser <launching> camoufox.exe")
	trace.observe("pw:browser <launched> pid=123")
	trace.observe("pw:browser <launching> ffmpeg.exe")
	trace.observe("pw:browser <launched> pid=456")
	if data := trace.observe("pw:browser [pid=456] <process did exit: exitCode=1, signal=null>"); len(data) != 0 {
		t.Fatal("误把录屏辅助进程当成浏览器")
	}
	trace.observe("pw:browser [pid=123] <gracefully close start>")
	data := trace.observe("pw:browser [pid=123] <process did exit: exitCode=1, signal=null>")
	var event map[string]any
	_ = json.Unmarshal(data, &event)
	if event["category"] != "运行" || event["expected_stop"] != true {
		t.Fatalf("主动结束被误报：%s", data)
	}
}
