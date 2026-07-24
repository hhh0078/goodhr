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
	"runtime"
	"time"

	"goodhr5/local-agent-go/internal/platformcore"
)

// stitchDetailScreenshot 将详情分段截图拼接成一张长图。
// exec 为平台执行器，positionID 为岗位运行 ID，screenshotsDir 为截图根目录，traceID 为详情追踪编号，candidate 为候选人，screenshot 为 Worker 截图信息。
func stitchDetailScreenshot(exec platformcore.Executor, positionID string, screenshotsDir string, traceID string, candidate map[string]any, screenshot map[string]any) map[string]any {
	operationStartedAt := time.Now()
	parts := mapList(screenshot["screenshot_parts"])
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	exec.Log("info", fmt.Sprintf(
		"详情长图拼接诊断[%s]：流程开始，候选人=%s，parts=%d，GoAlloc=%.1fMB，GoSys=%.1fMB",
		traceID,
		candidateName(candidate),
		len(parts),
		float64(memory.Alloc)/1024/1024,
		float64(memory.Sys)/1024/1024,
	))
	if len(parts) == 0 {
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：没有分段图片，直接返回 Worker 截图，耗时=%s", traceID, time.Since(operationStartedAt)))
		return screenshot
	}
	outputDir := filepath.Join(screenshotsDir, positionID)
	mkdirStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出目录开始，目录=%s", traceID, outputDir))
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出目录失败，耗时=%s，错误=%v", traceID, time.Since(mkdirStartedAt), err))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出目录完成，耗时=%s", traceID, time.Since(mkdirStartedAt)))
	outputPath := filepath.Join(outputDir, "detail-latest.png")
	if len(parts) == 1 {
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：仅一个分段，开始复制为固定截图", traceID))
		result := copySingleDetailPart(exec, traceID, parts[0], outputPath, screenshot)
		removeScreenshotParts(exec, traceID, parts, outputPath)
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段流程完成，总耗时=%s", traceID, time.Since(operationStartedAt)))
		return result
	}
	images := []image.Image{}
	for index, part := range parts {
		partNumber := index + 1
		filePath := firstNonEmpty(stringFromMap(part, "file_path"), stringFromMap(part, "path"))
		if filePath == "" {
			exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段缺少文件路径，跳过", traceID, partNumber))
			continue
		}
		statStartedAt := time.Now()
		info, statErr := os.Stat(filePath)
		if statErr != nil {
			exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段文件检查失败，路径=%s，耗时=%s，错误=%v", traceID, partNumber, filePath, time.Since(statStartedAt), statErr))
		} else {
			exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段文件检查完成，路径=%s，大小=%d，耗时=%s", traceID, partNumber, filePath, info.Size(), time.Since(statStartedAt)))
		}
		openStartedAt := time.Now()
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段打开文件开始", traceID, partNumber))
		file, err := os.Open(filePath)
		if err != nil {
			exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段打开文件失败，耗时=%s，错误=%v", traceID, partNumber, time.Since(openStartedAt), err))
			continue
		}
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段打开文件完成，耗时=%s", traceID, partNumber, time.Since(openStartedAt)))
		decodeStartedAt := time.Now()
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段 PNG 解码开始", traceID, partNumber))
		img, err := png.Decode(file)
		closeStartedAt := time.Now()
		closeErr := file.Close()
		if err != nil {
			exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：第%d段 PNG 解码失败，耗时=%s，关闭耗时=%s，关闭错误=%v，错误=%v", traceID, partNumber, time.Since(decodeStartedAt), time.Since(closeStartedAt), closeErr, err))
			continue
		}
		exec.Log("info", fmt.Sprintf(
			"详情长图拼接诊断[%s]：第%d段 PNG 解码完成，宽=%d，高=%d，解码耗时=%s，关闭耗时=%s，关闭错误=%v",
			traceID,
			partNumber,
			img.Bounds().Dx(),
			img.Bounds().Dy(),
			time.Since(decodeStartedAt),
			time.Since(closeStartedAt),
			closeErr,
		))
		images = append(images, img)
	}
	if len(images) <= 1 {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：成功解码图片不足两张，decoded=%d，降级复制第一段", traceID, len(images)))
		result := copySingleDetailPart(exec, traceID, parts[0], outputPath, screenshot)
		removeScreenshotParts(exec, traceID, parts, outputPath)
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：解码降级流程完成，总耗时=%s", traceID, time.Since(operationStartedAt)))
		return result
	}
	overlap := maxInt(0, intFromMap(screenshot, "overlap"))
	stitchStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：像素拼接开始，images=%d，overlap=%d", traceID, len(images), overlap))
	stitched := stitchImages(exec, traceID, images, overlap)
	if stitched == nil {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：像素拼接返回空，耗时=%s", traceID, time.Since(stitchStartedAt)))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：像素拼接完成，宽=%d，高=%d，耗时=%s", traceID, stitched.Bounds().Dx(), stitched.Bounds().Dy(), time.Since(stitchStartedAt)))
	createStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出文件开始，路径=%s", traceID, outputPath))
	file, err := os.Create(outputPath)
	if err != nil {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出文件失败，耗时=%s，错误=%v", traceID, time.Since(createStartedAt), err))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：创建输出文件完成，耗时=%s", traceID, time.Since(createStartedAt)))
	encodeStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：PNG 编码写入开始，宽=%d，高=%d", traceID, stitched.Bounds().Dx(), stitched.Bounds().Dy()))
	if err := png.Encode(file, stitched); err != nil {
		closeStartedAt := time.Now()
		closeErr := file.Close()
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：PNG 编码写入失败，编码耗时=%s，关闭耗时=%s，关闭错误=%v，错误=%v", traceID, time.Since(encodeStartedAt), time.Since(closeStartedAt), closeErr, err))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：PNG 编码写入完成，耗时=%s", traceID, time.Since(encodeStartedAt)))
	closeStartedAt := time.Now()
	closeErr := file.Close()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：输出文件关闭完成，耗时=%s，错误=%v", traceID, time.Since(closeStartedAt), closeErr))
	statStartedAt := time.Now()
	info, _ := os.Stat(outputPath)
	if info != nil {
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：输出文件检查完成，大小=%d，耗时=%s", traceID, info.Size(), time.Since(statStartedAt)))
	} else {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：输出文件检查未取得文件信息，耗时=%s", traceID, time.Since(statStartedAt)))
	}
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
	delete(result, "screenshot_parts")
	runtime.ReadMemStats(&memory)
	exec.Log("info", fmt.Sprintf(
		"详情截图已拼接：trace=%s parts=%d width=%d height=%d 总耗时=%s GoAlloc=%.1fMB GoSys=%.1fMB",
		traceID,
		len(images),
		stitched.Bounds().Dx(),
		stitched.Bounds().Dy(),
		time.Since(operationStartedAt),
		float64(memory.Alloc)/1024/1024,
		float64(memory.Sys)/1024/1024,
	))
	removeScreenshotParts(exec, traceID, parts, outputPath)
	return result
}

