// Package cloudserver 本文件验证云端初始化、扫描和等待的中文状态，不丢失内部故障阶段。
package cloudserver

import "testing"

// TestRuntimeProgressKeepsInternalFailureStage 验证实时中文进度能替换初始化字样，失败时仍保留准确的内部排障阶段。
func TestRuntimeProgressKeepsInternalFailureStage(t *testing.T) {
	r := &activeRun{status: "running", currentStep: "initialize_auto_reply_page"}
	l := &runLogger{run: r}
	if r.state()["current_step"] != "正在整理自动回复页面" {
		t.Fatal("初始化仍显示英文内部步骤")
	}
	for _, message := range []string{"正在检查候选人消息", "暂时没有新消息，我继续盯着"} {
		l.Progress("task", message)
		if r.state()["current_step"] != message || r.currentStep != "initialize_auto_reply_page" {
			t.Fatal("进度没更新或覆盖了内部故障阶段", r.state())
		}
	}
	r.status = "failed"
	if r.state()["current_step"] != "initialize_auto_reply_page" {
		t.Fatal("等待文案覆盖了真正的失败阶段")
	}
}
