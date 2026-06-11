// 本文件负责测试任务结束和失败时的邮件提醒。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type taskNoticeMailer struct {
	notices []TaskStatusNotice
	emails  []string
}

// SendLoginCode 忽略登录验证码邮件发送请求。
func (m *taskNoticeMailer) SendLoginCode(email string, code string) error {
	return nil
}

// SendSubscriptionReward 忽略会员奖励邮件发送请求。
func (m *taskNoticeMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	return nil
}

// SendTaskStatus 记录任务状态邮件内容，方便测试断言。
func (m *taskNoticeMailer) SendTaskStatus(email string, notice TaskStatusNotice) error {
	m.emails = append(m.emails, email)
	m.notices = append(m.notices, notice)
	return nil
}

// TestTaskStatusNoticeLabel 验证任务状态邮件区分结束和失败。
func TestTaskStatusNoticeLabel(t *testing.T) {
	if got := taskStatusNoticeLabel("failed"); got != "任务失败" {
		t.Fatalf("failed label = %s", got)
	}
	if got := taskStatusNoticeLabel("stopped"); got != "任务结束" {
		t.Fatalf("stopped label = %s", got)
	}
}

// TestSendTaskStatusNotice 验证任务状态邮件会携带任务统计和失败原因。
func TestSendTaskStatusNotice(t *testing.T) {
	store := NewMemoryTaskStore()
	task, err := store.CreateTask(TaskRun{
		UserEmail:         "notice@example.com",
		PlatformID:        "boss",
		PlatformAccountID: "account_1",
		Mode:              "keyword",
		MatchLimit:        50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.IncrementTaskCounts(task.ID, 10, 3, 6, 1); err != nil {
		t.Fatal(err)
	}
	mailer := &taskNoticeMailer{}
	service := &TaskService{
		store:  store,
		mailer: mailer,
	}

	service.sendTaskStatusNotice(task, "failed", "本地程序断开")

	if len(mailer.notices) != 1 {
		t.Fatalf("notice count = %d", len(mailer.notices))
	}
	notice := mailer.notices[0]
	if notice.StatusLabel != "任务失败" || notice.ErrorMessage != "本地程序断开" {
		t.Fatalf("unexpected notice status: %+v", notice)
	}
	if notice.ScannedCount != 10 || notice.GreetedCount != 3 || notice.SkippedCount != 6 || notice.FailedCount != 1 {
		t.Fatalf("unexpected notice counts: %+v", notice)
	}
}

// TestFailNoticeRequiresAuth 验证失败通知会根据 token 查当前用户邮箱。
func TestFailNoticeRequiresAuth(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "notice@example.com")
	server.tasks.mailer = &taskNoticeMailer{}

	task, err := server.tasks.store.CreateTask(TaskRun{
		UserEmail:  "notice@example.com",
		PlatformID: "boss",
		Mode:       "ai",
		MatchLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{
		"task_id":       task.ID,
		"error_message": "本地任务失败",
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/fail-notice", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()

	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	mailer, ok := server.tasks.mailer.(*taskNoticeMailer)
	if !ok {
		t.Fatal("mailer type mismatch")
	}
	if len(mailer.emails) != 1 || mailer.emails[0] != "notice@example.com" {
		t.Fatalf("emails = %+v", mailer.emails)
	}
	if len(mailer.notices) != 1 {
		t.Fatalf("notice count = %d", len(mailer.notices))
	}
	if mailer.notices[0].TaskID != task.ID || mailer.notices[0].ErrorMessage != "本地任务失败" {
		t.Fatalf("notice = %+v", mailer.notices[0])
	}
}
