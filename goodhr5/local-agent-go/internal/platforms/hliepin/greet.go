// Package hliepin 文件作用：承载 greet.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
)

const (
	hliepinGreetJobOptionSelector = hliepinGreetDropdownParent + " " + hliepinGreetJobOptionTarget
	hliepinGreetModalSelector     = hliepinGreetModalParent
	hliepinGreetPollAttempts      = 20
	hliepinGreetPollDelaySeconds  = 0.25
)

// GreetCandidate 执行猎聘猎头端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	if err := closeHliepinGreetModalIfPresent(ctx, exec); err != nil {
		exec.Log("warning", "猎聘打招呼：开始前清理遗留开聊弹框失败，继续尝试当前候选人，错误="+err.Error())
	}
	return r.greetCandidateOnce(ctx, exec, cfg, candidate)
}

// greetCandidateOnce 执行一次猎聘开聊流程，任一步骤失败后直接返回且不重新点击候选人。
func (r *Runtime) greetCandidateOnce(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	rowParent, err := hliepinCandidateRowParentSelector(candidate)
	if err != nil {
		return err
	}
	if _, err := hliepinStableClick(ctx, exec, rowParent, hliepinCandidateButtonTarget, map[string]any{
		"expected_text":     "立即沟通",
		"exact_text":        true,
		"wait_for_selector": hliepinGreetModalParent,
		"wait_timeout":      5000,
	}); err != nil {
		return fmt.Errorf("点击猎聘候选人“立即沟通”失败：%w", err)
	}
	positionName := strings.TrimSpace(r.currentPosition)
	if positionName == "" {
		return fmt.Errorf("猎聘打招呼时岗位运行岗位名称为空，无法选择开聊职位")
	}
	if err := waitForHliepinGreetModal(ctx, exec); err != nil {
		return err
	}
	if r.greetJobSelected {
		exec.Log("info", "猎聘打招呼：岗位模式已在搜索页选择职位，开聊弹框无需重复选择")
	} else {
		selected, err := r.selectGreetJob(ctx, exec, positionName)
		if err != nil {
			return err
		}
		if !selected {
			if err := clickGreetWithoutJob(ctx, exec, positionName); err != nil {
				return err
			}
			return r.finishGreetCandidate(ctx, exec)
		}
	}
	if _, err := hliepinStableClick(ctx, exec, hliepinGreetModalParent, hliepinGreetSubmitTarget, map[string]any{
		"expected_text": "立即开聊", "exact_text": true,
		"wait_for_hidden_selector": hliepinGreetModalParent, "wait_timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击猎聘“立即开聊”失败：%w", err)
	}
	return r.finishGreetCandidate(ctx, exec)
}

// finishGreetCandidate 等待猎聘开聊完成，并发送两次 Esc 关闭后续提示弹框。
func (r *Runtime) finishGreetCandidate(ctx context.Context, exec platformcore.Executor) error {
	if err := exec.Delay(ctx, "等待猎聘开聊后提示弹框", 1); err != nil {
		return err
	}
	exec.Log("info", "猎聘打招呼：立即开聊后发送第 1 次 Esc")
	if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape"}); err != nil {
		return fmt.Errorf("猎聘立即开聊后第 1 次按 Esc 失败：%w", err)
	}
	if err := exec.Delay(ctx, "等待猎聘后续提示弹框接收 Esc", 0.5); err != nil {
		return err
	}
	exec.Log("info", "猎聘打招呼：立即开聊后发送第 2 次 Esc")
	if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape"}); err != nil {
		return fmt.Errorf("猎聘立即开聊后第 2 次按 Esc 失败：%w", err)
	}
	return nil
}

// selectGreetJob 在猎聘开聊弹框中匹配并选择岗位，未匹配时返回 false 交给不选职位流程。
func (r *Runtime) selectGreetJob(ctx context.Context, exec platformcore.Executor, positionName string) (bool, error) {
	if _, err := hliepinStableClick(ctx, exec, hliepinGreetModalParent, hliepinGreetJobSelectTarget, map[string]any{
		"expected_text": "请选择开聊的职位", "exact_text": true,
		"wait_for_selector": hliepinGreetDropdownParent, "wait_timeout": 5000,
	}); err != nil {
		return false, fmt.Errorf("打开猎聘开聊职位下拉框失败：%w", err)
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinGreetJobOptionSelector},
		"fields":       []any{map[string]any{"position_name": map[string]any{"selector": ".hpublic-job-select-option strong"}}},
		"visible_only": true, "max_items": 100,
	})
	if err != nil {
		return false, fmt.Errorf("读取猎聘开聊职位列表失败：%w", err)
	}
	items := mapList(workerData(result, "items"))
	matchIndex, matchName := matchingGreetJobItem(items, positionName)
	if matchIndex < 0 {
		exec.Log("warning", fmt.Sprintf("猎聘打招呼：未找到开聊职位，准备不选职位直接开聊，岗位=%s，当前职位=%s", positionName, greetJobItemNames(items)))
		return false, nil
	}
	if _, err := hliepinStableClick(ctx, exec, hliepinGreetDropdownParent, hliepinGreetJobOptionTarget, map[string]any{
		"target_index": matchIndex, "nested_selector": hliepinGreetJobOptionNested,
		"expected_text": matchName, "exact_text": false,
		"wait_for_hidden_selector": hliepinGreetDropdownParent, "wait_timeout": 5000,
	}); err != nil {
		return false, fmt.Errorf("选择猎聘开聊职位“%s”失败：%w", matchName, err)
	}
	exec.Log("info", "猎聘打招呼：开聊职位已选择="+matchName)
	if err := exec.Delay(ctx, "等待猎聘开聊职位选择生效", 0.2); err != nil {
		return false, err
	}
	return true, nil
}

