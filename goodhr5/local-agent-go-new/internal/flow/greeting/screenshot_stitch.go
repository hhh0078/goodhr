// Package greeting 本文件保留旧详情拼图入口并复用平台公共图片算法，原打招呼的图片结果与调用方式保持不变。
package greeting

import (
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
)

// stitchScreenshotParts 复用同一套像素重叠算法拼接真实滚轮分段。
func stitchScreenshotParts(parts []contract.ScreenshotPart, outputPath string) (contract.ScreenshotPart, error) {
	return common.StitchScreenshotParts(parts, outputPath)
}
