// Package hliepin 测试猎聘猎头端平台运行时逻辑。
package hliepin

import (
	"context"
	"errors"
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
	payloads  []map[string]any
	findCalls int
}

// searchExecutor 模拟关键词、快捷搜索和发布职位页面操作。
type searchExecutor struct {
	paths                []string
	payloads             []map[string]any
	findItems            map[string][]any
	findItemSequences    map[string][][]any
	findSequenceCalls    map[string]int
	errors               map[string]error
	clickTextErrors      map[string]error
	clickTextFailures    map[string]int
	stableFailures       map[string]int
	greetModalOpen       bool
	chatModalOpen        bool
	candidateDrawerOpen  bool
	suppressContinueChat bool
	scrollActions        []string
	scrollCalls          int
	delays               []float64
	extractText          string
}

// hliepinStableTestCandidate 创建带稳定简历 ID 的猎聘候选人测试数据。
func hliepinStableTestCandidate(index int) platformcore.Candidate {
	return platformcore.Candidate{
		"card_index":            index,
		"card_item":             map[string]any{"selector": "tbody tr"},
		"platform_candidate_id": fmt.Sprintf("test-candidate-%d", index),
		"candidate_name":        "王**",
	}
}

// Post 记录猎聘搜索流程调用，并按选择器返回模拟页面元素。
func (e *searchExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.paths = append(e.paths, path)
	value, _ := payload.(map[string]any)
	e.payloads = append(e.payloads, value)
	if path == hliepinStableClickPath {
		target := stringFromMap(value, "target_selector")
		if e.stableFailures[target] > 0 {
			e.stableFailures[target]--
			return nil, errors.New("模拟猎聘稳定目标尚未就绪")
		}
		if target == hliepinCandidateButtonTarget && stringFromMap(value, "expected_text") == "立即沟通" {
			e.greetModalOpen = true
		}
		if target == hliepinCandidateButtonTarget && stringFromMap(value, "expected_text") == "继续沟通" {
			e.chatModalOpen = !e.suppressContinueChat
			e.candidateDrawerOpen = true
		}
		if target == hliepinChatCloseSelector {
			e.chatModalOpen = false
		}
		if target == hliepinCandidateListClose {
			e.candidateDrawerOpen = false
		}
		if target == hliepinGreetSubmitTarget || target == hliepinGreetWithoutJobTarget {
			e.greetModalOpen = false
		}
	}
	if path == "/api/v1/page/click-by-text" {
		text := stringFromMap(value, "text")
		if e.clickTextFailures[text] > 0 {
			e.clickTextFailures[text]--
			return nil, errors.New("模拟猎聘开聊控件尚未渲染")
		}
		if err := e.clickTextErrors[stringFromMap(value, "text")]; err != nil {
			return nil, err
		}
		if text == "立即开聊" || text == "不选职位" || text == "不选择职位" {
			e.greetModalOpen = false
		}
	}
	if path == "/api/v1/page/list-click-by-index" {
		item := mapFromAny(value["item"])
		if stringFromMap(item, "selector") == "tbody tr" {
			e.greetModalOpen = true
		}
	}
	if path == "/api/v1/page/press-key" && stringFromMap(value, "key") == "Escape" {
		e.greetModalOpen = false
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
	if path == "/api/v1/page/extract-text" {
		return map[string]any{"data": map[string]any{"text": e.extractText}}, nil
	}
	if path == "/api/v1/page/find-elements" {
		element := mapFromAny(value["element"])
		selector := stringFromMap(element, "selector")
		if selector == hliepinGreetModalSelector {
			items := []any{}
			if e.greetModalOpen {
				items = []any{map[string]any{"text": "请选择职位开聊\n请选择开聊的职位"}}
			}
			return map[string]any{"data": map[string]any{"items": items}}, nil
		}
		if sequences := e.findItemSequences[selector]; len(sequences) > 0 {
			if e.findSequenceCalls == nil {
				e.findSequenceCalls = map[string]int{}
			}
			index := e.findSequenceCalls[selector]
			e.findSequenceCalls[selector] = index + 1
			if index >= len(sequences) {
				index = len(sequences) - 1
			}
			return map[string]any{"data": map[string]any{"items": sequences[index]}}, nil
		}
		if items, configured := e.findItems[selector]; configured {
			return map[string]any{"data": map[string]any{"items": items}}, nil
		}
		if selector == hliepinChatModalParent && e.chatModalOpen {
			return map[string]any{"data": map[string]any{"items": []any{map[string]any{
				"text": "王先生", "fields": map[string]any{"candidate_name": "王先生"},
			}}}}, nil
		}
		if selector == hliepinCandidateDrawerParent && e.candidateDrawerOpen {
			return map[string]any{"data": map[string]any{"items": []any{map[string]any{"text": "我的沟通"}}}}, nil
		}
		for _, actionSelector := range []string{hliepinRequestPhoneSelector, hliepinRequestWechatSelector, hliepinRequestResumeSelector} {
			if selector == hliepinChatModalParent+" "+actionSelector {
				return map[string]any{"data": map[string]any{"items": []any{map[string]any{"text": "索要"}}}}, nil
			}
		}
		return map[string]any{"data": map[string]any{"items": e.findItems[selector]}}, nil
	}
	return map[string]any{"data": map[string]any{"ok": true}}, nil
}

// Log 忽略测试中的岗位运行日志。
func (e *searchExecutor) Log(string, string) {}

// Delay 记录猎聘页面操作等待时长并跳过真实等待。
func (e *searchExecutor) Delay(_ context.Context, _ string, seconds float64) error {
	e.delays = append(e.delays, seconds)
	return nil
}

func (e *routeExecutor) Post(_ context.Context, path string, payload any) (map[string]any, error) {
	e.paths = append(e.paths, path)
	value, _ := payload.(map[string]any)
	e.payloads = append(e.payloads, value)
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

// Log 模拟岗位运行日志写入。
// level 为日志级别，message 为日志内容。
func (e *testExecutor) Log(string, string) {}

// Delay 模拟业务动作等待。
// ctx 为运行上下文，message 为等待说明，seconds 为等待秒数。
func (e *testExecutor) Delay(context.Context, string, float64) error { return nil }

// TestIsPositionEntryPageUsesPageList 验证入口页判断使用页面列表接口。
// t 为测试对象。
func TestIsPositionEntryPageUsesPageList(t *testing.T) {
	runtime := NewRuntime()
	exec := &testExecutor{}
	ok, err := runtime.IsPositionEntryPage(context.Background(), exec, cloudapi.PlatformConfig{
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
	if len(exec.payloads) < 2 {
		t.Fatalf("find payloads = %d, want at least 2", len(exec.payloads))
	}
	if got := intFromMap(exec.payloads[1], "timeout"); got != hliepinReloadWaitTimeoutMS {
		t.Fatalf("candidate wait timeout = %d, want %d", got, hliepinReloadWaitTimeoutMS)
	}
	if got := intFromMap(exec.payloads[1], "poll_interval_ms"); got != hliepinReloadPollIntervalMS {
		t.Fatalf("candidate poll interval = %d, want %d", got, hliepinReloadPollIntervalMS)
	}
	if got := stringFromMap(exec.payloads[1], "required_text"); got != "立即沟通" {
		t.Fatalf("candidate required text = %q", got)
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

// TestPreparePositionSearchKeepsManualShortcutFilters 验证配置过快捷搜索时也不会自动点击页面搜索项或隐藏条件。
// t 为测试对象。
func TestPreparePositionSearchKeepsManualShortcutFilters(t *testing.T) {
	runtime := NewRuntime()
	runtime.shouldSelectGreetJob = true
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
	if len(exec.paths) != 0 {
		t.Fatalf("manual filtering should not call page APIs, paths=%#v", exec.paths)
	}
	if runtime.currentPosition != "Java开发工程师初级" {
		t.Fatalf("current position = %q", runtime.currentPosition)
	}
	if runtime.shouldSelectGreetJob {
		t.Fatal("configured shortcut mode should keep the existing greet-modal behavior")
	}
}

// TestPreparePositionSearchKeepsManualPublishedJobFilters 验证未配置快捷搜索时也不会自动选择发布职位或隐藏条件。
// t 为测试对象。
func TestPreparePositionSearchKeepsManualPublishedJobFilters(t *testing.T) {
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
	if len(exec.paths) != 0 {
		t.Fatalf("manual filtering should not call page APIs, paths=%#v", exec.paths)
	}
	if runtime.currentPosition != "Java开发工程师初级" {
		t.Fatalf("current position = %q", runtime.currentPosition)
	}
	if !runtime.shouldSelectGreetJob {
		t.Fatal("mode without a configured shortcut should still select the job in the greet modal")
	}
}

// TestPreparePositionSearchDoesNotApplyConfiguredHiddenFilters 验证三个旧隐藏条件无论如何配置都不再自动勾选。
// t 为测试对象。
func TestPreparePositionSearchDoesNotApplyConfiguredHiddenFilters(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"name": "后端工程师",
		"common_config": map[string]any{
			"hliepin_shortcut_search_name":  "后端工程师",
			"hliepin_hide_viewed":           false,
			"hliepin_hide_contacted":        true,
			"hliepin_hide_contact_obtained": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("configured hidden filters should remain manual, paths=%#v", exec.paths)
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

// TestFetchCandidateDetailAddsHLiepinScrollDiagnostics 验证猎聘详情点击携带平台、操作和候选人诊断上下文。
// t 为测试对象。
func TestFetchCandidateDetailAddsHLiepinScrollDiagnostics(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{extractText: "候选人详情内容"}
	candidate := hliepinStableTestCandidate(0)

	result, err := runtime.FetchCandidateDetail(
		context.Background(),
		exec,
		nil,
		candidate,
		platformcore.DetailRequest{Mode: "dom"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "候选人详情内容" {
		t.Fatalf("detail text = %q", result.Text)
	}
	if len(exec.paths) < 1 || exec.paths[0] != "/api/v1/page/list-click-by-index" {
		t.Fatalf("paths = %#v", exec.paths)
	}
	payload := exec.payloads[0]
	if got := stringFromMap(payload, "diagnostic_platform"); got != "hliepin" {
		t.Fatalf("diagnostic platform = %q", got)
	}
	if got := stringFromMap(payload, "diagnostic_platform_name"); got != "猎聘" {
		t.Fatalf("diagnostic platform name = %q", got)
	}
	if got := stringFromMap(payload, "diagnostic_action"); got != "读取候选人详情" {
		t.Fatalf("diagnostic action = %q", got)
	}
	if got := stringFromMap(payload, "diagnostic_candidate_name"); got != "王**" {
		t.Fatalf("diagnostic candidate name = %q", got)
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

// TestMatchingPublishedJobItemPrefersExactName 验证完整职位名优先于列表中更靠前的截断职位名。
func TestMatchingPublishedJobItemPrefersExactName(t *testing.T) {
	items := []map[string]any{{"text": "AI应用开发…"}, {"text": "AI应用开发初级工程师"}}
	index, name := matchingPublishedJobItem(items, "AI应用开发初级工程师")
	if index != 1 || name != "AI应用开发初级工程师" {
		t.Fatalf("match = %d %q, want exact item", index, name)
	}
}

// TestMatchingPublishedJobItemUsesFirstSixCharacters 验证发布职位去掉中英文省略号后按前六个字匹配。
func TestMatchingPublishedJobItemUsesFirstSixCharacters(t *testing.T) {
	items := []map[string]any{{"text": "Java开发工..."}, {"text": "AI应用开发…"}}
	index, name := matchingPublishedJobItem(items, "AI应用开发初级工程师")
	if index != 1 || name != "AI应用开发…" {
		t.Fatalf("match = %d %q, want truncated item", index, name)
	}
}

// TestMatchingPublishedJobItemChoosesFirstDuplicatePrefix 验证多个截断职位前六字相同时选择列表中的第一个。
func TestMatchingPublishedJobItemChoosesFirstDuplicatePrefix(t *testing.T) {
	items := []map[string]any{{"text": "AI应用开发…"}, {"text": "AI应用开发..."}}
	index, _ := matchingPublishedJobItem(items, "AI应用开发初级工程师")
	if index != 0 {
		t.Fatalf("match index = %d, want first duplicate", index)
	}
}

// TestGreetCandidateSelectsPositionAndPressesEscape 验证猎聘开聊选择岗位、立即开聊并固定按 Esc。
// t 为测试对象。
func TestGreetCandidateSelectsPositionAndPressesEscape(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "Java开发工程师高级"
	runtime.shouldSelectGreetJob = true
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinGreetJobOptionSelector: {
			map[string]any{"fields": map[string]any{"position_name": "AI应用开发工程师初..."}},
			map[string]any{"fields": map[string]any{"position_name": "Java开发工程师高..."}},
		},
	}}
	err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(2))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/page/find-elements", "/api/v1/page/find-elements", "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/find-elements", hliepinStableClickPath, hliepinStableClickPath, "/api/v1/page/press-key", "/api/v1/page/press-key"}
	if fmt.Sprint(exec.paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := intFromMap(exec.payloads[7], "target_index"); got != 1 {
		t.Fatalf("position index = %d, want 1", got)
	}
	if got := stringFromMap(exec.payloads[7], "parent_selector"); got != hliepinGreetDropdownParent {
		t.Fatalf("position parent = %q, want %q", got, hliepinGreetDropdownParent)
	}
	if got := stringFromMap(exec.payloads[7], "target_selector"); got != hliepinGreetJobOptionTarget {
		t.Fatalf("position target = %q, want %q", got, hliepinGreetJobOptionTarget)
	}
	if got := stringFromMap(exec.payloads[7], "nested_selector"); got != hliepinGreetJobOptionNested {
		t.Fatalf("position nested target = %q, want %q", got, hliepinGreetJobOptionNested)
	}
	if got := stringFromMap(exec.payloads[3], "parent_selector"); got != "tbody tr.r-test-candidate-2" {
		t.Fatalf("candidate parent = %q", got)
	}
	if got := stringFromMap(exec.payloads[3], "target_selector"); got != hliepinCandidateButtonTarget {
		t.Fatalf("candidate target = %q", got)
	}
	if got := countPath(exec.paths, "/api/v1/page/press-key"); got != 2 {
		t.Fatalf("escape presses = %d, want 2", got)
	}
}

// TestGreetCandidateUsesNoPositionForShortcutMode 验证快捷搜索模式直接点击不选择职位开聊。
// t 为测试对象。
func TestGreetCandidateUsesNoPositionForShortcutMode(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "PHP程序员"
	runtime.shouldSelectGreetJob = false
	exec := &searchExecutor{}
	err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(0))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/page/find-elements", "/api/v1/page/find-elements", "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/press-key", "/api/v1/page/press-key"}
	if fmt.Sprint(exec.paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := stringFromMap(exec.payloads[5], "expected_text"); got != "不选择职位开聊" {
		t.Fatalf("button text = %q", got)
	}
}

// TestGreetCandidateFallsBackToChatWithoutPosition 验证开聊职位未匹配时会点击不选职位继续开聊。
// t 为测试对象。
func TestGreetCandidateFallsBackToChatWithoutPosition(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "不存在的岗位"
	runtime.shouldSelectGreetJob = true
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinGreetJobOptionSelector: {
			map[string]any{"fields": map[string]any{"position_name": "Java开发工程师"}},
		},
	}}
	err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(1))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/api/v1/page/find-elements", "/api/v1/page/find-elements", "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/find-elements", hliepinStableClickPath, "/api/v1/page/press-key", "/api/v1/page/press-key"}
	if fmt.Sprint(exec.paths) != fmt.Sprint(want) {
		t.Fatalf("paths = %#v", exec.paths)
	}
	if got := stringFromMap(exec.payloads[7], "expected_text"); got != "不选择职位开聊" {
		t.Fatalf("fallback button text = %q", got)
	}
	if exact, _ := exec.payloads[7]["exact_text"].(bool); !exact {
		t.Fatal("fallback button should use exact text match")
	}
}

// TestGreetCandidateClearsStalePanelsBeforeImmediateChat 验证猎聘会先关闭遗留开聊弹框、聊天框和候选人列表，再点击当前候选人。
func TestGreetCandidateClearsStalePanelsBeforeImmediateChat(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "PHP程序员"
	runtime.shouldSelectGreetJob = false
	exec := &searchExecutor{greetModalOpen: true, chatModalOpen: true, candidateDrawerOpen: true}
	if err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(0)); err != nil {
		t.Fatal(err)
	}
	stableTargets := make([]string, 0)
	for index, path := range exec.paths {
		if path == hliepinStableClickPath {
			stableTargets = append(stableTargets, stringFromMap(exec.payloads[index], "target_selector"))
		}
	}
	wantPrefix := []string{hliepinChatCloseSelector, hliepinCandidateListClose, hliepinCandidateButtonTarget}
	if len(stableTargets) < len(wantPrefix) || fmt.Sprint(stableTargets[:len(wantPrefix)]) != fmt.Sprint(wantPrefix) {
		t.Fatalf("stable targets = %#v, want cleanup before candidate click %#v", stableTargets, wantPrefix)
	}
	if exec.chatModalOpen || exec.candidateDrawerOpen {
		t.Fatalf("stale panels remain: chat=%v drawer=%v", exec.chatModalOpen, exec.candidateDrawerOpen)
	}
}

// TestGreetCandidateStopsWhenStalePanelCannotClose 验证猎聘遗留弹层关闭失败时不会继续点击当前候选人。
func TestGreetCandidateStopsWhenStalePanelCannotClose(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "PHP程序员"
	runtime.shouldSelectGreetJob = false
	exec := &searchExecutor{
		chatModalOpen:  true,
		stableFailures: map[string]int{hliepinChatCloseSelector: 1},
	}
	err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(0))
	if err == nil || !strings.Contains(err.Error(), "处理前清理弹层失败") {
		t.Fatalf("error = %v, want stale panel cleanup failure", err)
	}
	for index, path := range exec.paths {
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinCandidateButtonTarget {
			t.Fatal("candidate should not be clicked after stale panel cleanup failure")
		}
	}
}

// TestGreetCandidatePreservesChatForCandidateInfo 验证本候选人随后需要索要信息时不发送 Esc 关闭自动打开的聊天框。
func TestGreetCandidatePreservesChatForCandidateInfo(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "不存在的岗位"
	runtime.shouldSelectGreetJob = true
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinGreetJobOptionSelector: {
			map[string]any{"fields": map[string]any{"position_name": "Java开发工程师"}},
		},
	}}
	candidate := hliepinStableTestCandidate(1)
	candidate["_candidate_info_after_greet"] = true
	if err := runtime.GreetCandidate(context.Background(), exec, nil, candidate); err != nil {
		t.Fatal(err)
	}
	if got := countPath(exec.paths, "/api/v1/page/press-key"); got != 0 {
		t.Fatalf("escape presses = %d, want 0 when candidate info follows", got)
	}
	fallbackClicks := 0
	for index, path := range exec.paths {
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinGreetWithoutJobTarget {
			fallbackClicks++
		}
	}
	if fallbackClicks != 1 {
		t.Fatalf("fallback clicks = %d, want 1", fallbackClicks)
	}
}

// TestGreetCandidateUsesScopedWithoutPositionButton 验证未匹配职位时只点击开聊弹框内唯一的不选择职位按钮。
// t 为测试对象。
func TestGreetCandidateUsesScopedWithoutPositionButton(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "不存在的岗位"
	exec := &searchExecutor{findItems: map[string][]any{hliepinGreetJobOptionSelector: {}}}
	if err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(1)); err != nil {
		t.Fatal(err)
	}
	foundScopedFallback := false
	for _, payload := range exec.payloads {
		if stringFromMap(payload, "parent_selector") == hliepinGreetModalParent && stringFromMap(payload, "target_selector") == hliepinGreetWithoutJobTarget {
			foundScopedFallback = true
			break
		}
	}
	if !foundScopedFallback {
		t.Fatal("scoped fallback button was not clicked")
	}
}

// TestGreetCandidateReadsJobOptionsOnce 验证猎聘职位下拉选项只查询一次，未找到时立即不选职位开聊。
// t 为测试对象。
func TestGreetCandidateReadsJobOptionsOnce(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "Java开发工程师"
	runtime.shouldSelectGreetJob = true
	exec := &searchExecutor{findItemSequences: map[string][][]any{
		hliepinGreetJobOptionSelector: {
			{},
			{map[string]any{"fields": map[string]any{"position_name": "Java开发工程师"}}},
		},
	}}
	if err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(0)); err != nil {
		t.Fatal(err)
	}
	if got := exec.findSequenceCalls[hliepinGreetJobOptionSelector]; got != 1 {
		t.Fatalf("job option queries = %d, want 1", got)
	}
	foundFallback := false
	for _, payload := range exec.payloads {
		if stringFromMap(payload, "target_selector") == hliepinGreetWithoutJobTarget {
			foundFallback = true
			break
		}
	}
	if !foundFallback {
		t.Fatal("empty first job query should immediately click without position")
	}
}

// TestGreetCandidateDoesNotRetry 验证猎聘开聊失败后直接返回，不重新点击候选人执行第二次。
// t 为测试对象。
func TestGreetCandidateDoesNotRetry(t *testing.T) {
	runtime := NewRuntime()
	runtime.currentPosition = "Java开发工程师"
	runtime.shouldSelectGreetJob = true
	exec := &searchExecutor{
		findItems: map[string][]any{
			hliepinGreetJobOptionSelector: {map[string]any{"fields": map[string]any{"position_name": "Java开发工程师"}}},
		},
		stableFailures: map[string]int{hliepinGreetJobSelectTarget: 1},
	}
	err := runtime.GreetCandidate(context.Background(), exec, nil, hliepinStableTestCandidate(0))
	if err == nil || !strings.Contains(err.Error(), "打开猎聘开聊职位下拉框失败") {
		t.Fatalf("error = %v, want dropdown open failure", err)
	}
	candidateClicks := 0
	for index, path := range exec.paths {
		if path != hliepinStableClickPath {
			continue
		}
		if stringFromMap(exec.payloads[index], "target_selector") == hliepinCandidateButtonTarget && stringFromMap(exec.payloads[index], "expected_text") == "立即沟通" {
			candidateClicks++
		}
	}
	if candidateClicks != 1 {
		t.Fatalf("candidate clicks = %d, want 1 attempt", candidateClicks)
	}
	if got := countPath(exec.paths, "/api/v1/page/press-key"); got != 0 {
		t.Fatalf("escape presses = %d, want no retry cleanup", got)
	}
}

// TestRequestCandidateInfoUsesVerifiedChatSelectors 验证猎聘按勾选项索要信息、发送问候语并依次关闭两个弹层。
func TestRequestCandidateInfoUsesVerifiedChatSelectors(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	err := runtime.RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{
		RequestPhone: true, RequestWechat: true, RequestResume: true, GreetMessage: "你好，想和你沟通这个岗位。",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantStableSelectors := []string{
		hliepinCandidateButtonTarget,
		hliepinRequestPhoneSelector,
		hliepinRequestWechatSelector,
		hliepinRequestResumeSelector,
		hliepinChatCloseSelector,
		hliepinCandidateListClose,
	}
	var stableSelectors []string
	var typePayload map[string]any
	var pressPayload map[string]any
	for index, path := range exec.paths {
		switch path {
		case hliepinStableClickPath:
			stableSelectors = append(stableSelectors, stringFromMap(exec.payloads[index], "target_selector"))
		case "/api/v1/page/type":
			typePayload = exec.payloads[index]
		case "/api/v1/page/press-key":
			pressPayload = exec.payloads[index]
		}
	}
	if fmt.Sprint(stableSelectors) != fmt.Sprint(wantStableSelectors) {
		t.Fatalf("stable selectors = %#v, want %#v", stableSelectors, wantStableSelectors)
	}
	if got := stringFromMap(mapFromAny(typePayload["element"]), "selector"); got != hliepinChatInputSelector {
		t.Fatalf("chat input selector = %q, want %q", got, hliepinChatInputSelector)
	}
	if got := stringFromMap(typePayload, "text"); got != "你好，想和你沟通这个岗位。" {
		t.Fatalf("message = %q", got)
	}
	if got := stringFromMap(pressPayload, "key"); got != "Enter" {
		t.Fatalf("send key = %q", got)
	}
	for _, delay := range exec.delays {
		if delay >= 1 {
			t.Fatalf("fixed one-second delay should be removed, delays = %#v", exec.delays)
		}
	}
}

// TestRequestCandidateInfoConfirmsOptionalDialog 验证猎聘索要确认弹框存在时会点击弹框内的确定按钮。
func TestRequestCandidateInfoConfirmsOptionalDialog(t *testing.T) {
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinRequestConfirmDialog: {map[string]any{"text": "确定向对方索要手机号吗？"}},
	}}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err != nil {
		t.Fatal(err)
	}
	confirmFindIndex := -1
	confirmClickIndex := -1
	for index, path := range exec.paths {
		if path == "/api/v1/page/find-elements" && stringFromMap(mapFromAny(exec.payloads[index]["element"]), "selector") == hliepinRequestConfirmDialog {
			confirmFindIndex = index
		}
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinRequestConfirmButton {
			confirmClickIndex = index
		}
	}
	if confirmFindIndex < 0 || confirmClickIndex < 0 {
		t.Fatalf("confirm paths missing: %#v", exec.paths)
	}
	if got := stringFromMap(mapFromAny(exec.payloads[confirmFindIndex]["element"]), "selector"); got != hliepinRequestConfirmDialog {
		t.Fatalf("confirm dialog selector = %q", got)
	}
	if got := stringFromMap(exec.payloads[confirmClickIndex], "parent_selector"); got != hliepinRequestConfirmDialog {
		t.Fatalf("confirm parent selector = %q", got)
	}
	if got := stringFromMap(exec.payloads[confirmClickIndex], "target_selector"); got != hliepinRequestConfirmButton {
		t.Fatalf("confirm button selector = %q", got)
	}
	if normalize, _ := exec.payloads[confirmClickIndex]["normalize_text_whitespace"].(bool); !normalize {
		t.Fatal("confirm button should normalize text whitespace")
	}
}

