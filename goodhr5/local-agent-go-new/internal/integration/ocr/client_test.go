// Package ocr 文件作用：验证 OCR 稳定错误码可以区分组件故障和单图无文字。
package ocr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestReadyDarwinVisionWithoutModel 验证 macOS 内置 Vision 不需要 RapidOCR 模型但需要可执行文件。
func TestReadyDarwinVisionWithoutModel(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("只检查 macOS Vision 组件")
	}
	path := filepath.Join(t.TempDir(), "RapidOCR-json")
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := New(path)
	if err := client.Ready(); err != nil {
		t.Fatalf("Vision 组件不应要求模型：%v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(); !IsUnavailable(err) {
		t.Fatalf("不可执行的 Vision 组件应报 OCR_UNAVAILABLE：%v", err)
	}
}

// TestDarwinVisionOCRProtocol 使用真实 Mac OCR 二进制验证 Go 逐行请求和常驻进程复用。
func TestDarwinVisionOCRProtocol(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("只在 macOS 验证 Vision OCR")
	}
	binary := os.Getenv("GOODHR_VISION_OCR_BINARY")
	image := os.Getenv("GOODHR_VISION_OCR_TEST_IMAGE")
	if binary == "" || image == "" {
		t.Skip("设置 GOODHR_VISION_OCR_BINARY 和 GOODHR_VISION_OCR_TEST_IMAGE 后运行")
	}
	client := New(binary)
	defer client.Close()
	if err := client.Ready(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		result, err := client.Recognize(ctx, image)
		if err != nil || !strings.Contains(result.Text, "招聘经理") {
			t.Fatalf("第 %d 次中文识别失败：text=%q err=%v", attempt+1, result.Text, err)
		}
	}
}

// TestErrorClassification 验证 OCR 错误分类不会依赖中文字符串判断。
func TestErrorClassification(t *testing.T) {
	unavailable := &Error{Code: ErrorUnavailable, Message: "组件不可用"}
	noText := &Error{Code: ErrorNoText, Message: "没有文字"}
	if !IsUnavailable(unavailable) || IsUnavailable(noText) {
		t.Fatal("OCR 组件故障分类不正确")
	}
	if !IsNoText(noText) || IsNoText(unavailable) {
		t.Fatal("OCR 无文字分类不正确")
	}
	if IsUnavailable(errors.New("OCR 组件不可用")) {
		t.Fatal("普通文本错误不应伪装成稳定 OCR 错误码")
	}
}

// TestRecognizeRestartsExitedProcess 验证首次识别崩溃可恢复，连续崩溃最多重试一次，正常请求复用进程。
func TestRecognizeRestartsExitedProcess(t *testing.T) {
	for _, mode := range []string{"once", "always"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(self)
			if err != nil {
				t.Fatal(err)
			}
			executable := filepath.Join(root, "ocr-test"+filepath.Ext(self))
			if err := os.WriteFile(executable, content, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(root, "models"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(root, "models", "ch_PP-OCRv3_det_infer.onnx"), filepath.Join(root, "image.png")} {
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GOODHR_OCR_TEST_MODE", mode)
			t.Setenv("GOODHR_OCR_TEST_MARKER", filepath.Join(root, "starts"))
			t.Setenv("GOODHR_OCR_ARGS", "-test.run=TestOCRHelperProcess")
			events := make(chan []byte, 8)
			client := New(executable, func(line []byte) { events <- line })
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			result, err := client.Recognize(ctx, filepath.Join(root, "image.png"))
			if mode == "always" {
				if !IsUnavailable(err) {
					t.Fatalf("连续崩溃应报错：%v", err)
				}
			} else {
				if err != nil || result.Text != "本科销售" {
					t.Fatalf("重启未恢复：%+v %v", result, err)
				}
				if _, err := client.Recognize(ctx, filepath.Join(root, "image.png")); err != nil {
					t.Fatal(err)
				}
			}
			starts, err := os.ReadFile(filepath.Join(root, "starts"))
			if err != nil || string(starts) != "xx" {
				t.Fatalf("启动次数不符：%q %v", starts, err)
			}
			select {
			case line := <-events:
				var event map[string]any
				if json.Unmarshal(line, &event) != nil || event["action"] != "ocr.exit" || event["exit_code"] != float64(7) || event["category"] != "异常退出" {
					t.Fatalf("OCR 原始退出信息不完整：%s", line)
				}
				if !strings.Contains(string(line), "native OCR fault") {
					t.Fatalf("OCR stderr 丢失：%s", line)
				}
			case <-time.After(time.Second):
				t.Fatal("OCR 退出没有留下详细日志")
			}
		})
	}
}

