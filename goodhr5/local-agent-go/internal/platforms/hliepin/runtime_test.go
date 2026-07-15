// Package hliepin 测试猎聘猎头端平台运行时逻辑。
package hliepin

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
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

// searchExecutor 模拟关键词、快捷搜索和发布职位页面操作。
type searchExecutor struct {
	paths           []string
	payloads        []map[string]any
	findItems       map[string][]any
	errors          map[string]error
	clickTextErrors map[string]error
	scrollActions   []string
	scrollCalls     int
}

// Post 记录猎聘搜索流程调用，并按选择器返回模拟页面元素。
func (e *searchExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.paths = append(e.paths, path)
	value, _ := payload.(map[string]any)
	e.payloads = append(e.payloads, value)
	if path == "/api/v1/page/click-by-text" {
		if err := e.clickTextErrors[stringFromMap(value, "text")]; err != nil {
			return nil, err
		}
	}
	if err := e.errors[path]; err != nil {
		return nil, err
	}
	if path == "/api/v1/page/scroll-or-click-next" {
		action := "end"
		if e.scrollCalls < len(e.scrollActions) {
			action = e.scrollActions[e.scrollCalls]
		}
		e.scrollCalls++
		return map[string]any{"data": map[string]any{"action": action}}, nil
	}
	if path == "/api/v1/page/find-elements" {
		element := mapFromAny(value["element"])
		return map[string]any{"data": map[string]any{"items": e.findItems[stringFromMap(element, "selector")]}}, nil
	}
	return map[string]any{"data": map[string]any{"ok": true}}, nil
}

// Log 忽略测试中的任务日志。
func (e *searchExecutor) Log(string, string) {}

// Delay 跳过测试中的真实等待。
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
			map[string]any{"index": 0, "ref": "candidate-1", "text": "在线\n陈**\n28岁\n工作5年\n成都 Java\n立即沟通", "fields": map[string]any{"platform_candidate_id": "resume-001"}},
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
	if got := stringFromMap(candidates[0], "id"); got == "" {
		t.Fatal("猎聘候选人应在进入主流程前生成去重 ID")
	}
	if got := stringFromMap(candidates[0], "id"); got != "hliepin_resume-001" {
		t.Fatalf("candidate id = %q", got)
	}
	if exec.findCalls != 2 {
		t.Fatalf("find calls = %d, want 2", exec.findCalls)
	}
}

// TestCandidateFingerprintUsesStableResumeID 验证查看标记和活跃状态变化后仍按猎聘简历 ID 去重。
// t 为测试对象。
func TestCandidateFingerprintUsesStableResumeID(t *testing.T) {
	runtime := NewRuntime()
	before := platformcore.Candidate{
		"platform_candidate_id": "7c75e8fb7974Ffa811f22fff9",
		"candidate_name":        "王**",
		"raw_text":              "今天活跃\n王**\n34岁\n成都PHP\n立即沟通",
	}
	after := platformcore.Candidate{
		"platform_candidate_id": "7c75e8fb7974Ffa811f22fff9",
		"candidate_name":        "王**\n阅",
		"raw_text":              "3天内活跃\n王**\n阅\n34岁\n成都PHP\n立即沟通",
	}
	if first, second := runtime.CandidateFingerprint(before), runtime.CandidateFingerprint(after); first != second {
		t.Fatalf("fingerprint changed: %q != %q", first, second)
	}
}

// TestHLiepinCandidateFieldRequestsReadsResumeIDAttribute 验证候选人提取会读取行内稳定简历 ID。
// t 为测试对象。
func TestHLiepinCandidateFieldRequestsReadsResumeIDAttribute(t *testing.T) {
	fields := hliepinCandidateFieldRequests(nil)
	if len(fields) == 0 {
		t.Fatal("candidate fields should include platform candidate id")
	}
	config := mapFromAny(fields[len(fields)-1]["platform_candidate_id"])
	if stringFromMap(config, "selector") != "input[name='res_id_encode']" || stringFromMap(config, "attribute") != "value" {
		t.Fatalf("platform candidate id config = %#v", config)
	}
}

