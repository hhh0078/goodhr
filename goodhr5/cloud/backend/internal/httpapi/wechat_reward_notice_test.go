// 本文件验证公众号赠送复用统一邮件内容、动态奖励快照、禁发开关和SMTP失败处理；不外发真实邮件。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// wechatNoticeTestStore 模拟一次独占领取及带令牌的结果写回，便于验证发送后的失败边界。
type wechatNoticeTestStore struct {
	job         WechatRewardNoticeJob
	claimed     bool
	claims      int
	finishes    int
	status      string
	nextAttempt time.Time
	claimErr    error
	finishErr   error
	onClaim     func()
}

// Claim 只让一笔任务领取一次，错误与取消由各场景明确设置。
func (s *wechatNoticeTestStore) Claim(ctx context.Context, now time.Time) (WechatRewardNoticeJob, bool, error) {
	s.claims++
	if s.claimErr != nil {
		return WechatRewardNoticeJob{}, false, s.claimErr
	}
	if s.claimed {
		return WechatRewardNoticeJob{}, false, nil
	}
	s.claimed = true
	if s.onClaim != nil {
		s.onClaim()
	}
	return s.job, true, nil
}

// Finish 记录写回状态并检查发送结果保存使用独立有效上下文。
func (s *wechatNoticeTestStore) Finish(ctx context.Context, job WechatRewardNoticeJob, status string, nextAttempt time.Time) (bool, error) {
	s.finishes++
	s.status, s.nextAttempt = status, nextAttempt
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.finishErr != nil {
		return false, s.finishErr
	}
	return true, nil
}

// wechatNoticeTestMailer 记录共享通知并支持模拟发送过程中服务取消。
type wechatNoticeTestMailer struct {
	DevMailer
	calls  int
	to     string
	notice SubscriptionRewardNotice
	err    error
	onSend func()
}

// SendSubscriptionReward 捕获统一模板补齐后的通知，不创建任何真实网络连接。
func (m *wechatNoticeTestMailer) SendSubscriptionReward(email string, notice SubscriptionRewardNotice) error {
	m.calls++
	m.to, m.notice = email, notice
	if m.onSend != nil {
		m.onSend()
	}
	return m.err
}

// newWechatNoticeTestJob 创建不含真实用户信息的已到账奖励快照。
func newWechatNoticeTestJob() WechatRewardNoticeJob {
	return WechatRewardNoticeJob{Email: "follow@example.com", Days: 2, MemberType: memberTypeMax,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).Truncate(time.Microsecond), ClaimToken: "test-claim", Attempts: 1}
}

// TestWechatRewardNoticeUsesSharedTemplate 验证2/7天及Plus/Max均取账本快照，并由原会员通知方法补齐套餐信息。
func TestWechatRewardNoticeUsesSharedTemplate(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	for _, days := range []int{2, 7} {
		for _, member := range []string{memberTypePlus, memberTypeMax} {
			t.Run(fmt.Sprintf("%s-%d-days", member, days), func(t *testing.T) {
				job := newWechatNoticeTestJob()
				job.Days, job.MemberType = days, member
				store := &wechatNoticeTestStore{job: job}
				mailer := &wechatNoticeTestMailer{}
				configs := NewMemorySystemConfigStore()
				// 修改当前奖励配置不会改变已经入账的通知天数。
				if err := configs.Save(SystemConfig{ConfigKey: "system.onboarding_config", ConfigValue: `{"wechat_follow_reward_days":30}`, Enabled: true}); err != nil {
					t.Fatal(err)
				}
				svc := NewWechatRewardNoticeService(store, mailer, configs)
				svc.processOne(context.Background(), time.Now())
				svc.processOne(context.Background(), time.Now())
				if mailer.calls != 1 || store.status != "sent" || store.finishes != 1 {
					t.Fatalf("calls=%d status=%s finishes=%d", mailer.calls, store.status, store.finishes)
				}
				n := mailer.notice
				if mailer.to != job.Email || n.Days != days || n.MemberType != member || !n.ExpiresAt.Equal(job.ExpiresAt) || n.Reason != "公众号关注赠送会员" {
					t.Fatalf("wrong reward snapshot: %+v", n)
				}
				if n.MemberName == "" || n.RemainingDays == 0 || len(n.Features) == 0 {
					t.Fatalf("shared helper not used: %+v", n)
				}
				html := (SMTPMailer{}).renderHTML("subscription_reward.html", map[string]any{"Reason": n.Reason, "MemberName": n.MemberName, "DaysText": fmt.Sprintf("%+d 天", n.Days), "ExpiresAt": n.ExpiresAt.Format("2006-01-02 15:04:05"), "RemainingDays": n.RemainingDays, "AllowAutoReply": n.AllowAutoReply, "Features": n.Features})
				if !strings.Contains(html, n.Reason) || !strings.Contains(html, n.MemberName) {
					t.Fatalf("shared template missing reward content")
				}
			})
		}
	}
}