// copySingleDetailPart 将单张分段截图复制为岗位运行级固定截图文件。
// exec 为平台执行器，traceID 为详情追踪编号，part 为分段截图，outputPath 为固定输出路径，screenshot 为原始截图信息。
func copySingleDetailPart(exec platformcore.Executor, traceID string, part map[string]any, outputPath string, screenshot map[string]any) map[string]any {
	operationStartedAt := time.Now()
	source := firstNonEmpty(stringFromMap(part, "file_path"), stringFromMap(part, "path"))
	if source == "" {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：单分段复制缺少源文件路径", traceID))
		return screenshot
	}
	readStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段文件读取开始，路径=%s", traceID, source))
	data, err := os.ReadFile(source)
	if err != nil {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：单分段文件读取失败，耗时=%s，错误=%v", traceID, time.Since(readStartedAt), err))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段文件读取完成，字节=%d，耗时=%s", traceID, len(data), time.Since(readStartedAt)))
	writeStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段固定文件写入开始，路径=%s", traceID, outputPath))
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：单分段固定文件写入失败，耗时=%s，错误=%v", traceID, time.Since(writeStartedAt), err))
		return screenshot
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段固定文件写入完成，耗时=%s", traceID, time.Since(writeStartedAt)))
	statStartedAt := time.Now()
	info, _ := os.Stat(outputPath)
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：单分段输出文件检查完成，耗时=%s，文件信息存在=%v，总耗时=%s", traceID, time.Since(statStartedAt), info != nil, time.Since(operationStartedAt)))
	result := map[string]any{}
	for key, value := range screenshot {
		result[key] = value
	}
	result["file_path"] = outputPath
	result["path"] = outputPath
	if info != nil {
		result["size"] = info.Size()
	}
	result["width"] = part["width"]
	result["height"] = part["height"]
	result["stitched"] = false
	result["parts_count"] = 1
	delete(result, "screenshot_parts")
	return result
}