// TestRequestCandidateInfoWaitsForDelayedCurrentChat 验证猎聘聊天框延迟出现时直接复用，不再点击继续沟通。
func TestRequestCandidateInfoWaitsForDelayedCurrentChat(t *testing.T) {
	exec := &searchExecutor{}
	exec.findItemSequences = map[string][][]any{
		hliepinChatModalParent: [][]any{
			[]any{},
			[]any{},
			[]any{map[string]any{"text": "王先生", "fields": map[string]any{"candidate_name": "王先生"}}},
			[]any{},
		},
	}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err != nil {
		t.Fatal(err)
	}
	continueClicks := 0
	phoneClicks := 0
	for index, path := range exec.paths {
		if path != hliepinStableClickPath {
			continue
		}
		switch stringFromMap(exec.payloads[index], "target_selector") {
		case hliepinCandidateButtonTarget:
			continueClicks++
		case hliepinRequestPhoneSelector:
			phoneClicks++
		}
	}
	if continueClicks != 0 || phoneClicks != 1 {
		t.Fatalf("continue clicks = %d, phone clicks = %d", continueClicks, phoneClicks)
	}
}

// TestRequestCandidateInfoWaitsForDelayedActionButton 验证聊天姓名先出现、索要按钮稍后渲染时会按100毫秒轮询后再点击。
func TestRequestCandidateInfoWaitsForDelayedActionButton(t *testing.T) {
	exec := &searchExecutor{}
	exec.findItemSequences = map[string][][]any{
		hliepinChatModalParent + " " + hliepinRequestPhoneSelector: [][]any{
			[]any{},
			[]any{map[string]any{"text": "索要手机"}},
		},
	}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err != nil {
		t.Fatal(err)
	}
	phoneClicks := 0
	for index, path := range exec.paths {
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinRequestPhoneSelector {
			phoneClicks++
		}
	}
	if phoneClicks != 1 {
		t.Fatalf("phone clicks = %d, want 1", phoneClicks)
	}
	foundPollingDelay := false
	for _, delay := range exec.delays {
		if delay == hliepinPanelPollInterval {
			foundPollingDelay = true
			break
		}
	}
	if !foundPollingDelay {
		t.Fatalf("delays = %#v, want 100ms polling", exec.delays)
	}
}