// TestCandidateFingerprintFallbackIgnoresTransientText 验证旧页面没有简历 ID 时也会忽略瞬时状态文本。
// t 为测试对象。
func TestCandidateFingerprintFallbackIgnoresTransientText(t *testing.T) {
	runtime := NewRuntime()
	before := platformcore.Candidate{"candidate_name": "王**", "raw_text": "今天活跃\n王**\n34岁\n成都PHP\n立即沟通"}
	after := platformcore.Candidate{"candidate_name": "王**", "raw_text": "3天内活跃\n王**\n阅\n34岁\n成都PHP\n立即沟通"}
	if first, second := runtime.CandidateFingerprint(before), runtime.CandidateFingerprint(after); first != second {
		t.Fatalf("fallback fingerprint changed: %q != %q", first, second)
	}
}

// TestScrollCandidateListClicksNextPageDirectly 验证猎聘处理完整页候选人后直接定位并点击下一页。
// t 为测试对象。
func TestScrollCandidateListClicksNextPageDirectly(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	if err := runtime.ScrollCandidateList(context.Background(), exec, nil, 735); err != nil {
		t.Fatal(err)
	}
	if len(exec.payloads) != 1 || exec.paths[0] != "/api/v1/page/scroll-or-click-next" {
		t.Fatalf("paths=%#v payloads=%#v", exec.paths, exec.payloads)
	}
	if direct, ok := exec.payloads[0]["click_next_directly"].(bool); !ok || !direct {
		t.Fatalf("click_next_directly = %#v", exec.payloads[0]["click_next_directly"])
	}
	if got := intFromMap(exec.payloads[0], "distance"); got != 735 {
		t.Fatalf("distance = %d", got)
	}
}

// TestPositionSelectionIsSkipped 验证猎聘跳过岗位切换和三个隐藏筛选。
// t 为测试对象。
func TestPositionSelectionIsSkipped(t *testing.T) {
	runtime := NewRuntime()
	exec := &routeExecutor{}
	if !runtime.ShouldSkipPositionSelection() {
		t.Fatal("猎聘应跳过页面岗位处理")
	}
	if err := runtime.SelectPosition(context.Background(), exec, nil, "Java开发工程师"); err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("猎聘岗位处理不应调用页面接口，paths=%#v", exec.paths)
	}
}

// TestPreparePositionSearchSelectsExactShortcutWithoutTypingKeyword 验证猎聘直接选择完整同名的快捷搜索且不再输入关键词。
// t 为测试对象。
func TestPreparePositionSearchSelectsExactShortcutWithoutTypingKeyword(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinShortcutItemSelector: {
			map[string]any{"text": "java开发"},
			map[string]any{"text": "java开发工程师初"},
			map[string]any{"text": "AI应用开发工程师初"},
		},
	}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"name":          "Java开发工程师初级",
		"common_config": map[string]any{"hliepin_shortcut_search_name": "java开发工程师初"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 9 || exec.paths[0] != "/api/v1/page/find-elements" || exec.paths[1] != "/api/v1/page/find-elements" || exec.paths[2] != "/api/v1/page/list-click-by-index" {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := intFromMap(exec.payloads[2], "index"); got != 1 {
		t.Fatalf("shortcut index = %d, want exact match index 1", got)
	}
	if got := countPath(exec.paths, "/api/v1/page/ensure-checked-by-text"); got != 3 {
		t.Fatalf("hidden filter clicks = %d, want 3", got)
	}
}

// TestPreparePositionSearchSelectsPublishedJobWhenShortcutEmpty 验证未配置新快捷搜索名时忽略旧关键词字段并匹配正在发布的职位。
// t 为测试对象。
func TestPreparePositionSearchSelectsPublishedJobWhenShortcutEmpty(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinPublishedJobSelector: {map[string]any{"text": "Java开发工程师初级"}},
	}}
	position := map[string]any{
		"name":          "Java开发工程师初级",
		"common_config": map[string]any{"hliepin_search_keyword": "Java Python"},
	}
	if err := runtime.PreparePositionSearch(context.Background(), exec, nil, position); err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 9 || exec.paths[0] != "/api/v1/page/find-elements" || exec.paths[1] != "/api/v1/page/find-elements" || exec.paths[2] != "/api/v1/page/click-by-text" {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := stringFromMap(exec.payloads[2], "text"); got != "Java开发工程师初级" {
		t.Fatalf("position text = %q", got)
	}
	if got := countPath(exec.paths, "/api/v1/page/ensure-checked-by-text"); got != 3 {
		t.Fatalf("hidden filter clicks = %d, want 3", got)
	}
}

