// Package hliepin 文件作用：验证猎聘猎头端候选人稳定文本和聊天姓名保护规则。
package hliepin

import (
	"strings"
	"testing"
)

// TestStableCandidateText 验证动态状态不会进入候选人稳定指纹。
func TestStableCandidateText(t *testing.T) {
	result := stableCandidateText("张三\n28岁\n在线\n立即沟通\n今天活跃")
	if strings.Contains(result, "在线") || strings.Contains(result, "立即沟通") || strings.Contains(result, "活跃") {
		t.Fatalf("稳定文本仍包含动态状态：%q", result)
	}
	if !strings.Contains(result, "张三") || !strings.Contains(result, "28岁") {
		t.Fatalf("稳定文本丢失候选人基础信息：%q", result)
	}
}

// TestCandidateNamesMatch 验证完整姓名和脱敏姓名不会明显串台。
func TestCandidateNamesMatch(t *testing.T) {
	if !candidateNamesMatch("张三", "张三先生") {
		t.Fatalf("完整姓名应该匹配")
	}
	if !candidateNamesMatch("张*三", "张三") {
		t.Fatalf("脱敏姓名首字相同应该匹配")
	}
	if candidateNamesMatch("张*三", "李四") {
		t.Fatalf("不同姓氏不应该匹配")
	}
}