// TestRequestCandidateInfoStopsWhenActionButtonNeverAppears 验证索要按钮始终未渲染时不执行错误坐标点击。
func TestRequestCandidateInfoStopsWhenActionButtonNeverAppears(t *testing.T) {
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinChatModalParent + " " + hliepinRequestPhoneSelector: {},
	}}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err == nil || !strings.Contains(err.Error(), "索要手机按钮超时") {
		t.Fatalf("error = %v, want action button timeout", err)
	}
	for index, path := range exec.paths {
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinRequestPhoneSelector {
			t.Fatal("phone button should not be clicked before it appears")
		}
	}
}

// TestRequestCandidateInfoReopensMismatchedChat 验证聊天框姓名不是当前候选人时先清理，再只打开一次当前候选人。
func TestRequestCandidateInfoReopensMismatchedChat(t *testing.T) {
	exec := &searchExecutor{}
	exec.findItemSequences = map[string][][]any{
		hliepinChatModalParent: [][]any{
			[]any{map[string]any{"text": "华志强", "fields": map[string]any{"candidate_name": "华志强"}}},
			[]any{map[string]any{"text": "华志强", "fields": map[string]any{"candidate_name": "华志强"}}},
			[]any{map[string]any{"text": "华志强", "fields": map[string]any{"candidate_name": "华志强"}}},
			[]any{},
			[]any{},
			[]any{},
			[]any{},
			[]any{},
			[]any{map[string]any{"text": "王先生", "fields": map[string]any{"candidate_name": "王先生"}}},
			[]any{},
		},
	}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err != nil {
		t.Fatal(err)
	}
	continueClicks := 0
	phoneClicks := 0
	for index, path := range exec.paths {
		if path != hliepinStableClickPath {
			continue
		}
		switch stringFromMap(exec.payloads[index], "target_selector") {
		case hliepinCandidateButtonTarget:
			continueClicks++
		case hliepinRequestPhoneSelector:
			phoneClicks++
		}
	}
	if continueClicks != 1 || phoneClicks != 1 {
		t.Fatalf("continue clicks = %d, phone clicks = %d", continueClicks, phoneClicks)
	}
}

