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
	if !strings.Contains(loginHTML, "1234") || !strings.Contains(loginHTML, "验证码来啦") {
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

	balanceHTML := mailer.renderHTML("ai_balance_notice.html", map[string]any{
		"Reason":      "活动赠送",
		"ChangeText":  "+10.00",
		"BalanceText": "20.00",
	})
	if !strings.Contains(balanceHTML, "活动赠送") || !strings.Contains(balanceHTML, "20.00") {
		t.Fatalf("balance template did not render expected content: %s", balanceHTML)
	}

	positionHTML := mailer.renderHTML("position_status.html", map[string]any{
		"PositionName":      "带货主播",
		"Status":            "failed",
		"StatusLabel":       "岗位运行失败",
		"TodayGreetedCount": 12,
		"RunGreetedCount":   3,
		"RunSkippedCount":   6,
		"ErrorMessage":      "本地程序断开",
	})
	if !strings.Contains(positionHTML, "今日打招呼") || !strings.Contains(positionHTML, "本次跳过") || !strings.Contains(positionHTML, "本地程序断开") {
		t.Fatalf("position template did not render expected content: %s", positionHTML)
	}
}

// TestBuildMailMessageHasSingleSubject 验证邮件只包含一个主题头。
func TestBuildMailMessageHasSingleSubject(t *testing.T) {
	message := buildMailMessage("from@example.com", "to@example.com", "GoodHR 测试", "hello", "<p>hello</p>")
	if count := strings.Count(message, "\r\nSubject: "); count != 1 {
		t.Fatalf("subject header count = %d, want 1", count)
	}
}
