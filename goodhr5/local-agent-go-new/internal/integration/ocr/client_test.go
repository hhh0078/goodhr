// Package ocr 文件作用：验证 OCR 稳定错误码可以区分组件故障和单图无文字。
package ocr

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
