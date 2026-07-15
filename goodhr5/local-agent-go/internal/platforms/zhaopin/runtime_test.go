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

// positionCall 记录一次智联职位切换 Worker 调用。
type positionCall struct {
	path    string
	payload map[string]any
}

// positionExecutor 模拟智联职位选择弹层的搜索和点击结果。
type positionExecutor struct {
	calls     []positionCall
	findCalls int
}

// Post 返回智联职位搜索结果，并记录第一条结果点击。
// ctx 为运行上下文，path 为 Worker 路由，payload 为请求参数。
func (e *positionExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	data, _ := payload.(map[string]any)
	e.calls = append(e.calls, positionCall{path: path, payload: data})
	if path != "/api/v1/page/find-elements" {
		return map[string]any{"data": map[string]any{}}, nil
	}
	e.findCalls++
	if e.findCalls == 1 {
		return map[string]any{"data": map[string]any{"items": []any{
			map[string]any{
				"element_ref": "job-ref-1",
				"fields":      map[string]any{"position_name": "线下运营销售"},
			},
		}}}, nil
	}
	return map[string]any{"data": map[string]any{"items": []any{}}}, nil
}

// Log 模拟智联职位切换日志。
func (e *positionExecutor) Log(string, string) {}

// Delay 模拟智联职位切换等待。
// ctx 为运行上下文，label 为等待说明，seconds 为等待秒数。
func (e *positionExecutor) Delay(context.Context, string, float64) error { return nil }

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

// TestShouldSelectPositionDirectly 验证智联跳过当前岗位读取并直接切换。
// t 为测试对象。
func TestShouldSelectPositionDirectly(t *testing.T) {
	if !NewRuntime().ShouldSelectPositionDirectly() {
		t.Fatal("智联招聘应直接切换任务岗位")
	}
}

// TestSelectPositionSearchesAndClicksFirstResult 验证智联点击选择职位、搜索并点击第一条结果。
// t 为测试对象。
func TestSelectPositionSearchesAndClicksFirstResult(t *testing.T) {
	exec := &positionExecutor{}
	err := NewRuntime().SelectPosition(context.Background(), exec, nil, "线下运营销售 _ 德阳 6-11K")
	if err != nil {
		t.Fatal(err)
	}
	wantPaths := []string{
		"/api/v1/page/click",
		"/api/v1/page/type",
		"/api/v1/page/find-elements",
		"/api/v1/page/click",
		"/api/v1/page/find-elements",
	}
	if len(exec.calls) != len(wantPaths) {
		t.Fatalf("calls = %+v", exec.calls)
	}
	for index, path := range wantPaths {
		if exec.calls[index].path != path {
			t.Fatalf("call %d path = %s", index, exec.calls[index].path)
		}
	}
	if exec.calls[1].payload["text"] != "线下运营销售" {
		t.Fatalf("search text = %v", exec.calls[1].payload["text"])
	}
	if exec.calls[3].payload["element_ref"] != "job-ref-1" {
		t.Fatalf("click payload = %+v", exec.calls[3].payload)
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
		"card_index":     2,
		"element_ref":    "candidate-2",
		"candidate_name": "张女士",
		"raw_text":       "张女士 本科 三年课程顾问经验",
	}, platformcore.DetailRequest{TaskID: "task-zhaopin", Mode: "dom"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.lastPath != "/api/v1/boss/candidates/detail" {
		t.Fatalf("path = %s", exec.lastPath)
	}
	if exec.payload["platform_id"] != "zhaopin" || exec.payload["screenshot"] != false {
		t.Fatalf("payload = %+v", exec.payload)
	}
	if exec.payload["candidate_name"] != "张女士" || exec.payload["candidate_match_text"] != "张女士 本科 三年课程顾问经验" || exec.payload["require_candidate_match"] != true {
		t.Fatalf("智联详情必须携带候选人身份匹配信息，payload = %+v", exec.payload)
	}
	if exec.payload["force_scroll"] != false || exec.payload["card_scroll_attempts"] != 3 || exec.payload["require_full"] != false {
		t.Fatalf("智联详情不应持续强制滚动，payload = %+v", exec.payload)
	}
	if exec.payload["detail_ready_timeout"] != 5000 {
		t.Fatalf("智联详情选择器最长等待应为5秒，payload = %+v", exec.payload)
	}
	if _, exists := exec.payload["wait_ms"]; exists {
		t.Fatalf("智联点击详情后不应再固定等待，payload = %+v", exec.payload)
	}
	if exec.payload["task_id"] != "task-zhaopin" {
		t.Fatalf("智联详情请求应携带任务 ID 以写入可见日志，payload = %+v", exec.payload)
	}
	if result.Text != "候选人详情" || result.Source != "dom" || len(result.Screenshot) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

// TestScrollCandidateDetailTargetsResumeDialog 验证 AI 分析期间只滚动智联详情弹框。
// t 为测试对象。
func TestScrollCandidateDetailTargetsResumeDialog(t *testing.T) {
	exec := &testExecutor{}
	if err := NewRuntime().ScrollCandidateDetail(context.Background(), exec, nil, nil, 260); err != nil {
		t.Fatal(err)
	}
	if exec.lastPath != "/api/v1/page/scroll" || exec.payload["distance"] != 260 {
		t.Fatalf("path=%s payload=%+v", exec.lastPath, exec.payload)
	}
	if _, exists := exec.payload["wait_ms"]; exists {
		t.Fatalf("智联单次滚动完成后不应由 Worker 追加等待：%+v", exec.payload)
	}
	if exec.payload["min_steps"] != 2 || exec.payload["max_steps"] != 4 {
		t.Fatalf("智联详情滚动应使用快速拟人鼠标移动：%+v", exec.payload)
	}
	element := mapFromAny(exec.payload["element"])
	if !strings.Contains(stringFromMap(element, "selector"), ".new-resume-detail--inner") {
		t.Fatalf("详情滚动选择器错误：%+v", element)
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
