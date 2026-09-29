// Package diagnostics 文件作用：从 Playwright 原生日志提取浏览器真实退出码、信号和退出前输出。
package diagnostics

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var launchedBrowser = regexp.MustCompile(`<launched> pid=([0-9]+)`)
var nativeBrowserLine = regexp.MustCompile(`\[pid=([0-9]+)\]`)
var nativeBrowserExit = regexp.MustCompile(`<process did exit: exitCode=(-?[0-9]+|null), signal=([^>]+)>`)

// browserProcessTrace 跟踪已确认的 Camoufox/Firefox 进程，不把录屏等辅助进程当成浏览器。
type browserProcessTrace struct {
	launching bool
	pid       int
	startedAt time.Time
	graceful  bool
	tail      []string
}

// observe 接收标准进程调试日志，退出时返回一条结构化、可上传的事件。
func (b *browserProcessTrace) observe(line string) []byte {
	if !strings.Contains(line, "pw:browser") {
		return nil
	}
	if strings.Contains(line, "<launching>") {
		lower := strings.ToLower(line)
		b.launching = strings.Contains(lower, "camoufox") || strings.Contains(lower, "firefox")
		return nil
	}
	if match := launchedBrowser.FindStringSubmatch(line); match != nil && b.launching {
		b.pid, _ = strconv.Atoi(match[1])
		b.startedAt, b.graceful, b.tail, b.launching = time.Now(), false, nil, false
		return nil
	}
	pid := nativeBrowserLine.FindStringSubmatch(line)
	if pid == nil || pid[1] != strconv.Itoa(b.pid) || b.pid == 0 {
		return nil
	}
	if strings.Contains(line, "<gracefully close start>") {
		b.graceful = true
	}
	if strings.Contains(line, "[err]") || strings.Contains(line, "[out]") {
		if len(line) > 500 {
			line = strings.ToValidUTF8(line[:500], "")
		}
		b.tail = append(b.tail, line)
		if len(b.tail) > 6 {
			b.tail = b.tail[len(b.tail)-6:]
		}
	}
	exit := nativeBrowserExit.FindStringSubmatch(line)
	if exit == nil {
		return nil
	}
	var code any
	if exit[1] != "null" {
		code, _ = strconv.ParseInt(exit[1], 10, 64)
	}
	signal := strings.TrimSpace(exit[2])
	abnormal := !b.graceful && (exit[1] != "0" || signal != "null")
	category, status := "运行", "finished"
	reason := "浏览器进程已结束"
	if abnormal {
		category, status, reason = "异常退出", "failed", "浏览器进程异常结束"
	}
	reason += "，退出码 " + exit[1] + "，信号 " + signal
	errorMessage := ""
	if abnormal {
		errorMessage = reason
	}
	data, _ := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "category": category,
		"action": "browser.process_exit", "step": "native_process_exit", "status": status,
		"is_error": abnormal, "error_message": errorMessage, "exit_reason": reason,
		"reason_source": "playwright.process", "expected_stop": b.graceful, "pid": b.pid,
		"exit_code": code, "signal": signal, "browser_uptime_ms": time.Since(b.startedAt).Milliseconds(),
		"input": map[string]any{}, "output": map[string]any{"exit_code": code, "signal": signal, "stderr_tail": b.tail},
	})
	b.pid = 0
	return data
}