// TestWechatRewardNoticeFailurePolicy 验证明确临时失败退避，永久拒绝或不确定发送停止自动重发。
func TestWechatRewardNoticeFailurePolicy(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	cases := []struct {
		name     string
		err      error
		attempts int
		status   string
		delay    time.Duration
	}{
		{"temporary", &textproto.Error{Code: 450, Msg: "temporary"}, 1, "retry", time.Minute},
		{"last-attempt", &textproto.Error{Code: 450, Msg: "temporary"}, 5, "failed", 0},
		{"permanent", &textproto.Error{Code: 550, Msg: "rejected"}, 1, "failed", 0},
		{"uncertain", &mailDeliveryUncertainError{cause: io.EOF}, 1, "uncertain", 0},
		{"template-error", errors.New("invalid template"), 1, "failed", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			job := newWechatNoticeTestJob()
			job.Attempts = c.attempts
			store := &wechatNoticeTestStore{job: job}
			mailer := &wechatNoticeTestMailer{err: c.err}
			now := time.Now()
			NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore()).processOne(context.Background(), now)
			if store.status != c.status || mailer.calls != 1 {
				t.Fatalf("status=%s calls=%d", store.status, mailer.calls)
			}
			if c.delay > 0 && !store.nextAttempt.Equal(now.Add(c.delay)) {
				t.Fatalf("wrong backoff %v", store.nextAttempt)
			}
			if c.delay == 0 && !store.nextAttempt.IsZero() {
				t.Fatalf("unexpected retry")
			}
		})
	}
}

// TestWechatRewardNoticeClaimAndFinishFailure 验证领取失败不发送、发送后写回失败也不在当前进程重新发送。
func TestWechatRewardNoticeClaimAndFinishFailure(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	t.Run("claim-failed", func(t *testing.T) {
		store := &wechatNoticeTestStore{claimErr: errors.New("db unavailable")}
		mailer := &wechatNoticeTestMailer{}
		NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore()).processOne(context.Background(), time.Now())
		if mailer.calls != 0 || store.finishes != 0 {
			t.Fatal("failed claim sent mail")
		}
	})
	t.Run("finish-failed", func(t *testing.T) {
		store := &wechatNoticeTestStore{job: newWechatNoticeTestJob(), finishErr: errors.New("db unavailable")}
		mailer := &wechatNoticeTestMailer{}
		svc := NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore())
		svc.processOne(context.Background(), time.Now())
		svc.processOne(context.Background(), time.Now())
		if mailer.calls != 1 || store.status != "sent" {
			t.Fatalf("calls=%d status=%s", mailer.calls, store.status)
		}
	})
}

// TestWechatRewardNoticeCancellation 验证发送前取消可安全重试，而SMTP接受后取消仍保存成功结果。
func TestWechatRewardNoticeCancellation(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	t.Run("before-send", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &wechatNoticeTestStore{job: newWechatNoticeTestJob(), onClaim: cancel}
		mailer := &wechatNoticeTestMailer{}
		NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore()).processOne(ctx, time.Now())
		if mailer.calls != 0 || store.status != "retry" {
			t.Fatal("cancelled task started SMTP")
		}
	})
	t.Run("after-send", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		store := &wechatNoticeTestStore{job: newWechatNoticeTestJob()}
		mailer := &wechatNoticeTestMailer{onSend: cancel}
		NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore()).processOne(ctx, time.Now())
		if mailer.calls != 1 || store.status != "sent" {
			t.Fatal("accepted mail did not save result")
		}
	})
}

// TestWechatRewardNoticeDisableAndDevelopment 验证候选禁发、开发Mailer及空依赖均不领取或误标已发送。
func TestWechatRewardNoticeDisableAndDevelopment(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "true")
	store := &wechatNoticeTestStore{job: newWechatNoticeTestJob()}
	mailer := &wechatNoticeTestMailer{}
	svc := NewWechatRewardNoticeService(store, mailer, NewMemorySystemConfigStore())
	if svc.Start(context.Background()) {
		t.Fatal("disabled worker started")
	}
	if NewWechatRewardNoticeService(store, SMTPMailer{Host: "127.0.0.1", Username: "test", Password: "test"}, nil).Start(context.Background()) {
		t.Fatal("candidate SMTP worker started")
	}
	svc.processOne(context.Background(), time.Now())
	if store.claims != 0 || mailer.calls != 0 {
		t.Fatal("disabled worker sent mail")
	}
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	if NewWechatRewardNoticeService(store, DevMailer{}, nil).Start(context.Background()) {
		t.Fatal("dev mail marked as real delivery")
	}
	if NewWechatRewardNoticeService(nil, SMTPMailer{}, nil).Start(context.Background()) {
		t.Fatal("nil store started")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if NewWechatRewardNoticeService(store, SMTPMailer{}, nil).Start(ctx) {
		t.Fatal("cancelled worker started")
	}
}

// TestWechatRewardNoticeLoopStops 验证取消后立即退出，不为新服务留下仍运行的发送协程。
func TestWechatRewardNoticeLoopStops(t *testing.T) {
	t.Setenv("GOODHR_DISABLE_EMAIL_SCHEDULER", "false")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		NewWechatRewardNoticeService(&wechatNoticeTestStore{}, &wechatNoticeTestMailer{}, nil).run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}
