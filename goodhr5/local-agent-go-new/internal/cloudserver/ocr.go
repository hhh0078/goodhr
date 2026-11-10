// Package cloudserver 本文件为云端扫描件提供中英文OCR，使用服务器镜像内的Tesseract，支持Linux amd64和arm64。
package cloudserver

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/integration/ocr"
)

type serverOCR struct{}

// Recognize 在服务器执行单页图片识别，参数直接传递给进程，不经过shell，不保留临时图片。
func (serverOCR) Recognize(ctx context.Context, path string) (ocr.Result, error) {
	if !filepath.IsAbs(path) {
		return ocr.Result{}, fmt.Errorf("扫描件图片路径无效")
	}
	cmd := exec.CommandContext(ctx, "tesseract", path, "stdout", "-l", "chi_sim+eng")
	data, err := cmd.Output()
	if err != nil {
		return ocr.Result{}, fmt.Errorf("服务器未能识别这页扫描件，请检查中英文 OCR 组件")
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ocr.Result{}, fmt.Errorf("这页扫描件没有读到可核实的文字")
	}
	return ocr.Result{Text: text}, nil
}
