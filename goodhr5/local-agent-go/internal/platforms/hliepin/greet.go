// Package hliepin 文件作用：承载 greet.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"errors"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"strings"
)

const (
	hliepinGreetJobOptionSelector = ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option"
	hliepinGreetJobClickSelector  = ".hpublic-job-select-option"
)

// GreetCandidate 执行猎聘猎头端候选人打招呼。
func (r *Runtime) GreetCandidate(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, candidate platformcore.Candidate) error {
	item := candidateItemElement(candidate, cfg)
	greetBtn := platformElement(cfg, "actions", "greetBtn")
	if greetBtn == nil {
		greetBtn = map[string]any{"selector": "button"}
	} else {
		greetBtn["selectors"] = []any{"button"}
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{"index": intFromMap(candidate, "card_index"), "item": item, "clickTarget": greetBtn, "timeout": 10000}); err != nil {
		return err
	}
	positionName := strings.TrimSpace(r.currentPosition)
	if positionName == "" {
		return fmt.Errorf("猎聘打招呼时岗位运行岗位名称为空，无法选择开聊职位")
	}
	if err := exec.Delay(ctx, "等待猎聘开聊职位弹框", 0.4); err != nil {
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
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "立即开聊", "exact": true,
		"element": map[string]any{"selector": ".ant-modal button"}, "timeout": 5000,
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
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "请选择开聊的职位", "exact": true,
		"element": map[string]any{"selector": ".ant-modal .ant-select-selector"}, "timeout": 5000,
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
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"element":      map[string]any{"selector": hliepinGreetJobOptionSelector},
		"click_target": map[string]any{"selector": hliepinGreetJobClickSelector},
		"index":        matchIndex, "timeout": 5000, "require_full": false,
	}); err != nil {
		return false, fmt.Errorf("选择猎聘开聊职位“%s”失败：%w", matchName, err)
	}
	exec.Log("info", "猎聘打招呼：开聊职位已选择="+matchName)
	if err := exec.Delay(ctx, "等待猎聘开聊职位选择生效", 0.2); err != nil {
		return false, err
	}
	return true, nil
}

// clickGreetWithoutJob 在职位未匹配时兼容点击“不选职位”或“不选择职位”开聊按钮。
func clickGreetWithoutJob(ctx context.Context, exec platformcore.Executor, positionName string) error {
	var failures []error
	for _, text := range []string{"不选职位", "不选择职位"} {
		if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
			"text": text, "exact": false,
			"element": map[string]any{"selector": ".ant-modal button"}, "timeout": 3000,
		}); err == nil {
			exec.Log("info", "猎聘打招呼：未匹配岗位“"+positionName+"”，已点击不选职位直接开聊")
			return nil
		} else {
			failures = append(failures, fmt.Errorf("匹配文字“%s”：%w", text, err))
		}
	}
	return fmt.Errorf("猎聘未找到匹配职位，点击不选职位开聊失败：%w", errors.Join(failures...))
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
