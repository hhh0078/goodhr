// Package auto_reply 本文件把服务器扫描附件的既有PDF图片渲染结果交给已配置的AI，不再调用或检查本机OCR组件。
package auto_reply

import (
	"context"
	"fmt"
	"io"
	"os"

	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
)

// serverResumeVisionReader 只复用旧提取器的图片识别协议，实际由服务器AI客户端完成，绝不创建OCR客户端。
type serverResumeVisionReader struct {
	client *ai.Client
	config cloud.AIConfig
	prompt string
}

// Recognize 读取服务器受管附件的临时渲染图并立即识别，不接受设备路径、不把文件保存为本机业务数据。
func (r serverResumeVisionReader) Recognize(ctx context.Context, path string) (ocr.Result, error) {
	if r.client == nil {
		return ocr.Result{}, fmt.Errorf("扫描附件需要服务器AI识别，当前AI尚未准备好")
	}
	file, err := os.Open(path)
	if err != nil {
		return ocr.Result{}, fmt.Errorf("服务器扫描附件图片未能打开")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 20<<20 {
		return ocr.Result{}, fmt.Errorf("服务器扫描附件图片为空或超过大小限制")
	}
	data, err := io.ReadAll(io.LimitReader(file, 20<<20+1))
	if err != nil {
		return ocr.Result{}, fmt.Errorf("服务器扫描附件图片未能读取")
	}
	text, err := r.client.ReadDetailTextVision(ctx, r.config, [][]byte{data}, r.prompt)
	if err != nil {
		return ocr.Result{}, err
	}
	if text == "" {
		return ocr.Result{}, fmt.Errorf("扫描附件图片未识别出文字，原附件已保留")
	}
	return ocr.Result{Text: text}, nil
}
