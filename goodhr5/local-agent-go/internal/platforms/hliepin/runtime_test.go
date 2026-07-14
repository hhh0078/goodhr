// Package hliepin 测试猎聘猎头端平台运行时逻辑。
package hliepin

import (
	"context"
	"fmt"
	"testing"

	"goodhr5/local-agent-go/internal/cloudapi"
)

// testExecutor 模拟平台运行时调用浏览器 Worker。
type testExecutor struct {
	lastPath string
}

// routeExecutor 按路由返回测试数据并记录调用。
type routeExecutor struct {
	paths     []string
	findCalls int
}

type searchExecutor struct {
	paths    []string
	payloads []map[string]any
}

func (e *searchExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.paths = append(e.paths, path)
	value, _ := payload.(map[string]any)
	e.payloads = append(e.payloads, value)
	return map[string]any{"data": map[string]any{"ok": true}}, nil
}

func (e *searchExecutor) Log(string, string)                           {}
func (e *searchExecutor) Delay(context.Context, string, float64) error { return nil }

func (e *routeExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.paths = append(e.paths, path)
	switch path {
	case "/api/v1/page/find-elements":
		e.findCalls++
		if e.findCalls == 1 {
			return map[string]any{"data": map[string]any{"items": []any{}}}, nil
		}
		return map[string]any{"data": map[string]any{"items": []any{
			map[string]any{"index": 0, "ref": "candidate-1", "text": "在线\n陈**\n28岁\n工作5年\n成都 Java\n立即沟通", "fields": map[string]any{}},
			map[string]any{"index": 1, "text": "1\n2\n3", "fields": map[string]any{}},
		}}}, nil
	case "/api/v1/page/click-by-text", "/api/v1/page/ensure-checked-by-text", "/api/v1/page/scroll":
		return map[string]any{"data": map[string]any{"ok": true, "payload": payload}}, nil
	default:
		return nil, fmt.Errorf("未预期路由：%s", path)
	}
}

func (e *routeExecutor) Log(string, string) {}

func (e *routeExecutor) Delay(context.Context, string, float64) error { return nil }

// Post 记录调用路径并返回页面列表。
// ctx 为运行上下文，path 为 Worker 路由，payload 为请求参数。
func (e *testExecutor) Post(_ context.Context, path string, _ any) (map[string]any, error) {
	e.lastPath = path
	return map[string]any{
		"data": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://h.liepin.com/search/getConditionItem", "is_default": true},
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
				map[string]any{"url": "https://h.liepin.com/search/getConditionItem", "entry": true},
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

// TestListVisibleCandidatesFallsBackToTableRows 验证旧选择器失效时回退到表格行。
func TestListVisibleCandidatesFallsBackToTableRows(t *testing.T) {
	runtime := NewRuntime()
	exec := &routeExecutor{}
	candidates, err := runtime.ListVisibleCandidates(context.Background(), exec, cloudapi.PlatformConfig{
		"card": map[string]any{"item": map[string]any{"selector": ".old-card"}},
	}, 60)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if got := stringFromMap(candidates[0], "candidate_name"); got != "陈**" {
		t.Fatalf("candidate_name = %q", got)
	}
	if exec.findCalls != 2 {
		t.Fatalf("find calls = %d, want 2", exec.findCalls)
	}
}

// TestSelectPositionUsesPublishedJobsAndHiddenFilters 验证猎聘切岗不使用输入框。
func TestSelectPositionUsesPublishedJobsAndHiddenFilters(t *testing.T) {
	runtime := NewRuntime()
	exec := &routeExecutor{}
	if err := runtime.SelectPosition(context.Background(), exec, nil, "Java开发工程师"); err != nil {
		t.Fatal(err)
	}
	if got := runtime.currentPosition; got != "Java开发工程师" {
		t.Fatalf("current position = %q", got)
	}
	clickCalls, checkedCalls := 0, 0
	for _, path := range exec.paths {
		if path == "/api/v1/page/open" {
			t.Fatal("选择岗位不应再打开慢速职位管理页")
		}
		switch path {
		case "/api/v1/page/click-by-text":
			clickCalls++
		case "/api/v1/page/ensure-checked-by-text":
			checkedCalls++
		}
	}
	if clickCalls != 2 || checkedCalls != 3 {
		t.Fatalf("click calls = %d, checked calls = %d", clickCalls, checkedCalls)
	}
}

func TestPreparePositionSearchTypesKeywordThenClicksSearch(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"common_config": map[string]any{"hliepin_search_keyword": "AI 应用开发 Python"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 2 || exec.paths[0] != "/api/v1/page/type" || exec.paths[1] != "/api/v1/page/click" {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := stringFromMap(exec.payloads[0], "text"); got != "AI 应用开发 Python" {
		t.Fatalf("typed keyword = %q", got)
	}
	button := mapFromAny(exec.payloads[1]["element"])
	if got := stringFromMap(button, "selector"); got != ".search-auto-complete-box button.search-btn" {
		t.Fatalf("search button selector = %q", got)
	}
}

func TestPreparePositionSearchSkipsEmptyKeyword(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	if err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("paths = %#v, want no browser operation", exec.paths)
	}
}
