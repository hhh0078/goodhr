// Package zhaopin 测试智联招聘平台运行时逻辑。
package zhaopin

import (
	"context"
	"strings"
	"testing"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

// testExecutor 模拟平台运行时调用浏览器 Worker。
type testExecutor struct {
	lastPath string
	payload  map[string]any
	response map[string]any
}

// Post 记录调用路径并返回页面列表。
// ctx 为运行上下文，path 为 Worker 路由，payload 为请求参数。
func (e *testExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.lastPath = path
	e.payload, _ = payload.(map[string]any)
	if e.response != nil {
		return e.response, nil
	}
	return map[string]any{
		"data": map[string]any{
			"pages": []any{
				map[string]any{"url": "https://rd6.zhaopin.com/app/recommend", "is_default": true},
			},
		},
	}, nil
}

// TestCandidateFingerprintUsesZhaopinPrefix 验证智联候选人 ID 使用平台前缀和姓名年龄。
// t 为测试对象。
func TestCandidateFingerprintUsesZhaopinPrefix(t *testing.T) {
	runtime := NewRuntime()
	got := runtime.CandidateFingerprint(platformcore.Candidate{
		"candidate_name": "张 三",
		"fields":         map[string]any{"basic_info": "男 28岁 本科"},
	})
	if got != "zhaopin_张三_28" {
		t.Fatalf("fingerprint = %s", got)
	}
}

// TestFetchCandidateDetailUsesDOM 验证智联详情复用稳定定位链路且不请求截图。
// t 为测试对象。
func TestFetchCandidateDetailUsesDOM(t *testing.T) {
	runtime := NewRuntime()
	exec := &testExecutor{response: map[string]any{
		"data": map[string]any{"detail_text": "候选人详情"},
	}}
	result, err := runtime.FetchCandidateDetail(context.Background(), exec, cloudapi.PlatformConfig{"id": "zhaopin"}, platformcore.Candidate{
		"card_index":  2,
		"element_ref": "candidate-2",
	}, platformcore.DetailRequest{Mode: "dom"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.lastPath != "/api/v1/boss/candidates/detail" {
		t.Fatalf("path = %s", exec.lastPath)
	}
	if exec.payload["platform_id"] != "zhaopin" || exec.payload["screenshot"] != false {
		t.Fatalf("payload = %+v", exec.payload)
	}
	if result.Text != "候选人详情" || result.Source != "dom" || len(result.Screenshot) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

// TestFetchCandidateDetailRejectsScreenshotMode 验证智联拒绝 OCR 和截图详情模式。
// t 为测试对象。
func TestFetchCandidateDetailRejectsScreenshotMode(t *testing.T) {
	runtime := NewRuntime()
	exec := &testExecutor{}
	_, err := runtime.FetchCandidateDetail(context.Background(), exec, nil, nil, platformcore.DetailRequest{Mode: "ocr"})
	if err == nil || !strings.Contains(err.Error(), "只支持 DOM") {
		t.Fatalf("err = %v", err)
	}
	if exec.lastPath != "" {
		t.Fatalf("不应调用 Worker，path = %s", exec.lastPath)
	}
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
				map[string]any{"url": "https://rd6.zhaopin.com/app/recommend", "entry": true},
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
