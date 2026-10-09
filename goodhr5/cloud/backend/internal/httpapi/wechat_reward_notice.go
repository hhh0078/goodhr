// 本文件在后台发送公众号关注赠送的会员邮件，复用现有通知模板并记录明确成功、可重试和不确定的发送结果。
package httpapi

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

const wechatRewardNoticePollInterval = 5 * time.Second

// WechatRewardNoticeService 从已提交的奖励账本读取邮件任务，不参与会员发放或微信回调。
type WechatRewardNoticeService struct {
	store         WechatRewardNoticeStore
	mailer        Mailer
	systemConfigs SystemConfigStore
}

// NewWechatRewardNoticeService 组装关注赠送通知，会员天数和到期时间始终取奖励入账时的快照。
func NewWechatRewardNoticeService(store WechatRewardNoticeStore, mailer Mailer, systemConfigs SystemConfigStore) *WechatRewardNoticeService {
	return &WechatRewardNoticeService{store: store, mailer: mailer, systemConfigs: systemConfigs}
}

// Start 在明确配置真实邮件且未禁用调度时启动串行后台任务；取消上下文后停止领取新邮件。
func (s *WechatRewardNoticeService) Start(ctx context.Context) bool {
	if s == nil || s.store == nil || s.mailer == nil || ctx == nil || ctx.Err() != nil || wechatRewardNoticeDisabled() {
		return false
	}
	switch mailer := s.mailer.(type) {
	case SMTPMailer:
		if mailer.Host == "" || mailer.Username == "" || mailer.Password == "" {
			return false
		}
	case *SMTPMailer:
		if mailer == nil || mailer.Host == "" || mailer.Username == "" || mailer.Password == "" {
			return false
		}
	default:
		// 开发邮件只写日志，不得据此将持久任务标成已发送。
		return false
	}
	go s.run(ctx)
	log.Print("GoodHR wechat reward notice worker started")
	return true
}

// run 每五秒最多发送一封邮件，避免集中发送影响SMTP额度，也不阻塞微信回调。
func (s *WechatRewardNoticeService) run(ctx context.Context) {
	ticker := time.NewTicker(wechatRewardNoticePollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.processOne(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processOne 独占领取一笔通知，复用统一会员邮件方法；发送结果不确定时停止自动重发。
func (s *WechatRewardNoticeService) processOne(ctx context.Context, now time.Time) {
	if s == nil || s.store == nil || s.mailer == nil || ctx == nil || ctx.Err() != nil || wechatRewardNoticeDisabled() {
		return
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	job, found, err := s.store.Claim(claimCtx, now)
	cancel()
	if err != nil {
		log.Print("GoodHR wechat reward notice claim failed")
		return
	}
	if !found {
		return
	}
	status, nextAttempt := "sent", time.Time{}
	if ctx.Err() != nil {
		// 取消发生在开始SMTP前，可安全把任务交回队列。
		status, nextAttempt = "retry", now
	} else {
		err = sendSubscriptionRewardNotice(s.mailer, s.systemConfigs, job.Email, SubscriptionRewardNotice{
			Reason: "公众号关注赠送会员", Days: job.Days, MemberType: job.MemberType, ExpiresAt: job.ExpiresAt,
		})
		if err != nil {
			switch {
			case IsMailDeliveryUncertain(err):
				status = "uncertain"
			case IsMailDeliveryTemporary(err) && job.Attempts < 5:
				status = "retry"
				nextAttempt = now.Add(wechatRewardNoticeRetryDelay(job.Attempts))
			default:
				status = "failed"
			}
		}
	}
	// SMTP已有真实超时；即使停止服务也应尽力写回已知结果，不能因取消而把已接收邮件重新排队。
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	updated, finishErr := s.store.Finish(finishCtx, job, status, nextAttempt)
	if finishErr != nil || !updated {
		// 若进程在发送后崩溃或写回失败，过期租约只标记uncertain，不能保证接收方绝对只收到一次。
		log.Print("GoodHR wechat reward notice result not saved; automatic resend suppressed")
		return
	}
	log.Printf("GoodHR wechat reward notice status=%s days=%d attempts=%d", status, job.Days, job.Attempts)
}

// wechatRewardNoticeDisabled 沿用现有候选服务禁发开关，与营销邮件启用配置互不影响。
func wechatRewardNoticeDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GOODHR_DISABLE_EMAIL_SCHEDULER")), "true")
}

// wechatRewardNoticeRetryDelay 对明确未发送的临时失败逐步退避，第五次失败停止自动重试。
func wechatRewardNoticeRetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(delays) {
		attempt = len(delays)
	}
	return delays[attempt-1]
}
