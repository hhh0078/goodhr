// Package client 本文件把截图操作的已观测阶段与 Playwright 原生超时写入岗位诊断，排除图片和路径。
package client

import (
	"encoding/json"
	"errors"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

// screenshotFailure 保留原错误，仅在 Worker 观测到具体步骤或五秒原生超时时细化说明。
func screenshotFailure(err error, operation string, started time.Time) error {
	if err == nil {
		return nil
	}
	d := failurediag.Detail{Source: "浏览器截图", Operation: operation, Phase: "等待截图操作结果", Reason: "未取得完整图片", ElapsedMS: time.Since(started).Milliseconds()}
	var workerErr *contract.WorkerError
	var evidence struct {
		Request          contract.RequestDiagnostic `json:"request_diagnostic"`
		CaptureTimeoutMS int                        `json:"capture_timeout_ms"`
	}
	if errors.As(err, &workerErr) {
		_ = json.Unmarshal(workerErr.Body.Details, &evidence)
	} else {
		var transport interface {
			BrowserRequestDiagnostic() contract.RequestDiagnostic
		}
		if errors.As(err, &transport) {
			evidence.Request = transport.BrowserRequestDiagnostic()
		}
	}
	switch evidence.Request.WorkerPhase {
	case "require_page":
		d.Phase = "确认活动页面"
	case "screenshot_prepare":
		d.Phase = "准备截图区域"
	case "screenshot_capture":
		d.Phase = "截取页面画面"
	case "screenshot_top":
		d.Phase = "滚动到详情顶部"
	case "screenshot_save":
		d.Phase = "保存截图分段"
	case "screenshot_advance":
		d.Phase = "滚动到下一段"
	}
	if workerErr != nil && workerErr.Body.Code == "SCREENSHOT_FAILED" && evidence.CaptureTimeoutMS == 5000 {
		d.LimitMS = 5000
		d.Reason = "浏览器截图调用超时"
		d.Phase = "截取页面画面"
	}
	return failurediag.Wrap(err, d)
}