// removeScreenshotParts 删除详情分段截图，只保留固定输出图。
// exec 为平台执行器，traceID 为详情追踪编号，parts 为分段截图列表，keepPath 为需要保留的最终截图路径。
func removeScreenshotParts(exec platformcore.Executor, traceID string, parts []map[string]any, keepPath string) {
	operationStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：临时分段清理开始，parts=%d，保留=%s", traceID, len(parts), keepPath))
	deleted := 0
	for index, part := range parts {
		filePath := firstNonEmpty(stringFromMap(part, "file_path"), stringFromMap(part, "path"))
		if filePath == "" || filepath.Clean(filePath) == filepath.Clean(keepPath) {
			continue
		}
		removeStartedAt := time.Now()
		err := os.Remove(filePath)
		if err != nil && !os.IsNotExist(err) {
			exec.Log("warning", fmt.Sprintf("详情长图拼接诊断[%s]：第%d个临时分段删除失败，路径=%s，耗时=%s，错误=%v", traceID, index+1, filePath, time.Since(removeStartedAt), err))
			continue
		}
		deleted++
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d个临时分段删除完成，路径=%s，耗时=%s", traceID, index+1, filePath, time.Since(removeStartedAt)))
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：临时分段清理完成，删除=%d，耗时=%s", traceID, deleted, time.Since(operationStartedAt)))
}

// stitchImages 将多张 PNG 图片按重叠区域纵向拼接。
// exec 为平台执行器，traceID 为详情追踪编号，images 为分段截图，overlap 为预期重叠像素。
func stitchImages(exec platformcore.Executor, traceID string, images []image.Image, overlap int) *image.RGBA {
	if len(images) == 0 {
		return nil
	}
	convertStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第1张图片转 RGBA 开始，宽=%d，高=%d", traceID, images[0].Bounds().Dx(), images[0].Bounds().Dy()))
	result := imageToRGBA(images[0])
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第1张图片转 RGBA 完成，耗时=%s", traceID, time.Since(convertStartedAt)))
	for index := 1; index < len(images); index++ {
		partNumber := index + 1
		convertStartedAt = time.Now()
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d张图片转 RGBA 开始，宽=%d，高=%d", traceID, partNumber, images[index].Bounds().Dx(), images[index].Bounds().Dy()))
		nextImage := imageToRGBA(images[index])
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d张图片转 RGBA 完成，耗时=%s", traceID, partNumber, time.Since(convertStartedAt)))
		mergeStartedAt := time.Now()
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮图片合并开始，上图=%dx%d，下图=%dx%d，overlap=%d", traceID, index, result.Bounds().Dx(), result.Bounds().Dy(), nextImage.Bounds().Dx(), nextImage.Bounds().Dy(), overlap))
		result = mergeTwoImages(exec, traceID, index, result, nextImage, overlap)
		exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮图片合并完成，结果=%dx%d，耗时=%s", traceID, index, result.Bounds().Dx(), result.Bounds().Dy(), time.Since(mergeStartedAt)))
	}
	return result
}

// mergeTwoImages 合并上下两张截图。
// exec 为平台执行器，traceID 为详情追踪编号，round 为当前合并轮次，top 为上图，bottom 为下图，overlap 为预期重叠像素。
func mergeTwoImages(exec platformcore.Executor, traceID string, round int, top *image.RGBA, bottom *image.RGBA, overlap int) *image.RGBA {
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
	searchStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮重叠区域计算开始，stripHeight=%d，searchRange=%d，startY=%d，endY=%d", traceID, round, stripHeight, searchRange, startY, endY))
	for y := startY; y <= endY; y++ {
		diff := imageStripDiff(top, bottom, y, stripHeight)
		if diff < bestDiff {
			bestDiff = diff
			bestY = y
		}
	}
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮重叠区域计算完成，bestY=%d，bestDiff=%.3f，耗时=%s", traceID, round, bestY, bestDiff, time.Since(searchStartedAt)))
	width := maxInt(topBounds.Dx(), bottomBounds.Dx())
	height := bestY + bottomBounds.Dy()
	allocateStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮结果图内存分配开始，宽=%d，高=%d，预计RGBA=%.1fMB", traceID, round, width, height, float64(width*height*4)/1024/1024))
	merged := image.NewRGBA(image.Rect(0, 0, width, height))
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮结果图内存分配完成，耗时=%s", traceID, round, time.Since(allocateStartedAt)))
	drawStartedAt := time.Now()
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮像素绘制开始", traceID, round))
	draw.Draw(merged, merged.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(merged, image.Rect(0, 0, topBounds.Dx(), topBounds.Dy()), top, topBounds.Min, draw.Over)
	draw.Draw(merged, image.Rect(0, bestY, bottomBounds.Dx(), bestY+bottomBounds.Dy()), bottom, bottomBounds.Min, draw.Over)
	exec.Log("info", fmt.Sprintf("详情长图拼接诊断[%s]：第%d轮像素绘制完成，耗时=%s", traceID, round, time.Since(drawStartedAt)))
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
