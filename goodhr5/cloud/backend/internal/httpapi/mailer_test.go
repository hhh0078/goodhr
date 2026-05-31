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

	taskHTML := mailer.renderHTML("task_status.html", map[string]any{
		"TaskID":          "task_1",
		"Status":          "failed",
		"StatusLabel":     "任务失败",
		"PlatformID":      "boss",
		"PlatformAccount": "测试账号",
		"Mode":            "keyword",
		"MatchLimit":      50,
		"ScannedCount":    10,
		"GreetedCount":    3,
		"SkippedCount":    6,
		"FailedCount":     1,
		"FinishedAt":      time.Date(2026, 6, 2, 12, 0, 0, 0, time.Local).Format("2006-01-02 15:04:05"),
		"ErrorMessage":    "本地程序断开",
	})
	if !strings.Contains(taskHTML, "任务失败") || !strings.Contains(taskHTML, "本地程序断开") {
		t.Fatalf("task template did not render expected content: %s", taskHTML)
	}
}

// TestBuildMailMessageHasSingleSubject 验证邮件只包含一个主题头。
func TestBuildMailMessageHasSingleSubject(t *testing.T) {
	message := buildMailMessage("from@example.com", "to@example.com", "GoodHR 测试", "hello", "<p>hello</p>")
	if count := strings.Count(message, "\r\nSubject: "); count != 1 {
		t.Fatalf("subject header count = %d, want 1", count)
	}
}
