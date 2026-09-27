// Package lifecycle 文件作用：验证平台公开方法的成功、失败和 panic 都进入岗位日志。
package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

type runtimeLogFixture struct {
	model.Runtime
	failure error
	panics  bool
}

// PlatformID 返回测试平台编号。
func (f runtimeLogFixture) PlatformID() string { return "test" }

// OpenLoginPage 模拟平台方法失败或 panic。
func (f runtimeLogFixture) OpenLoginPage(context.Context, model.Browser, model.Config) error {
	if f.panics {
		panic("test panic")
	}
	return f.failure
}

type runtimeLogCollector struct{ events []runtimeLogEvent }
type runtimeLogEvent struct{ step, status string }

// Step 收集测试方法的最终日志。
func (c *runtimeLogCollector) Step(_ string, _ string, step string, status string, _ time.Time, _ error) {
	c.events = append(c.events, runtimeLogEvent{step, status})
}

// TestLoggedRuntimeRecordsMethods 验证正常调用与失败调用都至少有一条结果日志。
func TestLoggedRuntimeRecordsMethods(t *testing.T) {
	collector := &runtimeLogCollector{}
	runtime := &loggedRuntime{Runtime: runtimeLogFixture{failure: errors.New("页面未打开")}, taskID: "task", logger: collector}
	if runtime.PlatformID() != "test" {
		t.Fatal("平台编号未返回")
	}
	if err := runtime.OpenLoginPage(context.Background(), nil, model.Config{}); err == nil {
		t.Fatal("平台失败没有返回")
	}
	if len(collector.events) != 2 || collector.events[0].status != "success" || collector.events[1] != (runtimeLogEvent{"open_login_page", "failed"}) {
		t.Fatalf("方法日志不完整：%+v", collector.events)
	}
}

// TestLoggedRuntimeRecordsPanic 验证平台 panic 写失败日志并继续由任务边界处理。
func TestLoggedRuntimeRecordsPanic(t *testing.T) {
	collector := &runtimeLogCollector{}
	runtime := &loggedRuntime{Runtime: runtimeLogFixture{panics: true}, taskID: "task", logger: collector}
	defer func() {
		if recover() == nil {
			t.Fatal("平台 panic 被意外吞掉")
		}
		if len(collector.events) != 1 || collector.events[0].status != "panic" {
			t.Fatalf("平台 panic 没有日志：%+v", collector.events)
		}
	}()
	_ = runtime.OpenLoginPage(context.Background(), nil, model.Config{})
}
