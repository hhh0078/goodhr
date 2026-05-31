// 本文件负责测试任务结束和失败时的邮件提醒。
package httpapi

import "testing"

type taskNoticeMailer struct {
	notices []TaskStatusNotice
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
		MatchLimit:        20,
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
