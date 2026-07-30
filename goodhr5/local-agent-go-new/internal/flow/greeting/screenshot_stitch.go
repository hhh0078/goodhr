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

// stitchScreenshotParts 按相邻截图的重叠区域拼接长图，并保留原始分段供当前任务核对。
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
		stitched = mergeScreenshotParts(
			stitched,
			images[index],
			images[index-1].Bounds().Dy(),
		)
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
	return contract.ScreenshotPart{
		Path: outputPath, Filename: filepath.Base(outputPath), Size: info.Size(),
		Index: 0, ScrollPosition: 0,
	}, nil
}

// mergeScreenshotParts 根据两张图片的实际像素重叠纵向合并，不依赖 CSS 滚轮距离。
func mergeScreenshotParts(top *image.RGBA, bottom *image.RGBA, previousHeight int) *image.RGBA {
	topHeight := top.Bounds().Dy()
	bottomHeight := bottom.Bounds().Dy()
	maxOverlap := min(max(previousHeight, 1), bottomHeight)
	minOverlap := max(12, min(maxOverlap/12, 60))
	searchStart := max(topHeight-maxOverlap, 0)
	searchEnd := max(topHeight-minOverlap, searchStart)
	expectedY := topHeight - maxOverlap/4
	bestY := searchStart
	bestDiff := math.MaxFloat64
	for y := searchStart; y <= searchEnd; y++ {
		overlap := min(topHeight-y, bottomHeight)
		diff := screenshotOverlapDiff(top, bottom, y, overlap)
		if diff < bestDiff-0.001 ||
			(math.Abs(diff-bestDiff) <= 0.001 && absInt(y-expectedY) < absInt(bestY-expectedY)) {
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

// screenshotOverlapDiff 计算候选重叠区域内多行采样像素的平均 RGB 差异。
func screenshotOverlapDiff(top *image.RGBA, bottom *image.RGBA, topY int, height int) float64 {
	width := min(top.Bounds().Dx(), bottom.Bounds().Dx())
	if width <= 0 || height <= 0 {
		return math.MaxFloat64
	}
	stepX := max(width/120, 1)
	stepY := max(height/80, 1)
	total := 0.0
	count := 0
	for y := 0; y < height; y += stepY {
		for x := 0; x < width; x += stepX {
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

// absInt 返回整数绝对值。
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// imageToRGBA 把任意 PNG 解码结果转换为从零坐标开始的 RGBA 图片。
func imageToRGBA(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), source, bounds.Min, draw.Src)
	return result
}
