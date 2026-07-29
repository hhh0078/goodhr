// Package greeting 文件作用：验证详情分段截图按重叠像素拼接且不会重复内容。
package greeting

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
)

// TestStitchScreenshotPartsMergesOverlap 验证两张包含 30 像素重叠的图片会拼成 170 像素高的长图。
func TestStitchScreenshotPartsMergesOverlap(t *testing.T) {
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first.png")
	secondPath := filepath.Join(directory, "second.png")
	writeStitchTestImage(t, firstPath, 0)
	writeStitchTestImage(t, secondPath, 70)
	outputPath := filepath.Join(directory, "stitched.png")
	part, err := stitchScreenshotParts([]contract.ScreenshotPart{
		{Path: firstPath, Filename: "first.png", ScrollPosition: 0},
		{Path: secondPath, Filename: "second.png", ScrollPosition: 70},
	}, outputPath)
	if err != nil {
		t.Fatalf("拼接详情截图失败：%v", err)
	}
	file, err := os.Open(part.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bounds().Dy() != 170 {
		t.Fatalf("拼接高度=%d，期望=170", result.Bounds().Dy())
	}
	if _, err = os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("拼接成功后原始分段没有删除：%v", err)
	}
}

// writeStitchTestImage 写入每行颜色唯一、可稳定识别重叠区域的测试 PNG。
func writeStitchTestImage(t *testing.T, filePath string, rowOffset int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 80, 100))
	for y := 0; y < 100; y++ {
		value := uint8((y + rowOffset) % 255)
		for x := 0; x < 80; x++ {
			img.SetRGBA(x, y, color.RGBA{R: value, G: uint8(x), B: value / 2, A: 255})
		}
	}
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}
