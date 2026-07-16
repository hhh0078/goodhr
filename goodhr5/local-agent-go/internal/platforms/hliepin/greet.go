// Package hliepin 文件作用：承载 greet.go 对应的平台职责实现。
package hliepin

import (
	"context"
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
		if err := r.selectGreetJob(ctx, exec, positionName); err != nil {
			return err
		}
	}
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "立即开聊", "exact": true,
		"element": map[string]any{"selector": ".ant-modal button"}, "timeout": 5000,
	}); err != nil {
		return fmt.Errorf("点击猎聘“立即开聊”失败：%w", err)
	}
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

// selectGreetJob 在猎聘开聊弹框中读取职位名列表，并兼容完整名称和省略号截断名称选择岗位运行岗位。
func (r *Runtime) selectGreetJob(ctx context.Context, exec platformcore.Executor, positionName string) error {
	if _, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "请选择开聊的职位", "exact": true,
		"element": map[string]any{"selector": ".ant-modal .ant-select-selector"}, "timeout": 5000,
	}); err != nil {
		return fmt.Errorf("打开猎聘开聊职位下拉框失败：%w", err)
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element":      map[string]any{"selector": hliepinGreetJobOptionSelector},
		"fields":       []any{map[string]any{"position_name": map[string]any{"selector": ".hpublic-job-select-option strong"}}},
		"visible_only": true, "max_items": 100,
	})
	if err != nil {
		return fmt.Errorf("读取猎聘开聊职位列表失败：%w", err)
	}
	items := mapList(workerData(result, "items"))
	matchIndex, matchName := matchingGreetJobItem(items, positionName)
	if matchIndex < 0 {
		return fmt.Errorf("猎聘开聊弹框中未找到岗位运行岗位“%s”，当前职位=%s", positionName, greetJobItemNames(items))
	}
	if _, err := exec.Post(ctx, "/api/v1/page/list-click-by-index", map[string]any{
		"element":      map[string]any{"selector": hliepinGreetJobOptionSelector},
		"click_target": map[string]any{"selector": hliepinGreetJobClickSelector},
		"index":        matchIndex, "timeout": 5000, "require_full": false,
	}); err != nil {
		return fmt.Errorf("选择猎聘开聊职位“%s”失败：%w", matchName, err)
	}
	exec.Log("info", "猎聘打招呼：开聊职位已选择="+matchName)
	return exec.Delay(ctx, "等待猎聘开聊职位选择生效", 0.2)
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
