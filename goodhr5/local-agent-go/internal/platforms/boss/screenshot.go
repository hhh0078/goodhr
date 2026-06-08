// Package boss 负责 Boss 详情截图拼接。
package boss

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"goodhr5/local-agent-go/internal/platformcore"
)

// stitchDetailScreenshot 将详情分段截图拼接成一张长图。
// exec 为平台执行器，taskID 为任务 ID，screenshotsDir 为截图根目录，candidate 为候选人，screenshot 为 Worker 截图信息。
func stitchDetailScreenshot(exec platformcore.Executor, taskID string, screenshotsDir string, candidate map[string]any, screenshot map[string]any) map[string]any {
	parts := mapList(screenshot["screenshot_parts"])
	if len(parts) <= 1 {
		return screenshot
	}
	images := []image.Image{}
	for _, part := range parts {
		filePath := firstNonEmpty(stringFromMap(part, "file_path"), stringFromMap(part, "path"))
		if filePath == "" {
			continue
		}
		file, err := os.Open(filePath)
		if err != nil {
			exec.Log("warning", "打开详情分段截图失败："+err.Error())
			continue
		}
		img, err := png.Decode(file)
		_ = file.Close()
		if err != nil {
			exec.Log("warning", "解析详情分段截图失败："+err.Error())
			continue
		}
		images = append(images, img)
	}
	if len(images) <= 1 {
		return screenshot
	}
	overlap := maxInt(0, intFromMap(screenshot, "overlap"))
	stitched := stitchImages(images, overlap)
	if stitched == nil {
		return screenshot
	}
	outputDir := filepath.Join(screenshotsDir, taskID)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		exec.Log("warning", "创建详情长图目录失败："+err.Error())
		return screenshot
	}
	filename := fmt.Sprintf("detail-%s-stitched.png", safePathName(stringFromMap(candidate, "id")))
	outputPath := filepath.Join(outputDir, filename)
	file, err := os.Create(outputPath)
	if err != nil {
		exec.Log("warning", "创建详情长图失败："+err.Error())
		return screenshot
	}
	if err := png.Encode(file, stitched); err != nil {
		_ = file.Close()
		exec.Log("warning", "保存详情长图失败："+err.Error())
		return screenshot
	}
	_ = file.Close()
	info, _ := os.Stat(outputPath)
	result := map[string]any{}
	for key, value := range screenshot {
		result[key] = value
	}
	result["file_path"] = outputPath
	result["path"] = outputPath
	if info != nil {
		result["size"] = info.Size()
	}
	result["width"] = stitched.Bounds().Dx()
	result["height"] = stitched.Bounds().Dy()
	result["stitched"] = true
	result["parts_count"] = len(images)
	exec.Log("info", fmt.Sprintf("详情截图已拼接：parts=%d width=%d height=%d", len(images), stitched.Bounds().Dx(), stitched.Bounds().Dy()))
	return result
}

// stitchImages 将多张 PNG 图片按重叠区域纵向拼接。
// images 为分段截图，overlap 为预期重叠像素。
func stitchImages(images []image.Image, overlap int) *image.RGBA {
	if len(images) == 0 {
		return nil
	}
	result := imageToRGBA(images[0])
	for index := 1; index < len(images); index++ {
		result = mergeTwoImages(result, imageToRGBA(images[index]), overlap)
	}
	return result
}

// mergeTwoImages 合并上下两张截图。
// top 为上图，bottom 为下图，overlap 为预期重叠像素。
func mergeTwoImages(top *image.RGBA, bottom *image.RGBA, overlap int) *image.RGBA {
	topBounds := top.Bounds()
	bottomBounds := bottom.Bounds()
	stripHeight := minInt(30, bottomBounds.Dy()-1)
	if stripHeight <= 0 {
		stripHeight = 1
	}
	searchRange := minInt(maxInt(overlap+50, stripHeight), minInt(topBounds.Dy()-1, bottomBounds.Dy()-1))
	bestY := maxInt(topBounds.Dy()-overlap, 0)
	bestDiff := math.MaxFloat64
	startY := maxInt(topBounds.Dy()-searchRange, 0)
	endY := maxInt(topBounds.Dy()-stripHeight, startY)
	for y := startY; y <= endY; y++ {
		diff := imageStripDiff(top, bottom, y, stripHeight)
		if diff < bestDiff {
			bestDiff = diff
			bestY = y
		}
	}
	width := maxInt(topBounds.Dx(), bottomBounds.Dx())
	height := bestY + bottomBounds.Dy()
	merged := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(merged, merged.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(merged, image.Rect(0, 0, topBounds.Dx(), topBounds.Dy()), top, topBounds.Min, draw.Over)
	draw.Draw(merged, image.Rect(0, bestY, bottomBounds.Dx(), bestY+bottomBounds.Dy()), bottom, bottomBounds.Min, draw.Over)
	return merged
}

// imageStripDiff 计算两张图重叠条带的像素差异。
// top 为上图，bottom 为下图，topY 为上图条带起点，height 为条带高度。
func imageStripDiff(top *image.RGBA, bottom *image.RGBA, topY int, height int) float64 {
	width := minInt(top.Bounds().Dx(), bottom.Bounds().Dx())
	if width <= 0 || height <= 0 {
		return math.MaxFloat64
	}
	step := maxInt(width/120, 1)
	var total float64
	var count float64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x += step {
			a := top.RGBAAt(x, topY+y)
			b := bottom.RGBAAt(x, y)
			total += math.Abs(float64(a.R)-float64(b.R)) + math.Abs(float64(a.G)-float64(b.G)) + math.Abs(float64(a.B)-float64(b.B))
			count += 3
		}
	}
	if count == 0 {
		return math.MaxFloat64
	}
	return total / count
}

// imageToRGBA 将图片转换为 RGBA。
// img 为原始图片。
func imageToRGBA(img image.Image) *image.RGBA {
	bounds := img.Bounds()
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(result, result.Bounds(), img, bounds.Min, draw.Src)
	return result
}

// maxInt 返回较大整数。
// a 和 b 为比较值。
func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

// minInt 返回较小整数。
// a 和 b 为比较值。
func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

// safePathName 清理文件名中的危险字符。
// value 为原始名称。
func safePathName(value string) string {
	value = normalizeText(value)
	if value == "" {
		return "default"
	}
	result := ""
	for _, item := range value {
		if item >= 'a' && item <= 'z' || item >= 'A' && item <= 'Z' || item >= '0' && item <= '9' || item == '-' || item == '_' || item == '.' {
			result += string(item)
			continue
		}
		result += "_"
	}
	if result == "" {
		return "default"
	}
	if len(result) > 80 {
		return result[:80]
	}
	return result
}
