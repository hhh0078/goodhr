// Package greeting 文件作用：验证页面诊断日志不会记录查询参数中的敏感信息。
package greeting

import "testing"

// TestSafeDiagnosticPageAddress 验证诊断地址保留页面路径和片段，但移除全部查询参数。
func TestSafeDiagnosticPageAddress(t *testing.T) {
	actual := safeDiagnosticPageAddress("https://h.liepin.com/resume/showresumedetail/?res_id_encode=secret&token=secret#session")
	expected := "https://h.liepin.com/resume/showresumedetail/#session"
	if actual != expected {
		t.Fatalf("诊断地址 = %q，期望 %q", actual, expected)
	}
}
