// Package resume_import 文件作用：本地读取TXT、DOCX和图片，限制解压与图片大小，图片只交给本地OCR。
package resume_import

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// OCRReader 复用本地OCR能力，测试不需要安装模型或连接付费服务。
type OCRReader interface {
	Recognize(context.Context, string) (ocr.Result, error)
}

// Extractor 只读取导入目录内的文件，PDF渲染器不挂载本机文件系统。
type Extractor struct {
	Root string
	OCR  OCRReader
}

// Extract 限制路径与文件大小后进行本地解析；单份最多五分钟，可随任务取消。
func (e *Extractor) Extract(ctx context.Context, path string) (string, error) {
	rel, err := filepath.Rel(e.Root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("这份文件不在导入目录内")
	}
	realRoot, rootErr := filepath.EvalSymlinks(e.Root)
	realPath, pathErr := filepath.EvalSymlinks(path)
	if rootErr != nil || pathErr != nil {
		return "", fmt.Errorf("原文件没找到，请重新导入")
	}
	realRel, relErr := filepath.Rel(realRoot, realPath)
	if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) || filepath.IsAbs(realRel) {
		return "", fmt.Errorf("这份文件不在导入目录内")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("原文件没找到，请重新导入")
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxFileBytes {
		return "", fmt.Errorf("文件为空或超过10MB，请重新选择")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var text string
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		text, err = decodeText(data)
	case ".docx":
		text, err = extractDOCX(ctx, path)
	case ".pdf":
		text, err = e.extractPDF(ctx, path)
	case ".png", ".jpg", ".jpeg":
		text, err = e.extractImage(ctx, path)
	default:
		err = fmt.Errorf("这个格式暂不支持，请换成PDF、DOCX、TXT或图片")
	}
	if err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("文件里暂时没读到文字，请换一份更清晰的文件")
	}
	if len([]rune(text)) > 200000 {
		return "", fmt.Errorf("文字超过20万字，请分开导入")
	}
	return text, nil
}

// decodeText 接受UTF-8、带BOM的UTF-16和常见中文GB18030文本。
func decodeText(data []byte) (string, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if len(data) >= 2 && (bytes.Equal(data[:2], []byte{0xff, 0xfe}) || bytes.Equal(data[:2], []byte{0xfe, 0xff})) {
		decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(data)
		return string(decoded), err
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if err != nil {
		return "", fmt.Errorf("文字编码没认出来，请另存为UTF-8")
	}
	return string(decoded), nil
}

// extractDOCX 仅处理Word正文XML，不执行宏、不访问外链，限制解压后的正文大小。
func extractDOCX(ctx context.Context, path string) (string, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("DOCX没读出来，请检查文件是否完整")
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.Name != "word/document.xml" {
			continue
		}
		if entry.UncompressedSize64 > 20<<20 {
			return "", fmt.Errorf("Word正文太大，请分开导入")
		}
		file, err := entry.Open()
		if err != nil {
			return "", err
		}
		defer file.Close()
		decoder := xml.NewDecoder(io.LimitReader(file, 20<<20+1))
		var result strings.Builder
		inText := false
		for {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("Word正文没读出来，请重新导出DOCX")
			}
			switch value := token.(type) {
			case xml.StartElement:
				if value.Name.Local == "t" {
					inText = true
				}
				if value.Name.Local == "tab" {
					result.WriteString(" ")
				}
				if value.Name.Local == "br" {
					result.WriteString("\n")
				}
			case xml.EndElement:
				if value.Name.Local == "t" {
					inText = false
				}
				if value.Name.Local == "p" {
					result.WriteString("\n")
				}
			case xml.CharData:
				if inText {
					result.Write(value)
				}
			}
			if result.Len() > 2<<20 {
				return "", fmt.Errorf("Word文字太多，请分开导入")
			}
		}
		return result.String(), nil
	}
	return "", fmt.Errorf("没找到Word正文，请重新导出DOCX")
}

// extractImage 校验真实图片编码与像素数再调用本地OCR，避免巨大解码占用。
func (e *Extractor) extractImage(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	config, format, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil || (format != "png" && format != "jpeg") {
		return "", fmt.Errorf("图片没读出来，请换成PNG或JPEG")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 25_000_000 {
		return "", fmt.Errorf("图片超过2500万像素，请缩小后导入")
	}
	if e.OCR == nil {
		return "", fmt.Errorf("本地OCR还没准备好，请在本地程序里安装OCR后重试")
	}
	result, err := e.OCR.Recognize(ctx, path)
	return result.Text, err
}