// TestRequestCandidateInfoTimeoutCleansDrawer 验证聊天框未打开时只点击一次继续沟通，并在返回前关闭联系人抽屉。
func TestRequestCandidateInfoTimeoutCleansDrawer(t *testing.T) {
	exec := &searchExecutor{suppressContinueChat: true}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err == nil || !strings.Contains(err.Error(), "聊天框超时") {
		t.Fatalf("error = %v, want chat timeout", err)
	}
	continueClicks := 0
	for index, path := range exec.paths {
		if path == hliepinStableClickPath && stringFromMap(exec.payloads[index], "target_selector") == hliepinCandidateButtonTarget {
			continueClicks++
		}
	}
	if continueClicks != 1 {
		t.Fatalf("continue clicks = %d, want 1", continueClicks)
	}
	if exec.candidateDrawerOpen {
		t.Fatal("candidate drawer should be closed before returning")
	}
}

// TestRequestCandidateInfoSkipsEmptyRequest 验证岗位未勾选且无问候语时猎聘不打开沟通弹层。
func TestRequestCandidateInfoSkipsEmptyRequest(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	if err := runtime.RequestCandidateInfo(context.Background(), exec, nil, platformcore.Candidate{}, platformcore.CandidateInfoRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("empty request paths = %#v", exec.paths)
	}
}

