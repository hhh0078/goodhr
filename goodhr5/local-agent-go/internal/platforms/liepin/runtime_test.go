// Package liepin 测试猎聘企业端平台运行时逻辑。
package liepin

import (
	"context"
	"testing"

	"goodhr5/local-agent-go/internal/cloudapi"
)

// testExecutor 模拟平台运行时调用浏览器 Worker。
type testExecutor struct {
	lastPath string
}

// Post 记录调用路径并返回页面列表。
// ctx 为运行上下文，path 为 Worker 路由，payload 为请求参数。
func (e *testExecutor) Post(_ context.Context, path string, _ any) (map[string]any, error) {
	e.lastPath = path
	return map[string]any{
		"data": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://lpt.liepin.com/recommend", "is_default": true},
			},
		},
	}, nil
}

// Log 模拟任务日志写入。
// level 为日志级别，message 为日志内容。
func (e *testExecutor) Log(string, string) {}

// Delay 模拟业务动作等待。
// ctx 为运行上下文，message 为等待说明，seconds 为等待秒数。
func (e *testExecutor) Delay(context.Context, string, float64) error { return nil }

// TestIsTaskEntryPageUsesPageList 验证入口页判断使用页面列表接口。
// t 为测试对象。
func TestIsTaskEntryPageUsesPageList(t *testing.T) {
	runtime := NewRuntime()
	exec := &testExecutor{}
	ok, err := runtime.IsTaskEntryPage(context.Background(), exec, cloudapi.PlatformConfig{
		"auth": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://lpt.liepin.com/recommend", "entry": true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("当前页面应命中入口页")
	}
	if exec.lastPath != "/api/v1/page/list" {
		t.Fatalf("path = %s", exec.lastPath)
	}
}