// TestOCRHelperProcess 模拟 JSON 行 OCR 程序，仅在子进程测试标记存在时执行。
func TestOCRHelperProcess(t *testing.T) {
	mode := os.Getenv("GOODHR_OCR_TEST_MODE")
	if mode == "" {
		return
	}
	marker := os.Getenv("GOODHR_OCR_TEST_MARKER")
	previous, _ := os.ReadFile(marker)
	if err := os.WriteFile(marker, append(previous, 'x'), 0o600); err != nil {
		os.Exit(3)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if mode == "always" || len(previous) == 0 {
			_, _ = os.Stderr.WriteString("native OCR fault: missing model runtime\n")
			os.Exit(7)
		}
		_, _ = os.Stdout.WriteString("{\"text\":\"本科销售\"}\n")
	}
	os.Exit(0)
}

// TestParseOCRText 验证顶层和嵌套 RapidOCR 文字都能提取，坐标等字段不会混入正文。
func TestParseOCRText(t *testing.T) {
	raw := json.RawMessage(`{
		"data": [
			{"text": "第一行", "score": 0.99},
			{"result": [{"label": "第二行"}]}
		],
		"message": "success"
	}`)
	text, err := parseOCRText(raw)
	if err != nil {
		t.Fatalf("解析 OCR 文字失败：%v", err)
	}
	if text != "第一行\n第二行" {
		t.Fatalf("OCR 文字不正确：%q", text)
	}
}

// TestParseOCRTextNoText 验证没有文字字段时返回稳定的单图无文字错误。
func TestParseOCRTextNoText(t *testing.T) {
	_, err := parseOCRText(json.RawMessage(`{"code": 100, "data": []}`))
	if !IsNoText(err) {
		t.Fatalf("预期 OCR_NO_TEXT，实际：%v", err)
	}
}

// TestResolveExecutableFindsNestedInstall 验证 OCR 安装包多一层目录时客户端仍能找到可执行文件。
func TestResolveExecutableFindsNestedInstall(t *testing.T) {
	root := t.TempDir()
	nestedExecutable := filepath.Join(root, "RapidOCR-release", "RapidOCR-json")
	if err := os.MkdirAll(filepath.Dir(nestedExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedExecutable, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := New(filepath.Join(root, "RapidOCR-json"))
	resolved, err := client.resolveExecutable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nestedExecutable {
		t.Fatalf("resolveExecutable() = %s, want %s", resolved, nestedExecutable)
	}
}

// TestResolveExecutableFindsWindowsEXE 验证 Windows 已安装的 exe 和模型可直接用于 OCR 启动检查。
func TestResolveExecutableFindsWindowsEXE(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows RapidOCR 检查只在 Windows 运行")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "RapidOCR-json.exe")
	if err := os.MkdirAll(filepath.Join(root, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{executable, filepath.Join(root, "models", "ch_PP-OCRv3_det_infer.onnx")} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	client := New(filepath.Join(root, "RapidOCR-json"))
	resolved, err := client.resolveExecutable()
	if err != nil || resolved != executable {
		t.Fatalf("resolved=%s err=%v", resolved, err)
	}
	if err := client.Ready(); err != nil {
		t.Fatal(err)
	}
}