// TestRequestCandidateInfoFailsWhenRequestedButtonMissing 验证猎聘索要按钮不存在时整次索要失败并返回明确错误。
func TestRequestCandidateInfoFailsWhenRequestedButtonMissing(t *testing.T) {
	exec := &searchExecutor{stableFailures: map[string]int{hliepinRequestPhoneSelector: 1}}
	err := NewRuntime().RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true})
	if err == nil || !strings.Contains(err.Error(), "索要手机") || !strings.Contains(err.Error(), "稳定目标尚未就绪") {
		t.Fatalf("err = %v", err)
	}
}

// TestHLiepinCandidateRowParentSelector 验证猎聘候选人父级只能由安全的稳定简历 ID 生成。
// t 为测试对象。
func TestHLiepinCandidateRowParentSelector(t *testing.T) {
	selector, err := hliepinCandidateRowParentSelector(platformcore.Candidate{
		"platform_candidate_id": "abc_123-XYZ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if selector != "tbody tr.r-abc_123-XYZ" {
		t.Fatalf("selector = %q", selector)
	}
	if _, err := hliepinCandidateRowParentSelector(platformcore.Candidate{"platform_candidate_id": "bad id > button"}); err == nil {
		t.Fatal("unsafe candidate id should be rejected")
	}
}

// TestHLiepinStableClickPayloadsAlwaysUseParentAndTarget 验证猎聘稳定点击调用均同时携带父级和目标选择器。
// t 为测试对象。
func TestHLiepinStableClickPayloadsAlwaysUseParentAndTarget(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{}
	if err := runtime.RequestCandidateInfo(context.Background(), exec, nil, hliepinStableTestCandidate(0), platformcore.CandidateInfoRequest{RequestPhone: true}); err != nil {
		t.Fatal(err)
	}
	stableCalls := 0
	actionIDs := map[string]bool{}
	for index, path := range exec.paths {
		if path != hliepinStableClickPath {
			continue
		}
		stableCalls++
		if strings.TrimSpace(stringFromMap(exec.payloads[index], "parent_selector")) == "" {
			t.Fatalf("stable payload[%d] parent selector is empty", index)
		}
		if strings.TrimSpace(stringFromMap(exec.payloads[index], "target_selector")) == "" {
			t.Fatalf("stable payload[%d] target selector is empty", index)
		}
		actionID := strings.TrimSpace(stringFromMap(exec.payloads[index], "action_id"))
		if actionID == "" {
			t.Fatalf("stable payload[%d] action id is empty", index)
		}
		if actionIDs[actionID] {
			t.Fatalf("stable payload[%d] action id is duplicated: %s", index, actionID)
		}
		actionIDs[actionID] = true
	}
	if stableCalls == 0 {
		t.Fatal("expected stable click calls")
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

// TestPreparePositionSearchDoesNotInspectMissingShortcut 验证手动筛选模式不会因页面缺少配置过的快捷搜索而停止。
// t 为测试对象。
func TestPreparePositionSearchDoesNotInspectMissingShortcut(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{hliepinShortcutItemSelector: {}}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{
		"name": "Java开发工程师初级", "common_config": map[string]any{"hliepin_shortcut_search_name": "Java开发工程师初级"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("manual filtering should not inspect shortcuts, paths=%#v", exec.paths)
	}
}

// TestPreparePositionSearchDoesNotInspectMissingPublishedJob 验证手动筛选模式不会因页面没有对应发布职位而停止。
// t 为测试对象。
func TestPreparePositionSearchDoesNotInspectMissingPublishedJob(t *testing.T) {
	runtime := NewRuntime()
	exec := &searchExecutor{findItems: map[string][]any{
		hliepinPublishedJobSelector: {map[string]any{"text": "PHP程序员"}},
	}}
	err := runtime.PreparePositionSearch(context.Background(), exec, nil, map[string]any{"name": "Java开发工程师初级"})
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.paths) != 0 {
		t.Fatalf("manual filtering should not inspect published jobs, paths=%#v", exec.paths)
	}
}