// TestPreparePositionSearchExpandsShortcutWhenControlExists 验证快捷搜索存在展开入口时先展开再读取列表。
// t 为测试对象。
func TestPreparePositionSearchExpandsShortcutWhenControlExists(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinExpandShortcuts:      {map[string]any{"text": "展开更多条件"}},
		hliepinShortcutItemSelector: {map[string]any{"text": "Java开发工程师初"}},
	}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"name": "Java开发工程师初级", "common_config": map[string]any{"hliepin_shortcut_search_name": "Java开发工程师初"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) < 4 || exec.paths[0] != "/api/v1/page/find-elements" || exec.paths[1] != "/api/v1/page/click" || exec.paths[2] != "/api/v1/page/find-elements" || exec.paths[3] != "/api/v1/page/list-click-by-index" {
		t.Fatalf("paths = %#v", exec.paths)
	}
}

// TestScrollCandidateDetailReachesBottomWithoutMouseMove 验证猎聘详情使用直接滚轮并等待到达页面底部。
// t 为测试对象。
func TestScrollCandidateDetailReachesBottomWithoutMouseMove(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{scrollActions: []string{"end"}}
	err := runtime.ScrollCandidateDetail(context.Background(), exec, nil, map[string]any{"name": "张先生"}, 260)
	if err != nil {
		t.Fatal(err)
	}
	if exec.scrollCalls != 1 || countPath(exec.paths, "/api/v1/page/scroll-or-click-next") != 1 {
		t.Fatalf("scroll calls = %d paths=%#v", exec.scrollCalls, exec.paths)
	}
	if got := intFromMap(exec.payloads[0], "distance"); got != 360 {
		t.Fatalf("scroll distance = %d", got)
	}
	duration := intFromMap(exec.payloads[0], "scroll_to_bottom_duration_ms")
	if duration < 2000 || duration > 5000 {
		t.Fatalf("scroll duration = %d, want 2000..5000", duration)
	}
}

// TestMatchingShortcutItemRequiresExactName 验证快捷搜索不会再用截断名称或包含关系误匹配。
// t 为测试对象。
func TestMatchingShortcutItemRequiresExactName(t *testing.T) {
	items := []map[string]any{{"text": "Java开发"}, {"text": "Java开发工程师"}}
	if index, _ := matchingShortcutItem(items, "Java开发工程师高级"); index != -1 {
		t.Fatalf("partial shortcut should not match, index=%d", index)
	}
	if index, name := matchingShortcutItem(items, "Java开发工程师"); index != 1 || name != "Java开发工程师" {
		t.Fatalf("exact shortcut match = %d %q", index, name)
	}
}

