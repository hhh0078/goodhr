// 本文件负责测试邮件模板渲染和 MIME 邮件头格式。
package httpapi

import (
	"strings"
	"testing"
	"time"
)

// TestMailTemplatesRender 验证现有邮件模板可以正常渲染关键内容。
func TestMailTemplatesRender(t *testing.T) {
	mailer := SMTPMailer{}
	loginHTML := mailer.renderHTML("login_code.html", map[string]any{"Code": "1234"})
	if !strings.Contains(loginHTML, "1234") || !strings.Contains(loginHTML, "GoodHR 登录终端") {
		t.Fatalf("login template did not render expected content: %s", loginHTML)
	}

	rewardHTML := mailer.renderHTML("subscription_reward.html", map[string]any{
		"Reason":       "新用户注册赠送会员",
		"DaysText":     "+3 天",
		"MemberType":   "plus",
		"ExpiresAt":    time.Date(2026, 6, 2, 12, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
		"RelatedEmail": "",
	})
	if !strings.Contains(rewardHTML, "新用户注册赠送会员") || !strings.Contains(rewardHTML, "3 天") {
		t.Fatalf("reward template did not render expected content: %s", rewardHTML)
	}
}

// TestBuildMailMessageHasSingleSubject 验证邮件只包含一个主题头。
func TestBuildMailMessageHasSingleSubject(t *testing.T) {
	message := buildMailMessage("from@example.com", "to@example.com", "GoodHR 测试", "hello", "<p>hello</p>")
	if count := strings.Count(message, "\r\nSubject: "); count != 1 {
		t.Fatalf("subject header count = %d, want 1", count)
	}
}
