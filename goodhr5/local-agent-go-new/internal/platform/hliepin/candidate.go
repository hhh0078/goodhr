// Package hliepin 文件作用：实现猎聘猎头端候选人列表读取、定位、滚动和翻页。
package hliepin

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// FindCandidates 返回猎聘猎头端当前候选人列表的结构化摘要。
func (r *Runtime) FindCandidates(ctx context.Context, browser model.Browser, cfg model.Config, options model.CandidateListOptions) ([]model.Candidate, error) {
	candidates, err := common.FindCandidates(ctx, browser, cfg, r.PlatformID(), options)
	if err != nil {
		return nil, err
	}
	result := make([]model.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !strings.Contains(candidate.Summary, "立即沟通") && !strings.Contains(candidate.Summary, "继续沟通") {
			continue
		}
		if candidate.Fields == nil {
			candidate.Fields = make(map[string]string)
		}
		if name := stableCandidateName(candidate.Name); name != "" {
			candidate.Name = name
			candidate.Fields["name"] = name
			candidate.IdentityTexts = common.CandidateIdentityTexts(
				name,
				candidate.Fields,
				candidate.Summary,
			)
		}
		if strings.Contains(candidate.Summary, "立即沟通") {
			candidate.Fields["greet_state"] = "immediate"
		} else {
			candidate.Fields["greet_state"] = "continue"
		}
		stableID := strings.TrimSpace(candidate.Fields["platform_candidate_id"])
		if stableID != "" {
			candidate.Fingerprint = r.PlatformID() + "_" + stableID
		} else {
			candidate.Fingerprint = common.HashText(r.PlatformID() + "|" + stableCandidateText(candidate.Summary))
		}
		result = append(result, candidate)
	}
	return result, nil
}

// stableCandidateName 从猎聘姓名区域中只保留第一条真实姓名，移除“阅”等动态标记。
func stableCandidateName(value string) string {
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || transientCandidateLine(line) || line == "名片简历" {
			continue
		}
		return line
	}
	return ""
}

// ScrollToCandidate 先清理晚出现的开聊推广弹框，再通过真实滚轮定位候选人。
func (r *Runtime) ScrollToCandidate(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	if err := closePostGreetPromotion(ctx, browser, cfg, 1); err != nil {
		return err
	}
	return common.ScrollToCandidate(ctx, browser, cfg, candidate)
}

// AdvanceCandidateList 按网页实际页码寻找下一页，只有页面明确禁用下一页时才结束列表。
func (r *Runtime) AdvanceCandidateList(ctx context.Context, browser model.Browser, cfg model.Config, before []model.Candidate, options model.CandidateListOptions) (bool, error) {
	if err := closeCandidatePanels(ctx, browser, cfg); err != nil {
		return false, err
	}
	// 长列表逐段滚轮可能耗尽 Worker 等待时间，先用真实按键到页面底部，再由滚轮精确定位。
	if _, err := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Control+End", DelayMS: 200}); err != nil {
		return false, fmt.Errorf("定位猎聘分页栏失败：%w", err)
	}
	currentText, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.current_page")
	if err != nil {
		return false, fmt.Errorf("读取猎聘当前页码失败：%w", err)
	}
	currentPage, parseErr := strconv.Atoi(strings.TrimSpace(currentText))
	if !found || parseErr != nil || currentPage < 1 || currentPage == int(^uint(0)>>1) {
		log.Printf("event=candidate_page_advance platform=hliepin status=current_page_missing found=%t", found)
		return false, fmt.Errorf("暂时没读到猎聘当前页码，未执行翻页；请确认候选人列表底部的分页栏已加载后重试")
	}
	lastPage, err := common.SelectorExists(ctx, browser, cfg, "candidate.next_page_disabled")
	if err != nil {
		return false, err
	}
	if lastPage {
		log.Printf("event=candidate_page_advance platform=hliepin current_page=%d status=last_page", currentPage)
		return false, nil
	}
	pageNumber := currentPage + 1
	nextExists, err := common.SelectorExists(ctx, browser, cfg, "candidate.next_page")
	if err != nil {
		return false, err
	}
	method := "number_page"
	var advanced bool
	if nextExists {
		method = "next_page"
		selector, selectorErr := common.RequiredSelector(cfg, "candidate.next_page")
		if selectorErr != nil {
			return false, selectorErr
		}
		log.Printf("event=candidate_page_advance platform=hliepin current_page=%d target_page=%d method=%s status=start", currentPage, pageNumber, method)
		advanced, err = common.AdvanceCandidatePageTarget(ctx, browser, cfg, r.PlatformID(), selector, pageNumber, before, options)
	} else {
		log.Printf("event=candidate_page_advance platform=hliepin current_page=%d target_page=%d method=%s status=start", currentPage, pageNumber, method)
		advanced, err = common.AdvanceCandidateNumberPage(ctx, browser, cfg, r.PlatformID(), "candidate.page_number", pageNumber, before, options)
	}
	if err == nil && !advanced {
		err = fmt.Errorf("猎聘从第 %d 页翻到第 %d 页尚未确认成功，暂不继续处理；请检查分页栏后重试", currentPage, pageNumber)
	}
	log.Printf("event=candidate_page_advance platform=hliepin current_page=%d target_page=%d method=%s advanced=%t failed=%t", currentPage, pageNumber, method, advanced, err != nil)
	return advanced, err
}

// stableCandidateText 移除会随浏览和沟通变化的猎聘列表状态文字。
func stableCandidateText(value string) string {
	lines := make([]string, 0)
	for _, rawLine := range strings.Split(value, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || transientCandidateLine(line) {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// transientCandidateLine 判断一行是否为猎聘候选人动态状态。
func transientCandidateLine(value string) bool {
	if strings.HasSuffix(value, "活跃") && len([]rune(value)) <= 8 {
		return true
	}
	switch value {
	case "在线", "活跃状态", "隐藏", "阅", "已查看", "名片简历", "立即沟通", "继续沟通", "已沟通", "获取联系方式":
		return true
	default:
		return false
	}
}