// waitForHliepinGreetModal 轮询等待猎聘开聊弹框完整出现，避免固定延迟早于页面渲染完成。
func waitForHliepinGreetModal(ctx context.Context, exec platformcore.Executor) error {
	for attempt := 0; attempt < hliepinGreetPollAttempts; attempt++ {
		visible, err := hliepinGreetModalVisible(ctx, exec)
		if err == nil && visible {
			return nil
		}
		if attempt+1 < hliepinGreetPollAttempts {
			if delayErr := exec.Delay(ctx, "等待猎聘开聊职位弹框渲染", hliepinGreetPollDelaySeconds); delayErr != nil {
				return delayErr
			}
		}
	}
	return fmt.Errorf("等待猎聘开聊职位弹框超时")
}

// closeHliepinGreetModalIfPresent 仅检测并关闭猎聘开聊弹框，不影响页面上的其他弹层。
func closeHliepinGreetModalIfPresent(ctx context.Context, exec platformcore.Executor) error {
	for attempt := 0; attempt < 2; attempt++ {
		visible, err := hliepinGreetModalVisible(ctx, exec)
		if err != nil {
			return err
		}
		if !visible {
			return nil
		}
		if _, err := exec.Post(ctx, "/api/v1/page/press-key", map[string]any{"key": "Escape"}); err != nil {
			return fmt.Errorf("关闭猎聘开聊弹框失败：%w", err)
		}
		if err := exec.Delay(ctx, "等待猎聘遗留开聊弹框关闭", 0.2); err != nil {
			return err
		}
	}
	visible, err := hliepinGreetModalVisible(ctx, exec)
	if err != nil {
		return err
	}
	if visible {
		return fmt.Errorf("猎聘开聊弹框连续按两次 Esc 后仍未关闭")
	}
	return nil
}

// hliepinGreetModalVisible 判断当前可见弹层是否为猎聘“请选择职位开聊”弹框。
func hliepinGreetModalVisible(ctx context.Context, exec platformcore.Executor) (bool, error) {
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element": map[string]any{"selector": hliepinGreetModalSelector}, "visible_only": true, "max_items": 10,
	})
	if err != nil {
		return false, err
	}
	for _, item := range mapList(workerData(result, "items")) {
		text := strings.TrimSpace(stringFromMap(item, "text"))
		if strings.Contains(text, "请选择职位开聊") || strings.Contains(text, "请选择开聊的职位") {
			return true, nil
		}
	}
	return false, nil
}

// clickGreetWithoutJob 在职位未匹配时兼容点击“不选职位”或“不选择职位”开聊按钮。
func clickGreetWithoutJob(ctx context.Context, exec platformcore.Executor, positionName string) error {
	if _, err := hliepinStableClick(ctx, exec, hliepinGreetModalParent, hliepinGreetWithoutJobTarget, map[string]any{
		"expected_text": "不选择职位开聊", "exact_text": true,
		"wait_for_hidden_selector": hliepinGreetModalParent, "wait_timeout": 5000,
	}); err != nil {
		return fmt.Errorf("猎聘未找到匹配职位，点击不选择职位开聊失败：%w", err)
	}
	exec.Log("info", "猎聘打招呼：未匹配岗位“"+positionName+"”，已点击不选职位直接开聊")
	return nil
}

// normalizeSearchMatchText 统一开聊职位比较时的大小写并移除空白，兼容职位名称的截断显示。
func normalizeSearchMatchText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

// matchingGreetJobItem 优先完整匹配开聊职位，再去掉末尾省略号做双向包含匹配。
func matchingGreetJobItem(items []map[string]any, positionName string) (int, string) {
	target := normalizeSearchMatchText(positionName)
	matchIndex := -1
	matchName := ""
	matchLength := 0
	for index, item := range items {
		fields := mapFromAny(item["fields"])
		name := firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(item, "text"))
		normalized := normalizeSearchMatchText(name)
		if normalized == target && normalized != "" {
			return index, name
		}
		trimmed := strings.TrimRight(normalized, ".…")
		if trimmed == "" || (!strings.Contains(target, trimmed) && !strings.Contains(trimmed, target)) || len([]rune(trimmed)) <= matchLength {
			continue
		}
		matchIndex = index
		matchName = name
		matchLength = len([]rune(trimmed))
	}
	return matchIndex, matchName
}

// greetJobItemNames 汇总开聊弹框中读取到的职位名，用于匹配失败时输出错误信息。
func greetJobItemNames(items []map[string]any) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		fields := mapFromAny(item["fields"])
		if name := firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(item, "text")); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "无"
	}
	return strings.Join(names, "、")
}
