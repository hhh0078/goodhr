// Package hliepin 文件作用：实现猎聘猎头端候选人详情打开、提取和关闭。
package hliepin

import (
	"context"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// OpenCandidateDetail 打开指定猎聘猎头端候选人详情。
func (r *Runtime) OpenCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, candidate model.Candidate) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("读取猎聘候选人列表页地址失败：%w", err)
	}
	currentURL, err := currentPageURL(pages)
	if err != nil {
		return fmt.Errorf("暂时不能确定猎聘候选人列表页：%w", err)
	}

	selector, err := common.CandidateActionSelector(cfg, "candidate.open_target", candidate)
	if err != nil {
		return err
	}
	result, err := browser.Click(ctx, contract.ElementClickRequest{
		Selector: selector, ViewportMargin: 48,
		WaitForNewPage: true, NewPageTimeoutMS: 10000,
	})
	if err != nil {
		return err
	}
	if !result.NewPageOpened {
		return fmt.Errorf("猎聘候选人详情没有打开新页面")
	}
	r.detailReturnURL = currentURL
	return nil
}

// ExtractCandidateDetail 提取当前猎聘猎头端候选人详情文本。
func (r *Runtime) ExtractCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) (model.CandidateDetail, error) {
	return common.ExtractCandidateDetail(ctx, browser, cfg)
}

// CleanCandidateDetailText 清理猎聘猎头端候选人详情两端空白。
func (r *Runtime) CleanCandidateDetailText(text string) string {
	return strings.TrimSpace(text)
}

// BrowseCandidateDetail 在 AI 判断前使用真实滚轮把猎聘猎头端详情向下浏览。
func (r *Runtime) BrowseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	return common.BrowseCandidateDetail(ctx, browser, cfg, 360, 8)
}

// CloseCandidateDetail 关闭当前猎聘猎头端候选人详情。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	returnURL := r.detailReturnURL
	r.detailReturnURL = ""
	if returnURL == "" {
		return fmt.Errorf("没有记住原猎聘候选人列表页地址，暂时不能安全返回")
	}
	return closeCurrentPageToURL(ctx, browser, returnURL)
}

// currentPageURL 返回唯一活动标签页的完整地址。
func currentPageURL(pages contract.PageListResult) (string, error) {
	currentURL := ""
	currentCount := 0
	for _, page := range pages.Pages {
		if !page.Current {
			continue
		}
		currentCount++
		currentURL = strings.TrimSpace(page.URL)
	}
	if currentCount == 0 || currentURL == "" {
		return "", fmt.Errorf("暂时没有读到活动标签页地址")
	}
	if currentCount > 1 {
		return "", fmt.Errorf("当前同时存在 %d 个活动标签页", currentCount)
	}
	return currentURL, nil
}

// closeCurrentPageToURL 关闭当前猎聘详情页，并按完整地址切回唯一的原标签页。
func closeCurrentPageToURL(ctx context.Context, browser model.Browser, returnURL string) error {
	if err := browser.ClosePage(ctx); err != nil {
		return fmt.Errorf("关闭猎聘候选人详情页失败：%w", err)
	}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("关闭详情后读取猎聘标签页失败：%w", err)
	}
	matches := make([]contract.PageInfo, 0, 1)
	for _, page := range pages.Pages {
		if strings.TrimSpace(page.URL) == returnURL {
			matches = append(matches, page)
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("原猎聘候选人列表页已经不在了，地址：%s", returnURL)
	}
	if len(matches) > 1 {
		return fmt.Errorf("发现 %d 个地址相同的猎聘标签页，无法确定该返回哪一个：%s", len(matches), returnURL)
	}
	if _, err := browser.UsePage(ctx, contract.PageUseRequest{PageID: matches[0].PageID}); err != nil {
		return fmt.Errorf("切回原猎聘候选人列表页失败：%w", err)
	}
	return nil
}

// closeUnexpectedResumeDetail 关闭索要简历时意外打开的详情页，并切回原候选人列表页。
func closeUnexpectedResumeDetail(ctx context.Context, browser model.Browser, returnURL string) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("索要简历后读取猎聘标签页失败：%w", err)
	}
	currentURL, err := currentPageURL(pages)
	if err != nil {
		return err
	}
	if !strings.Contains(currentURL, "/resume/showresumedetail/") {
		return nil
	}
	return closeCurrentPageToURL(ctx, browser, returnURL)
}
