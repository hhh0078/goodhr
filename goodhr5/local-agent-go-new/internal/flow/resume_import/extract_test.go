// Package resume_import 文件作用：用合成文档验证实际PDF文字提取、扫描页渲染、DOCX/TXT与路径边界，不调用付费服务。
package resume_import

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"goodhr5/local-agent-go-new/internal/integration/ocr"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingOCR 验证OCR确实收到可解码的本地图片，并返回虚构文字。
type recordingOCR struct {
	calls int
	t     *testing.T
}

// Recognize 检查图片真实存在，不启动OCR进程。
func (o *recordingOCR) Recognize(_ context.Context, path string) (ocr.Result, error) {
	o.t.Helper()
	file, err := os.Open(path)
	if err != nil {
		return ocr.Result{}, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return ocr.Result{}, err
	}
	if config.Width > 1600 || config.Height > 2200 {
		o.t.Fatal("PDF渲染超出尺寸上限")
	}
	o.calls++
	return ocr.Result{Text: "虚构扫描简历 13800138000"}, nil
}

// syntheticPDF 生成包含一页的完整PDF和交叉引用表，避免依赖真实候选人文件。
func syntheticPDF(content string) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, output.Len())
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return output.Bytes()
}

// TestPDFExtractionAndScanOCR 验证实际WASM读取文字与空白扫描页渲染，并清理暂存截图。
func TestPDFExtractionAndScanOCR(t *testing.T) {
	root := t.TempDir()
	reader := &recordingOCR{t: t}
	extractor := &Extractor{Root: root, OCR: reader}
	for _, test := range []struct{ name, stream, expected string }{{"text.pdf", "BT /F1 12 Tf 40 700 Td (Example Resume 13800138000) Tj ET", "Example Resume"}, {"scan.pdf", "", "虚构扫描简历"}} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name)
			if err := os.WriteFile(path, syntheticPDF(test.stream), 0600); err != nil {
				t.Fatal(err)
			}
			text, err := extractor.Extract(context.Background(), path)
			if err != nil || !strings.Contains(text, test.expected) {
				t.Fatalf("实际PDF提取失败：%q %v", text, err)
			}
		})
	}
	if reader.calls != 1 {
		t.Fatalf("文本PDF不应OCR，扫描PDF应OCR一次：%d", reader.calls)
	}
	screenshots, _ := filepath.Glob(filepath.Join(root, "resume-ocr-*.png"))
	if len(screenshots) != 0 {
		t.Fatal("PDF暂存图片未清理")
	}
}

// TestTextDOCXImageAndPathLimits 验证中文文档正文、图片OCR和目录隔离。
func TestTextDOCXImageAndPathLimits(t *testing.T) {
	root := t.TempDir()
	reader := &recordingOCR{t: t}
	extractor := &Extractor{Root: root, OCR: reader}
	path := filepath.Join(root, "resume.txt")
	_ = os.WriteFile(path, []byte("中文简历 13800138000"), 0600)
	text, err := extractor.Extract(context.Background(), path)
	if err != nil || !strings.Contains(text, "中文简历") {
		t.Fatalf("TXT：%q %v", text, err)
	}
	var doc bytes.Buffer
	archive := zip.NewWriter(&doc)
	entry, _ := archive.Create("word/document.xml")
	_, _ = entry.Write([]byte(`<w:document xmlns:w="urn:word"><w:body><w:p><w:r><w:t>中文Word简历</w:t></w:r></w:p></w:body></w:document>`))
	_ = archive.Close()
	path = filepath.Join(root, "resume.docx")
	_ = os.WriteFile(path, doc.Bytes(), 0600)
	text, err = extractor.Extract(context.Background(), path)
	if err != nil || !strings.Contains(text, "中文Word简历") {
		t.Fatalf("DOCX：%q %v", text, err)
	}
	path = filepath.Join(root, "resume.png")
	file, _ := os.Create(path)
	_ = png.Encode(file, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	_ = file.Close()
	if _, err := extractor.Extract(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := extractor.Extract(context.Background(), filepath.Join(t.TempDir(), "outside.txt")); err == nil {
		t.Fatal("外部目录被读取")
	}
	path = filepath.Join(root, "fake.png")
	_ = os.WriteFile(path, []byte("not image"), 0600)
	if _, err := extractor.Extract(context.Background(), path); err == nil {
		t.Fatal("伪图片被接受")
	}
}

// TestPDFCancellationStopsWASM 验证取消的context可以终止PDF编译或调用，不无限占用工作槽。
func TestPDFCancellationStopsWASM(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "resume.pdf")
	_ = os.WriteFile(path, syntheticPDF(""), 0600)
	extractor := &Extractor{Root: root}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := extractor.Extract(ctx, path); err == nil {
		t.Fatal("取消后仍成功解析")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("取消没及时终止WASM")
	}
}
