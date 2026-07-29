// Package greeting 文件作用：把真实滚轮生成的详情分段 PNG 拼接成供 OCR 或多模态 AI 使用的完整长图。
package greeting

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// stitchScreenshotParts 按相邻截图的重叠区域拼接长图，成功后删除原始分段。
func stitchScreenshotParts(parts []contract.ScreenshotPart, outputPath string) (contract.ScreenshotPart, error) {
	if len(parts) == 0 {
		return contract.ScreenshotPart{}, fmt.Errorf("没有可拼接的详情截图")
	}
	images := make([]*image.RGBA, 0, len(parts))
	for _, part := range parts {
		file, err := os.Open(part.Path)
		if err != nil {
			return contract.ScreenshotPart{}, fmt.Errorf("打开截图 %s 失败：%w", part.Filename, err)
		}
		decoded, decodeErr := png.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			return contract.ScreenshotPart{}, fmt.Errorf("读取截图 %s 失败：%w", part.Filename, decodeErr)
		}
		if closeErr != nil {
			return contract.ScreenshotPart{}, fmt.Errorf("关闭截图 %s 失败：%w", part.Filename, closeErr)
		}
		images = append(images, imageToRGBA(decoded))
	}
	stitched := images[0]
	for index := 1; index < len(images); index++ {
		scrollDistance := parts[index].ScrollPosition - parts[index-1].ScrollPosition
		expectedOverlap := max(images[index-1].Bounds().Dy()-scrollDistance, 0)
		stitched = mergeScreenshotParts(stitched, images[index], expectedOverlap)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return contract.ScreenshotPart{}, fmt.Errorf("创建长图目录失败：%w", err)
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return contract.ScreenshotPart{}, fmt.Errorf("创建详情长图失败：%w", err)
	}
	if err = png.Encode(file, stitched); err != nil {
		_ = file.Close()
		return contract.ScreenshotPart{}, fmt.Errorf("写入详情长图失败：%w", err)
	}
	if err = file.Close(); err != nil {
		return contract.ScreenshotPart{}, fmt.Errorf("关闭详情长图失败：%w", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return contract.ScreenshotPart{}, fmt.Errorf("检查详情长图失败：%w", err)
	}
	for _, part := range parts {
		if filepath.Clean(part.Path) != filepath.Clean(outputPath) {
			_ = os.Remove(part.Path)
		}
	}
	return contract.ScreenshotPart{
		Path: outputPath, Filename: filepath.Base(outputPath), Size: info.Size(),
		Index: 0, ScrollPosition: 0,
	}, nil
}

// mergeScreenshotParts 根据预期重叠高度附近的像素差，纵向合并两张详情截图。
func mergeScreenshotParts(top *image.RGBA, bottom *image.RGBA, expectedOverlap int) *image.RGBA {
	stripHeight := min(30, bottom.Bounds().Dy())
	searchStart := max(top.Bounds().Dy()-expectedOverlap-80, 0)
	searchEnd := min(top.Bounds().Dy()-stripHeight, top.Bounds().Dy()-expectedOverlap+80)
	if searchEnd < searchStart {
		searchStart = max(top.Bounds().Dy()-stripHeight, 0)
		searchEnd = searchStart
	}
	bestY := searchStart
	bestDiff := math.MaxFloat64
	for y := searchStart; y <= searchEnd; y++ {
		if diff := screenshotStripDiff(top, bottom, y, stripHeight); diff < bestDiff {
			bestDiff = diff
			bestY = y
		}
	}
	width := max(top.Bounds().Dx(), bottom.Bounds().Dx())
	merged := image.NewRGBA(image.Rect(0, 0, width, bestY+bottom.Bounds().Dy()))
	draw.Draw(merged, merged.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(merged, image.Rect(0, 0, top.Bounds().Dx(), top.Bounds().Dy()), top, top.Bounds().Min, draw.Over)
	draw.Draw(merged, image.Rect(0, bestY, bottom.Bounds().Dx(), bestY+bottom.Bounds().Dy()), bottom, bottom.Bounds().Min, draw.Over)
	return merged
}

// screenshotStripDiff 计算上图指定条带与下图顶部条带的平均 RGB 差异。
func screenshotStripDiff(top *image.RGBA, bottom *image.RGBA, topY int, height int) float64 {
	width := min(top.Bounds().Dx(), bottom.Bounds().Dx())
	if width <= 0 || height <= 0 {
		return math.MaxFloat64
	}
	step := max(width/120, 1)
	total := 0.0
	count := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x += step {
			topColor := top.RGBAAt(x, topY+y)
			bottomColor := bottom.RGBAAt(x, y)
			total += math.Abs(float64(topColor.R)-float64(bottomColor.R)) +
				math.Abs(float64(topColor.G)-float64(bottomColor.G)) +
				math.Abs(float64(topColor.B)-float64(bottomColor.B))
			count += 3
		}
	}
	return total / float64(count)
}

// imageToRGBA 把任意 PNG 解码结果转换为从零坐标开始的 RGBA 图片。
func imageToRGBA(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), source, bounds.Min, draw.Src)
	return result
}
