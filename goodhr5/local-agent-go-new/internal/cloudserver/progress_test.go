// Package cloudserver 本文件验证云端初始化、扫描和等待的中文状态，不丢失内部故障阶段。
package cloudserver

import (
	"encoding/json"
	"goodhr5/local-agent-go-new/internal/replybridge"
	"strings"
	"testing"
	"time"
)

// TestBrowserFailureKeepsSafeRootReason 验证设备实际错误可供工作台展开，同时复用凭据脱敏，禁止回写发送或采集数据。
func TestBrowserFailureKeepsSafeRootReason(t *testing.T) {
	run, fixture, _, _, _, _ := serverGoalFixture(t)
	logger := &runLogger{run: run}
	logger.Step("task", "auto_reply", "process_conversation", "failed", time.Now(), &replybridge.RemoteError{Code: "ACTION_FAILED", Message: "BOSS当前会话的候选人编号还没核实 token=PRIVATE_TOKEN"})
	found := false
	for key, row := range fixture.data {
		if !strings.HasPrefix(key, "runtime_events|") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(row.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if text, ok := event["error_detail"].(string); ok && strings.Contains(text, "候选人编号还没核实") {
			found = true
		}
		if strings.Contains(string(row.Payload), "PRIVATE_TOKEN") {
			t.Fatal("设备错误泄露凭据")
		}
	}
	if !found {
		t.Fatal("设备实际错误原因仍被笼统文案覆盖")
	}
}

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
