// Package hliepin 文件作用：实现猎聘猎头端候选人详情打开、提取和关闭。
package hliepin

import (
	"context"
	"fmt"
	"net/url"
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
	r.detailURL = strings.TrimSpace(result.NewPageURL)
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

// CloseCandidateDetail 只关闭确认过的猎聘详情页，失败时保留列表地址，重复补关不关闭列表。
func (r *Runtime) CloseCandidateDetail(ctx context.Context, browser model.Browser, cfg model.Config, _ model.Candidate) error {
	returnURL := r.detailReturnURL
	if returnURL == "" {
		pages, err := browser.ListPages(ctx)
		if err != nil {
			return fmt.Errorf("读取猎聘标签页失败：%w", err)
		}
		currentURL, err := currentPageURL(pages)
		if err != nil {
			return err
		}
		if !isResumeDetailURL(currentURL) {
			return nil
		}
		return fmt.Errorf("没有记住原猎聘候选人列表页地址，暂时不能安全返回")
	}
	if r.detailURL != "" {
		pages, err := browser.ListPages(ctx)
		if err != nil {
			return fmt.Errorf("读取本次猎聘详情标签页失败：%w", err)
		}
		matches := 0
		for _, page := range pages.Pages {
			if strings.TrimSpace(page.URL) != r.detailURL {
				continue
			}
			matches++
			if !page.Current {
				return fmt.Errorf("本次候选人详情仍在另一个标签页，暂时不能安全确认关闭")
			}
		}
		if matches > 1 {
			return fmt.Errorf("本次候选人详情匹配到多个标签页，暂时不能安全确认关闭")
		}
		if matches == 0 {
			currentURL, err := currentPageURL(pages)
			if err != nil {
				return err
			}
			if isResumeDetailURL(currentURL) {
				return fmt.Errorf("当前标签页属于另一份候选人详情，暂时不能安全返回")
			}
		}
	}
	if err := closeCurrentPageToURL(ctx, browser, returnURL); err != nil {
		return err
	}
	r.detailReturnURL = ""
	r.detailURL = ""
	return nil
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

// closeCurrentPageToURL 仅在当前地址属于同站详情时关闭标签，再按完整地址返回唯一的原列表。
func closeCurrentPageToURL(ctx context.Context, browser model.Browser, returnURL string) error {
	pages, err := browser.ListPages(ctx)
	if err != nil {
		return fmt.Errorf("读取猎聘标签页失败：%w", err)
	}
	currentURL, err := currentPageURL(pages)
	if err != nil {
		return err
	}
	currentAddress, currentErr := url.Parse(currentURL)
	returnAddress, returnErr := url.Parse(returnURL)
	if currentURL != returnURL && currentErr == nil && returnErr == nil &&
		currentAddress.Host == returnAddress.Host && isResumeDetailURL(currentURL) {
		if err := browser.ClosePage(ctx); err != nil {
			return fmt.Errorf("关闭猎聘候选人详情页失败：%w", err)
		}
		pages, err = browser.ListPages(ctx)
		if err != nil {
			return fmt.Errorf("关闭详情后读取猎聘标签页失败：%w", err)
		}
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

// isResumeDetailURL 根据实际路径识别猎聘简历详情，排除仅在查询参数中出现详情地址的列表页。
func isResumeDetailURL(pageURL string) bool {
	address, err := url.Parse(pageURL)
	return err == nil && strings.HasPrefix(address.Path, "/resume/showresumedetail/")
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
	if !isResumeDetailURL(currentURL) {
		return nil
	}
	return closeCurrentPageToURL(ctx, browser, returnURL)
}
