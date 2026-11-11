// Package diagnostics 文件作用：验证匿名页面脚本输出单独保留提示，真实操作与致命错误继续报错。
package diagnostics

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestBrowserScriptOutputClassification 验证仅原始Firefox匿名异常被分类，Node、内部模块、致命及结构化动作错误均不降级。
func TestBrowserScriptOutputClassification(t *testing.T) {
	const prefix = "2026-11-11T15:40:20.000Z pw:browser [pid=42][err] JavaScript error: , line 0: uncaught exception: "
	for _, item := range []struct {
		line   string
		script bool
	}{
		{prefix + "role is ehire_online", true},
		{prefix + "该用户当天已展示", true},
		{prefix + "panic: failed", false},
		{prefix + "out of memory", false},
		{"JavaScript error: , line 0: uncaught exception: role is ehire_online", false},
		{"pw:browser [pid=42][err] JavaScript error: chrome://juggler/content/Runtime.js, line 1: Error: disconnected", false},
		{`{"action":"element.click","step":"click","status":"failed","level":"error","error_message":"role is ehire_online"}`, false},
	} {
		var buf bytes.Buffer
		WorkerLine(&buf, nil)([]byte(item.line))
		var got struct {
			Action, Status, Message string
			IsError                 bool `json:"is_error"`
			Diagnostic              bool
			Output                  map[string]any
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if item.script {
			if got.Action != "browser.script" || got.Status != "warning" || got.IsError || !got.Diagnostic || got.Output["operation_outcome"] != "unknown" || !strings.Contains(got.Message, strings.TrimPrefix(item.line, prefix)) {
				t.Fatalf("脚本诊断丢失或误报操作结果：%s", buf.String())
			}
		} else if !got.IsError || got.Action == "browser.script" {
			t.Fatalf("真实故障被降级：%s", buf.String())
		}
	}
}
