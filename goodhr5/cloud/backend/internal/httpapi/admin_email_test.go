// 本文件负责测试超管邮件自动任务的基础工具逻辑。
package httpapi

import (
	"strings"
	"testing"
	"time"
)

// TestNextRecoveryRun 验证自动挽回邮件会排到下一个目标小时。
func TestNextRecoveryRun(t *testing.T) {
	now := time.Date(2026, 7, 2, 8, 30, 0, 0, time.Local)
	sameDay := nextRecoveryRun(now, 9)
	if sameDay.Day() != 2 || sameDay.Hour() != 9 {
		t.Fatalf("sameDay = %v", sameDay)
	}
	nextDay := nextRecoveryRun(time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local), 9)
	if nextDay.Day() != 3 || nextDay.Hour() != 9 {
		t.Fatalf("nextDay = %v", nextDay)
	}
}

// TestAutomaticEmailTemplateHTML 验证自动邮件模板是完整移动端 HTML。
func TestAutomaticEmailTemplateHTML(t *testing.T) {
	html := automaticEmailTemplateHTML("inactive_3_days")
	if html == "" || !strings.Contains(html, "<!doctype html>") || !strings.Contains(html, "viewport") || !strings.Contains(html, "{{footer}}") {
		t.Fatalf("template html is incomplete: %q", html)
	}
}

// TestAppendEmailFooter 验证统一反馈文案会插入模板占位符。
func TestAppendEmailFooter(t *testing.T) {
	html := appendEmailFooter("<html><body>{{footer}}</body></html>", "wx-1")
	if !strings.Contains(html, "wx-1") || strings.Contains(html, "{{footer}}") || strings.Contains(html, "</html><p") {
		t.Fatalf("footer html = %s", html)
	}
}
