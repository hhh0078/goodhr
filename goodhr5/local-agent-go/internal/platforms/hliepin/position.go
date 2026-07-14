// Package hliepin 文件作用：承载 position.go 对应的平台职责实现。
package hliepin

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go/internal/cloudapi"
	"goodhr5/local-agent-go/internal/platformcore"
	"regexp"
	"strings"
	"time"
)

// CurrentPositionName 读取当前页面岗位名称。
func (r *Runtime) CurrentPositionName(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig) (string, error) {
	if name := normalizePositionName(r.currentPosition); name != "" {
		return name, nil
	}
	current := platformElement(cfg, "position", "current")
	if current == nil {
		return "", fmt.Errorf("平台配置中无当前岗位选择器")
	}
	result, err := exec.Post(ctx, "/api/v1/page/extract-text", map[string]any{"element": current, "timeout": 2500})
	if err != nil {
		return "", err
	}
	data := workerDataMap(result)
	name := normalizePositionName(firstNonEmpty(stringFromMap(data, "text"), firstStringFromAny(data["texts"])))
	if name == "" {
		return "", fmt.Errorf("页面当前岗位为空")
	}
	return name, nil
}

// SelectPosition 在猎聘猎头端页面切换岗位。
func (r *Runtime) SelectPosition(ctx context.Context, exec platformcore.Executor, cfg cloudapi.PlatformConfig, positionName string) error {
	target := strings.TrimSpace(positionName)
	if target == "" {
		return fmt.Errorf("任务岗位名称为空")
	}
	jobID, lookupErr := r.publishedJobID(ctx, exec, target)
	// 猎聘猎头端的岗位来自“正在发布的职位”，不使用输入框搜索。
	_, _ = exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
		"text": "展开更多职位", "exact": true, "timeout": 2500,
	})
	if err := exec.Delay(ctx, "等待正在发布的职位展开", 0.4); err != nil {
		return err
	}
	if jobID != "" {
		selector := fmt.Sprintf(`[data-tlg-ext*="%%22hjob_id%%22%%3A%s"]`, jobID)
		if _, err := exec.Post(ctx, "/api/v1/page/click", map[string]any{"element": map[string]any{"selector": selector}, "timeout": 8000}); err != nil {
			return fmt.Errorf("点击正在发布的职位失败：%s：%w", positionName, err)
		}
	} else {
		_, err := exec.Post(ctx, "/api/v1/page/click-by-text", map[string]any{
			"text": target, "exact": false, "element": map[string]any{"selector": "li"}, "timeout": 8000,
		})
		if err != nil {
			return fmt.Errorf("正在发布的职位中未找到岗位：%s，完整岗位映射错误=%v，页面匹配错误=%w", positionName, lookupErr, err)
		}
	}
	r.currentPosition = target
	if err := exec.Delay(ctx, "等待猎聘候选人结果刷新", 1.2); err != nil {
		return err
	}
	if err := r.ensureHiddenCandidateFilters(ctx, exec, true); err != nil {
		return err
	}
	return nil
}

// publishedJobID 从猎聘职位管理页读取完整岗位名对应的 hjob_id。
// 猎聘找人页会把长岗位名直接截断为省略号，使用 ID 可避免同前缀岗位被选错。
func (r *Runtime) publishedJobID(ctx context.Context, exec platformcore.Executor, positionName string) (string, error) {
	opened, err := exec.Post(ctx, "/api/v1/page/open", map[string]any{
		"url": "https://h.liepin.com/job/showlistpage?jobStatus=11", "new_page": true, "timeout": 30000,
	})
	if err != nil {
		return "", err
	}
	openData := workerDataMap(opened)
	detailToken := stringFromMap(openData, "page_token")
	returnToken := stringFromMap(openData, "previous_page_token")
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = exec.Post(cleanupCtx, "/api/v1/page/close", map[string]any{
			"page_token": detailToken, "return_page_token": returnToken,
			"only_url_contains": "/job/showlistpage", "return_url_contains": "h.liepin.com/search/",
		})
	}()
	if err := exec.Delay(ctx, "等待猎聘完整职位列表", 0.8); err != nil {
		return "", err
	}
	result, err := exec.Post(ctx, "/api/v1/page/find-elements", map[string]any{
		"element": map[string]any{"selector": "tbody tr"}, "visible_only": true, "include_html": true,
		"fields":    []any{map[string]any{"position_name": map[string]any{"selector": ".name-link"}}},
		"max_items": 200,
	})
	if err != nil {
		return "", err
	}
	target := normalizePositionName(positionName)
	idPattern := regexp.MustCompile(`data-row-key=["']([0-9]+)["']`)
	for _, item := range mapList(workerData(result, "items")) {
		fields := mapFromAny(item["fields"])
		name := normalizePositionName(firstNonEmpty(stringFromMap(fields, "position_name"), stringFromMap(item, "text")))
		if name != target {
			continue
		}
		match := idPattern.FindStringSubmatch(stringFromMap(item, "html"))
		if len(match) >= 2 {
			return match[1], nil
		}
		return "", fmt.Errorf("岗位“%s”未读到 hjob_id", positionName)
	}
	return "", fmt.Errorf("职位管理页未找到完整岗位名：%s", positionName)
}

// ensureHiddenCandidateFilters 确保猎聘结果页的三个隐藏筛选已选中。
func (r *Runtime) ensureHiddenCandidateFilters(ctx context.Context, exec platformcore.Executor, required bool) error {
	timeout := 3500
	if !required {
		timeout = 300
	}
	for _, label := range []string{"隐藏已查看", "隐藏已沟通", "隐藏已获取联系方式"} {
		// 猎聘每次勾选后都会自动滚到候选人列表，下一次必须先用真实滚轮回到顶部再重新查找。
		if _, err := exec.Post(ctx, "/api/v1/page/scroll", map[string]any{"distance": -10000}); err != nil {
			return fmt.Errorf("设置猎聘筛选前向上滚动失败：%w", err)
		}
		if err := exec.Delay(ctx, "等待猎聘隐藏筛选重新可见", 0.45); err != nil {
			return err
		}
		if _, err := exec.Post(ctx, "/api/v1/page/ensure-checked-by-text", map[string]any{
			"text": label, "required": required, "timeout": timeout, "viewport_margin": 20,
		}); err != nil {
			return fmt.Errorf("设置猎聘筛选“%s”失败：%w", label, err)
		}
	}
	return nil
}
