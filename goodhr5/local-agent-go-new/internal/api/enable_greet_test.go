// Package api 文件作用：验证岗位兼容入口和统一任务请求不会丢失三态打招呼开关。
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/flow/shared"
)

// TestPositionRunPreservesGreetingSetting 验证未指定、空值、开启和关闭会完整传入统一任务快照。
func TestPositionRunPreservesGreetingSetting(t *testing.T) {
	for _, test := range []struct {
		name, body, taskType string
		present, enabled     bool
	}{
		{name: "omitted", body: `{"token":"fixture"}`, taskType: "greeting"},
		{name: "null", body: `{"token":"fixture","enable_greet":null}`, taskType: "greeting"},
		{name: "true", body: `{"token":"fixture","enable_greet":true}`, taskType: "greeting", present: true, enabled: true},
		{name: "false_default_type", body: `{"token":"fixture","enable_greet":false}`, taskType: "greeting", present: true},
		{name: "false_explicit_type", body: `{"token":"fixture","enable_greet":false,"task_type":" GREETING "}`, taskType: "greeting", present: true},
		{name: "false_auto_reply", body: `{"token":"fixture","enable_greet":false,"task_type":" AUTO_REPLY "}`, taskType: "auto_reply", present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/local/positions/position-1/run", strings.NewReader(test.body))
			var parsed localPositionRunRequest
			if err := decodeJSON(httptest.NewRecorder(), request, &parsed); err != nil {
				t.Fatal(err)
			}
			start := parsed.startRequest("position-1", "task-1")
			if start.TaskID != "task-1" || start.PositionID != "position-1" || start.Token != "fixture" || start.TaskType != test.taskType {
				t.Fatalf("岗位启动参数丢失或任务类型未统一：%+v", start)
			}
			if (start.EnableGreet != nil) != test.present || (start.EnableGreet != nil && *start.EnableGreet != test.enabled) {
				t.Fatalf("打招呼开关未完整传递：%+v", start.EnableGreet)
			}
			encoded, err := json.Marshal(start)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot shared.StartRequest
			if err := json.Unmarshal(encoded, &snapshot); err != nil {
				t.Fatal(err)
			}
			if (snapshot.EnableGreet != nil) != test.present || (snapshot.EnableGreet != nil && *snapshot.EnableGreet != test.enabled) {
				t.Fatal("统一任务快照序列化丢失了打招呼开关")
			}
		})
	}
}

// TestUnifiedTaskStartAcceptsDisabledGreeting 验证统一任务入口也能直接解码禁止发送参数。
func TestUnifiedTaskStartAcceptsDisabledGreeting(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start", strings.NewReader(`{"task_id":"task-1","position_id":"position-1","task_type":"greeting","token":"fixture","enable_greet":false}`))
	var parsed shared.StartRequest
	if err := decodeJSON(httptest.NewRecorder(), request, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.EnableGreet == nil || *parsed.EnableGreet {
		t.Fatal("统一启动入口必须保留明确关闭的发送开关")
	}
}
