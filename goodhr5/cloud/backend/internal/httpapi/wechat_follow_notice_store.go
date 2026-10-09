// 本文件在公众号奖励账本上持久保存邮件领取与发送状态；历史奖励默认跳过，不修改会员或微信绑定。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"
)

const wechatRewardNoticeLease = 2 * time.Minute
const wechatRewardNoticeSkipLimit = 64

// WechatRewardNoticeJob 保存本次奖励的不可变快照与当前邮件领取身份，不包含OpenID或SMTP凭据。
type WechatRewardNoticeJob struct {
	Email      string
	Days       int
	MemberType string
	ExpiresAt  time.Time
	ClaimToken string
	Attempts   int
}

// WechatRewardNoticeStore 定义独占领取与带令牌写回，通知失败不会重新赠送会员。
type WechatRewardNoticeStore interface {
	// Claim 独占领取可发送通知，同时隔离结果不确定或已失效的账号。
	Claim(ctx context.Context, now time.Time) (WechatRewardNoticeJob, bool, error)
	// Finish 仅凭当前领取令牌写回发送结果，禁止旧工作进程覆盖状态。
	Finish(ctx context.Context, job WechatRewardNoticeJob, status string, nextAttempt time.Time) (bool, error)
}

// PostgresWechatRewardNoticeStore 复用永久奖励账本，在事务中串行领取同一通知。
type PostgresWechatRewardNoticeStore struct{ db *sql.DB }

// NewPostgresWechatRewardNoticeStore 创建公众号奖励邮件存储，不启动后台任务或发送邮件。
func NewPostgresWechatRewardNoticeStore(db *sql.DB) *PostgresWechatRewardNoticeStore {
	return &PostgresWechatRewardNoticeStore{db: db}
}

// Claim 原子领取到期通知；过期发送租约标为不确定，失效账号和无效邮箱标为跳过。
func (s *PostgresWechatRewardNoticeStore) Claim(ctx context.Context, now time.Time) (WechatRewardNoticeJob, bool, error) {
	if s == nil || s.db == nil {
		return WechatRewardNoticeJob{}, false, errors.New("邮件通知存储还没准备好")
	}
	if now.IsZero() {
		return WechatRewardNoticeJob{}, false, errors.New("领取通知需要有效时间")
	}
	token, err := newWechatMessageNonce()
	if err != nil {
		return WechatRewardNoticeJob{}, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WechatRewardNoticeJob{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `
		UPDATE wechat_follow_rewards SET mail_status='uncertain',mail_claim_token=NULL,
			mail_lease_until=NULL,mail_next_attempt_at=NULL
		WHERE mail_status='sending' AND mail_lease_until<=$1
	`, now); err != nil {
		return WechatRewardNoticeJob{}, false, err
	}
	for skipped := 0; skipped < wechatRewardNoticeSkipLimit; skipped++ {
		var job WechatRewardNoticeJob
		var userExists bool
		err = tx.QueryRowContext(ctx, `
			SELECT r.user_email,r.duration_days,r.member_type,r.subscription_expires_at,r.mail_attempts,
				EXISTS (SELECT 1 FROM users u WHERE u.id=r.user_id AND u.email=r.user_email)
			FROM wechat_follow_rewards r
			WHERE r.mail_status IN ('pending','retry') AND r.mail_next_attempt_at<=$1
			ORDER BY r.mail_next_attempt_at,r.applied_at,r.user_email
			FOR UPDATE OF r SKIP LOCKED LIMIT 1
		`, now).Scan(&job.Email, &job.Days, &job.MemberType, &job.ExpiresAt, &job.Attempts, &userExists)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return WechatRewardNoticeJob{}, false, err
		}
		if !userExists || !validWechatRewardNoticeEmail(job.Email) {
			if _, err = tx.ExecContext(ctx, `
				UPDATE wechat_follow_rewards SET mail_status='skipped',mail_next_attempt_at=NULL,
					mail_claim_token=NULL,mail_lease_until=NULL
				WHERE user_email=$1
			`, job.Email); err != nil {
				return WechatRewardNoticeJob{}, false, err
			}
			continue
		}
		job.ClaimToken = token
		job.Attempts++
		if _, err = tx.ExecContext(ctx, `
			UPDATE wechat_follow_rewards SET mail_status='sending',mail_claim_token=$2,
				mail_attempts=$3,mail_lease_until=$4,mail_next_attempt_at=NULL
			WHERE user_email=$1
		`, job.Email, job.ClaimToken, job.Attempts, now.Add(wechatRewardNoticeLease)); err != nil {
			return WechatRewardNoticeJob{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return WechatRewardNoticeJob{}, false, err
		}
		return job, true, nil
	}
	if err = tx.Commit(); err != nil {
		return WechatRewardNoticeJob{}, false, err
	}
	return WechatRewardNoticeJob{}, false, nil
}

// Finish 仅允许当前发送令牌写回结果；重试安排未来领取，成功记录SMTP接受时间。
func (s *PostgresWechatRewardNoticeStore) Finish(ctx context.Context, job WechatRewardNoticeJob, status string, nextAttempt time.Time) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("邮件通知存储还没准备好")
	}
	switch status {
	case "sent", "failed", "uncertain":
	case "retry":
		if nextAttempt.IsZero() {
			return false, errors.New("重试通知需要下一次发送时间")
		}
	default:
		return false, errors.New("邮件通知结果不正确")
	}
	if job.Email == "" || job.ClaimToken == "" {
		return false, errors.New("邮件通知缺少领取身份")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var next any
	if status == "retry" {
		next = nextAttempt
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE wechat_follow_rewards SET mail_status=$3,mail_next_attempt_at=$4,
			mail_claim_token=NULL,mail_lease_until=NULL,
			mail_sent_at=CASE WHEN $3='sent' THEN now() ELSE NULL END
		WHERE user_email=$1 AND mail_status='sending' AND mail_claim_token=$2
	`, job.Email, job.ClaimToken, status, next)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// validWechatRewardNoticeEmail 只接受单个完整邮箱地址，拒绝展示名称、空白及无法解析的目标。
func validWechatRewardNoticeEmail(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}