// TestGreetCandidateSelectsPositionAndPressesEscape 验证猎聘开聊选择岗位、立即开聊并固定按 Esc。
// t 为测试对象。
func TestGreetCandidateSelectsPositionAndPressesEscape(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "Java开发工程师高级"
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinGreetJobOptionSelector: {
			map[string]any{"fields": map[string]any{"position_name": "AI应用开发工程师初..."}},
			map[string]any{"fields": map[string]any{"position_name": "Java开发工程师高..."}},
		},
	}}
	err := runtime.GreetCandidate(context.Background(), exec, nil, map[string]any{
		"card_index": 2, "card_item": map[string]any{"selector": "tbody tr"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/page/list-click-by-index", "/api/v1/page/click-by-text", "/api/v1/page/find-elements", "/api/v1/page/list-click-by-index", "/api/v1/page/click-by-text", "/api/v1/page/press-key", "/api/v1/page/press-key"}
	if fmt.Sprint(exec.paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := intFromMap(exec.payloads[3], "index"); got != 1 {
		t.Fatalf("position index = %d, want 1", got)
	}
	if got := countPath(exec.paths, "/api/v1/page/press-key"); got != 2 {
		t.Fatalf("escape presses = %d, want 2", got)
	}
}

// TestGreetCandidateSkipsPositionForPublishedJobMode 验证岗位模式已选职位时直接立即开聊并按 Esc。
// t 为测试对象。
func TestGreetCandidateSkipsPositionForPublishedJobMode(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "PHP程序员"
	runtime.greetJobSelected = true
	exec := &searchExecutor{}
	err := runtime.GreetCandidate(context.Background(), exec, nil, map[string]any{
		"card_index": 0, "card_item": map[string]any{"selector": "tbody tr"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/page/list-click-by-index", "/api/v1/page/click-by-text", "/api/v1/page/press-key", "/api/v1/page/press-key"}
	if fmt.Sprint(exec.paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := stringFromMap(exec.payloads[1], "text"); got != "立即开聊" {
		t.Fatalf("button text = %q", got)
	}
}

// TestMatchingGreetJobItemPrefersExactMatch 验证完整职位名优先于省略号包含匹配。
// t 为测试对象。
func TestMatchingGreetJobItemPrefersExactMatch(t *testing.T) {
	items := []map[string]any{
		{"fields": map[string]any{"position_name": "PHP程序..."}},
		{"fields": map[string]any{"position_name": "PHP程序员"}},
	}
	index, name := matchingGreetJobItem(items, "PHP程序员")
	if index != 1 || name != "PHP程序员" {
		t.Fatalf("match = %d %q", index, name)
	}
}

// countPath 统计模拟执行器中指定 Worker 路由的调用次数。
func countPath(paths []string, target string) int {
	count := 0
	for _, path := range paths {
		if path == target {
			count++
		}
	}
	return count
}

// TestPreparePositionSearchStopsWhenShortcutMissing 验证找不到快捷搜索时返回明确停止错误。
// t 为测试对象。
func TestPreparePositionSearchStopsWhenShortcutMissing(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{hliepinShortcutItemSelector: {}}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"name": "Java开发工程师初级", "common_config": map[string]any{"hliepin_shortcut_search_name": "Java开发工程师初级"},
	})
	if err == nil || !strings.Contains(err.Error(), "未找到猎聘快捷搜索列表") || !strings.Contains(err.Error(), "任务已停止") {
		t.Fatalf("error = %v", err)
	}
}

// TestPreparePositionSearchStopsWhenPublishedJobMissing 验证找不到对应发布职位时返回明确停止错误。
// t 为测试对象。
func TestPreparePositionSearchStopsWhenPublishedJobMissing(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinPublishedJobSelector: {map[string]any{"text": "PHP程序员"}},
	}, clickTextErrors: map[string]error{"Java开发工程师初级": fmt.Errorf("未找到文字元素")}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{"name": "Java开发工程师初级"})
	if err == nil || !strings.Contains(err.Error(), "正在发布的职位中未找到任务岗位") || !strings.Contains(err.Error(), "任务已停止") {
		t.Fatalf("error = %v", err)
	}
}
