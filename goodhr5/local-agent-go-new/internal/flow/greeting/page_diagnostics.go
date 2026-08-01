// Package greeting 文件作用：记录主动打招呼各步骤所在的浏览器标签页，帮助定位页面串台问题。
package greeting

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/shared"
)

// reportPageDiagnostics 记录当前标签页和全部标签页的安全地址，不让诊断失败影响候选人流程。
func (f *Flow) reportPageDiagnostics(ctx context.Context, taskID string, stage string) {
	diagnosticCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	pages, err := f.Browser.ListPages(diagnosticCtx)
	if err != nil {
		shared.ReportProgress(f.Logger, taskID, fmt.Sprintf("页面诊断（%s）：标签页读取失败：%v", stage, err))
		return
	}
	current := "没有活动标签页"
	items := make([]string, 0, len(pages.Pages))
	for _, page := range pages.Pages {
		address := safeDiagnosticPageAddress(page.URL)
		items = append(items, fmt.Sprintf("%s=%s", page.PageID, address))
		if page.Current {
			current = fmt.Sprintf("%s=%s", page.PageID, address)
		}
	}
	shared.ReportProgress(
		f.Logger,
		taskID,
		fmt.Sprintf("页面诊断（%s）：当前 %s；全部 %d 个 [%s]", stage, current, len(items), strings.Join(items, "，")),
	)
}

// safeDiagnosticPageAddress 移除页面查询参数，只保留诊断所需的协议、域名、路径和片段。
func safeDiagnosticPageAddress(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "地址暂时读不到"
	}
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	return parsed.String()
}
