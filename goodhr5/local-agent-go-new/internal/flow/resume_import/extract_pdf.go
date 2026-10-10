// Package resume_import 文件作用：使用嵌入式WASM PDFium在本地提取PDF文字，扫描页渲染成图片后交给本地OCR。
package resume_import

import (
	"context"
	"fmt"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"image/png"
	"io"
	"os"
	"strings"
)

// pdfSlot 串行保护PDFium第三方包的全局实例索引，同时限制大文件渲染的总内存占用。
var pdfSlot = make(chan struct{}, 1)

// extractPDF 限制PDF最多20页、WASM内存512MB；不给渲染器挂载文件系统，取消可终止WASM执行。
func (e *Extractor) extractPDF(ctx context.Context, path string) (string, error) {
	select {
	case pdfSlot <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-pdfSlot }()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pool, err := webassembly.Init(webassembly.Config{Context: ctx, MaxTotal: 1, MaxIdle: 1, FSConfig: wazero.NewFSConfig(), RuntimeConfig: wazero.NewRuntimeConfig().WithMemoryLimitPages(8192).WithCloseOnContextDone(true), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		return "", fmt.Errorf("本地PDF解析器还没准备好：%w", err)
	}
	defer pool.Close()
	instance, err := pool.GetInstanceWithContext(ctx)
	if err != nil {
		return "", err
	}
	defer instance.Close()
	document, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return "", fmt.Errorf("PDF没读出来，可能已加密或文件损坏，请解密后再试")
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: document.Document})
	count, err := instance.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: document.Document})
	if err != nil {
		return "", err
	}
	if count.PageCount < 1 || count.PageCount > 20 {
		return "", fmt.Errorf("每份PDF最多20页，请拆开后再导入")
	}
	var text strings.Builder
	for index := 0; index < count.PageCount; index++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		page := requests.Page{ByIndex: &requests.PageByIndex{Document: document.Document, Index: index}}
		result, err := instance.GetPageText(&requests.GetPageText{Page: page})
		if err != nil {
			return "", fmt.Errorf("PDF第%d页暂时没读出来", index+1)
		}
		pageText := strings.TrimSpace(result.Text)
		if len([]rune(pageText)) < 20 {
			if e.OCR == nil {
				return "", fmt.Errorf("PDF第%d页需要OCR，请先安装本地OCR后重试", index+1)
			}
			rendered, err := instance.RenderPageInPixels(&requests.RenderPageInPixels{Page: page, Width: 1600, Height: 2200})
			if err != nil {
				return "", fmt.Errorf("PDF第%d页没转成图片，请检查文件", index+1)
			}
			file, err := os.CreateTemp(e.Root, "resume-ocr-*.png")
			if err != nil {
				rendered.Cleanup()
				return "", err
			}
			imagePath := file.Name()
			encodeErr := png.Encode(file, rendered.Result.Image)
			rendered.Cleanup()
			closeErr := file.Close()
			if encodeErr != nil || closeErr != nil {
				_ = os.Remove(imagePath)
				return "", fmt.Errorf("PDF图片暂存失败，请检查磁盘")
			}
			ocrResult, ocrErr := e.OCR.Recognize(ctx, imagePath)
			_ = os.Remove(imagePath)
			if ocrErr != nil {
				return "", fmt.Errorf("PDF第%d页OCR没处理成功：%w", index+1, ocrErr)
			}
			pageText = ocrResult.Text
		}
		text.WriteString(pageText)
		text.WriteString("\n")
		if text.Len() > 2<<20 {
			return "", fmt.Errorf("PDF文字太多，请分开导入")
		}
	}
	return text.String(), nil
}
