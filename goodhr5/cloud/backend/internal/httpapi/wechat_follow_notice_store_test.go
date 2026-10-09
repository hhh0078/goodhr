// 本文件在专用本机 PostgreSQL 中验证关注奖励通知的迁移桥接、不可变快照、独占领取、回滚和不确定发送边界；不发送邮件。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
)

// wechatNoticeFixture 为通知事务测试隔离账号、公众号身份和领取时间。
type wechatNoticeFixture struct {
	db      *sql.DB
	follow  *PostgresWechatFollowStore
	notices *PostgresWechatRewardNoticeStore
	prefix  string
	now     time.Time
}

// wechatNoticeGift 保存测试发奖身份，方便重复关注和消息重放使用同一真实账本。
type wechatNoticeGift struct {
	email  string
	hash   string
	openID string
	result WechatBindResult
}

// wechatNoticeState 读取持久通知字段，防止测试只观察工作进程返回值而遗漏数据库副作用。
type wechatNoticeState struct {
	status   string
	attempts int
	next     sql.NullTime
	token    sql.NullString
	lease    sql.NullTime
	sent     sql.NullTime
}

// newWechatNoticeFixture 创建带安全连接检查的隔离数据，并在结束后仅删除本组测试身份。
func newWechatNoticeFixture(t *testing.T) wechatNoticeFixture {
	t.Helper()
	db := openWechatTestDatabase(t)
	fixture := wechatNoticeFixture{
		db: db, follow: NewPostgresWechatFollowStore(db), notices: NewPostgresWechatRewardNoticeStore(db),
		prefix: fmt.Sprintf("notice-%d-", time.Now().UnixNano()),
		// 早于其他公众号测试的领取时刻，避免领取共享隔离库中的其他测试通知。
		now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			query string
			value string
		}{
			{`DELETE FROM wechat_follow_rewards WHERE user_email LIKE $1`, fixture.prefix + "%"},
			{`DELETE FROM wechat_follow_states WHERE app_id=$1`, fixture.prefix + "app"},
			{`DELETE FROM wechat_follow_binding_codes WHERE user_email LIKE $1`, fixture.prefix + "%"},
			{`DELETE FROM users WHERE email LIKE $1`, fixture.prefix + "%"},
		} {
			if _, err := db.Exec(cleanup.query, cleanup.value); err != nil {
				t.Errorf("clean isolated notice fixture: %v", err)
			}
		}
	})
	return fixture
}

// prepare 为一位账号保存真实待绑定码与关注记录，会员种类可指定为有效Plus或未付费账号。
func (f wechatNoticeFixture) prepare(t *testing.T, suffix, memberType string) wechatNoticeGift {
	t.Helper()
	gift := wechatNoticeGift{
		email: f.prefix + suffix + "@example.com",
		hash:  wechatTestCodeHash(f.prefix + suffix), openID: f.prefix + suffix + "-openid",
	}
	if err := f.follow.CreateBinding(context.Background(), gift.email, gift.hash, f.now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if memberType != "" {
		payload, err := json.Marshal(Subscription{MemberType: memberType, ExpiresAt: f.now.Add(20 * 24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Exec(`UPDATE users SET subscription=$2::jsonb WHERE email=$1`, gift.email, string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.follow.RecordFollow(context.Background(), f.prefix+"app", gift.openID, true, f.now); err != nil {
		t.Fatal(err)
	}
	return gift
}

// grant 通过生产绑定事务发奖，通知必须与会员和永久账本一起提交。
func (f wechatNoticeFixture) grant(t *testing.T, suffix string, days int, memberType string) wechatNoticeGift {
	t.Helper()
	gift := f.prepare(t, suffix, memberType)
	result, err := f.follow.BindCode(context.Background(), f.prefix+"app", gift.openID, gift.hash, f.now, WechatBindOptions{RewardDays: days})
	if err != nil || !result.GiftGranted {
		t.Fatalf("new gift=%+v err=%v", result, err)
	}
	gift.result = result
	return gift
}

// state 读取指定测试账号的完整通知状态。
func (f wechatNoticeFixture) state(t *testing.T, email string) wechatNoticeState {
	t.Helper()
	var state wechatNoticeState
	if err := f.db.QueryRow(`SELECT mail_status,mail_attempts,mail_next_attempt_at,mail_claim_token,mail_lease_until,mail_sent_at
		FROM wechat_follow_rewards WHERE user_email=$1`, email).Scan(&state.status, &state.attempts, &state.next, &state.token, &state.lease, &state.sent); err != nil {
		t.Fatal(err)
	}
	return state
}

// claim 领取并核对本组预期邮箱，避免测试误将别组奖励当作本组成功。
func (f wechatNoticeFixture) claim(t *testing.T, now time.Time, email string) WechatRewardNoticeJob {
	t.Helper()
	job, found, err := f.notices.Claim(context.Background(), now)
	if err != nil || !found || job.Email != email {
		t.Fatalf("claim=%+v found=%v expected=%s err=%v", job, found, email, err)
	}
	return job
}

// noClaim 核对尚未到期、正在发送或终态通知不会被重复领取。
func (f wechatNoticeFixture) noClaim(t *testing.T, now time.Time) {
	t.Helper()
	job, found, err := f.notices.Claim(context.Background(), now)
	if err != nil || found {
		t.Fatalf("unexpected claim=%+v found=%v err=%v", job, found, err)
	}
}

// TestPostgresWechatRewardNoticeSnapshotAndRetry 验证动态赠送快照、安全重试时间、令牌更换及SMTP接受后的终态。
func TestPostgresWechatRewardNoticeSnapshotAndRetry(t *testing.T) {
	f := newWechatNoticeFixture(t)
	gift := f.grant(t, "snapshot", 7, memberTypePlus)
	initial := f.state(t, gift.email)
	if initial.status != "pending" || initial.attempts != 0 || !initial.next.Valid || !initial.next.Time.Equal(f.now) {
		t.Fatalf("new reward notice=%+v", initial)
	}
	if _, err := f.db.Exec(`UPDATE users SET subscription=jsonb_build_object('member_type','max','expires_at',$2::text)
		WHERE email=$1`, gift.email, f.now.Add(100*24*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	job := f.claim(t, f.now, gift.email)
	if job.Days != 7 || job.MemberType != memberTypePlus || !job.ExpiresAt.Equal(f.now.Add(27*24*time.Hour)) || job.Attempts != 1 || len(job.ClaimToken) != 64 {
		t.Fatalf("notice must retain reward snapshot: %+v", job)
	}
	state := f.state(t, gift.email)
	if state.status != "sending" || state.next.Valid || !state.lease.Valid || !state.lease.Time.Equal(f.now.Add(2*time.Minute)) {
		t.Fatalf("claimed state=%+v", state)
	}
	f.noClaim(t, f.now.Add(time.Minute))
	nextAttempt := f.now.Add(3 * time.Minute)
	if changed, err := f.notices.Finish(context.Background(), job, "retry", nextAttempt); err != nil || !changed {
		t.Fatalf("finish retry changed=%v err=%v", changed, err)
	}
	f.noClaim(t, nextAttempt.Add(-time.Microsecond))
	second := f.claim(t, nextAttempt, gift.email)
	if second.Attempts != 2 || second.ClaimToken == job.ClaimToken || second.Days != job.Days || !second.ExpiresAt.Equal(job.ExpiresAt) {
		t.Fatalf("retry snapshot=%+v first=%+v", second, job)
	}
	if changed, err := f.notices.Finish(context.Background(), job, "sent", time.Time{}); err != nil || changed {
		t.Fatalf("stale finish changed=%v err=%v", changed, err)
	}
	if changed, err := f.notices.Finish(context.Background(), second, "sent", time.Time{}); err != nil || !changed {
		t.Fatalf("accepted finish changed=%v err=%v", changed, err)
	}
	state = f.state(t, gift.email)
	if state.status != "sent" || state.attempts != 2 || !state.sent.Valid || state.token.Valid || state.lease.Valid || state.next.Valid {
		t.Fatalf("sent state=%+v", state)
	}
	if changed, err := f.notices.Finish(context.Background(), second, "sent", time.Time{}); err != nil || changed {
		t.Fatalf("duplicate finish changed=%v err=%v", changed, err)
	}
	f.noClaim(t, nextAttempt.Add(time.Hour))
}

// TestPostgresWechatRewardNoticeReplayPreservesStatus 验证重复回调与取消重关不会重置已发送、失败、不确定或重试计划。
func TestPostgresWechatRewardNoticeReplayPreservesStatus(t *testing.T) {
	for _, status := range []string{"sent", "retry", "failed", "uncertain"} {
		t.Run(status, func(t *testing.T) {
			f := newWechatNoticeFixture(t)
			gift := f.grant(t, "replay", 2, "")
			job := f.claim(t, f.now, gift.email)
			nextAttempt := f.now.Add(time.Hour)
			if changed, err := f.notices.Finish(context.Background(), job, status, nextAttempt); err != nil || !changed {
				t.Fatalf("finish changed=%v err=%v", changed, err)
			}
			before := f.state(t, gift.email)
			replay, err := f.follow.BindCode(context.Background(), f.prefix+"app", gift.openID, gift.hash, f.now.Add(time.Second), WechatBindOptions{RewardDays: 7})
			if err != nil || replay.GiftGranted {
				t.Fatalf("replay=%+v err=%v", replay, err)
			}
			if err = f.follow.RecordFollow(context.Background(), f.prefix+"app", gift.openID, false, f.now.Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			if err = f.follow.RecordFollow(context.Background(), f.prefix+"app", gift.openID, true, f.now.Add(3*time.Second)); err != nil {
				t.Fatal(err)
			}
			newHash := wechatTestCodeHash(f.prefix + "replay-new")
			if err = f.follow.CreateBinding(context.Background(), gift.email, newHash, f.now.Add(11*time.Minute)); err != nil {
				t.Fatal(err)
			}
			replay, err = f.follow.BindCode(context.Background(), f.prefix+"app", gift.openID, newHash, f.now.Add(time.Minute), WechatBindOptions{RewardDays: 7})
			if err != nil || replay.GiftGranted || !replay.Subscription.ExpiresAt.Equal(gift.result.Subscription.ExpiresAt) {
				t.Fatalf("refollow=%+v err=%v", replay, err)
			}
			after := f.state(t, gift.email)
			if after != before {
				t.Fatalf("replay reset notice: before=%+v after=%+v", before, after)
			}
			var count, days int
			if err = f.db.QueryRow(`SELECT count(*),max(duration_days) FROM wechat_follow_rewards WHERE user_email=$1`, gift.email).Scan(&count, &days); err != nil || count != 1 || days != 2 {
				t.Fatalf("permanent reward count=%d days=%d err=%v", count, days, err)
			}
		})
	}
}

// TestPostgresWechatRewardNoticeGiftRollback 验证奖励INSERT之后发生事务失败时，会员、绑定码与邮件通知一起回滚。
func TestPostgresWechatRewardNoticeGiftRollback(t *testing.T) {
	f := newWechatNoticeFixture(t)
	gift := f.prepare(t, "rollback", "")
	var beforeSubscription string
	if err := f.db.QueryRow(`SELECT subscription::text FROM users WHERE email=$1`, gift.email).Scan(&beforeSubscription); err != nil {
		t.Fatal(err)
	}
	name := "notice_rollback_" + fmt.Sprint(time.Now().UnixNano())
	functionName, triggerName := pq.QuoteIdentifier(name), pq.QuoteIdentifier(name+"_trigger")
	if _, err := f.db.Exec(`CREATE FUNCTION ` + functionName + `() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF OLD.code_hash='` + gift.hash + `' THEN RAISE EXCEPTION 'isolated notice rollback'; END IF; RETURN NEW; END $$`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.db.Exec(`DROP TRIGGER IF EXISTS ` + triggerName + ` ON wechat_follow_binding_codes`); err != nil {
			t.Errorf("drop local rollback trigger: %v", err)
		}
		if _, err := f.db.Exec(`DROP FUNCTION IF EXISTS ` + functionName + `()`); err != nil {
			t.Errorf("drop local rollback function: %v", err)
		}
	})
	if _, err := f.db.Exec(`CREATE TRIGGER ` + triggerName + ` BEFORE UPDATE ON wechat_follow_binding_codes
		FOR EACH ROW EXECUTE FUNCTION ` + functionName + `()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.follow.BindCode(context.Background(), f.prefix+"app", gift.openID, gift.hash, f.now, WechatBindOptions{RewardDays: 2}); err == nil {
		t.Fatal("post-insert failure should rollback gift transaction")
	}
	var count int
	if err := f.db.QueryRow(`SELECT count(*) FROM wechat_follow_rewards WHERE user_email=$1`, gift.email).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback left notice count=%d err=%v", count, err)
	}
	var bound, completed bool
	if err := f.db.QueryRow(`SELECT wechat_openid IS NOT NULL FROM users WHERE email=$1`, gift.email).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT completed_at IS NOT NULL FROM wechat_follow_binding_codes WHERE code_hash=$1`, gift.hash).Scan(&completed); err != nil {
		t.Fatal(err)
	}
	if bound || completed {
		t.Fatalf("rollback bound=%v completed=%v", bound, completed)
	}
	var afterSubscription string
	if err := f.db.QueryRow(`SELECT subscription::text FROM users WHERE email=$1`, gift.email).Scan(&afterSubscription); err != nil || afterSubscription != beforeSubscription {
		t.Fatalf("rollback modified subscription before=%s after=%s err=%v", beforeSubscription, afterSubscription, err)
	}
	if _, err := f.db.Exec(`DROP TRIGGER ` + triggerName + ` ON wechat_follow_binding_codes`); err != nil {
		t.Fatal(err)
	}
	result, err := f.follow.BindCode(context.Background(), f.prefix+"app", gift.openID, gift.hash, f.now, WechatBindOptions{RewardDays: 2})
	if err != nil || !result.GiftGranted || f.state(t, gift.email).status != "pending" {
		t.Fatalf("safe retry gift=%+v err=%v", result, err)
	}
}

// TestPostgresWechatRewardNoticeConcurrentClaim 验证二十四个真实并发领取请求只能得到同一奖励的一份发送身份。
func TestPostgresWechatRewardNoticeConcurrentClaim(t *testing.T) {
	f := newWechatNoticeFixture(t)
	gift := f.grant(t, "concurrent", 2, "")
	type outcome struct {
		job   WechatRewardNoticeJob
		found bool
		err   error
	}
	results := make(chan outcome, 24)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < 24; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			job, found, err := f.notices.Claim(context.Background(), f.now)
			results <- outcome{job, found, err}
		}()
	}
	close(start)
	group.Wait()
	close(results)
	claimed := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.found {
			claimed++
			if result.job.Email != gift.email || result.job.Attempts != 1 {
				t.Fatalf("unexpected concurrent job=%+v", result.job)
			}
		}
	}
	if claimed != 1 || f.state(t, gift.email).attempts != 1 {
		t.Fatalf("concurrent claimed=%d", claimed)
	}
}

// TestPostgresWechatRewardNoticeSkipsLockedRow 验证被其他事务锁定的通知不会阻塞后续可发送奖励。
func TestPostgresWechatRewardNoticeSkipsLockedRow(t *testing.T) {
	f := newWechatNoticeFixture(t)
	first := f.grant(t, "a-locked", 2, "")
	second := f.grant(t, "b-ready", 2, "")
	tx, err := f.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT user_email FROM wechat_follow_rewards WHERE user_email=$1 FOR UPDATE`, first.email); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	job, found, err := f.notices.Claim(ctx, f.now)
	if err != nil || !found || job.Email != second.email {
		t.Fatalf("SKIP LOCKED job=%+v found=%v err=%v", job, found, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	f.claim(t, f.now, first.email)
}

// TestPostgresWechatRewardNoticeExpiredLeaseIsUncertain 验证崩溃或结果写回失败后只保留不确定状态，不自动重发。
func TestPostgresWechatRewardNoticeExpiredLeaseIsUncertain(t *testing.T) {
	for _, finishFailed := range []bool{false, true} {
		t.Run(fmt.Sprintf("finish-failed-%v", finishFailed), func(t *testing.T) {
			f := newWechatNoticeFixture(t)
			gift := f.grant(t, "lease", 2, "")
			job := f.claim(t, f.now, gift.email)
			if finishFailed {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if changed, err := f.notices.Finish(ctx, job, "sent", time.Time{}); err == nil || changed {
					t.Fatalf("canceled write changed=%v err=%v", changed, err)
				}
			}
			f.noClaim(t, f.now.Add(2*time.Minute-time.Microsecond))
			f.noClaim(t, f.now.Add(2*time.Minute))
			state := f.state(t, gift.email)
			if state.status != "uncertain" || state.attempts != 1 || state.token.Valid || state.lease.Valid || state.next.Valid || state.sent.Valid {
				t.Fatalf("expired sending=%+v", state)
			}
			if changed, err := f.notices.Finish(context.Background(), job, "sent", time.Time{}); err != nil || changed {
				t.Fatalf("expired token finish changed=%v err=%v", changed, err)
			}
			f.noClaim(t, f.now.Add(24*time.Hour))
		})
	}
}

// TestPostgresWechatRewardNoticeSkipsInvalidUsers 验证缺失或删除账号、失效邮箱和ID错配不会发送邮件，也不阻塞正常通知。
func TestPostgresWechatRewardNoticeSkipsInvalidUsers(t *testing.T) {
	f := newWechatNoticeFixture(t)
	missing := f.grant(t, "a-missing", 2, "")
	deleted := f.grant(t, "b-deleted", 2, "")
	invalid := f.grant(t, "c-invalid", 2, "")
	mismatch := f.grant(t, "d-mismatch", 2, "")
	healthy := f.grant(t, "z-healthy", 2, "")
	if _, err := f.db.Exec(`UPDATE wechat_follow_rewards SET user_id=NULL WHERE user_email=$1`, missing.email); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`DELETE FROM users WHERE email=$1`, deleted.email); err != nil {
		t.Fatal(err)
	}
	invalidAddress := f.prefix + "not-an-email"
	if _, err := f.db.Exec(`UPDATE users SET email=$2 WHERE email=$1`, invalid.email, invalidAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE wechat_follow_rewards SET user_email=$2 WHERE user_email=$1`, invalid.email, invalidAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE wechat_follow_rewards SET user_id=(SELECT id FROM users WHERE email=$2) WHERE user_email=$1`, mismatch.email, healthy.email); err != nil {
		t.Fatal(err)
	}
	f.claim(t, f.now, healthy.email)
	for _, email := range []string{missing.email, deleted.email, invalidAddress, mismatch.email} {
		state := f.state(t, email)
		if state.status != "skipped" || state.attempts != 0 || state.next.Valid || state.token.Valid || state.lease.Valid {
			t.Fatalf("invalid account %s state=%+v", email, state)
		}
	}
}

// TestPostgresWechatRewardNoticeFinishValidation 验证写回状态严格校验，不允许旧身份或无计划重试破坏账本。
func TestPostgresWechatRewardNoticeFinishValidation(t *testing.T) {
	f := newWechatNoticeFixture(t)
	gift := f.grant(t, "validation", 2, "")
	job := f.claim(t, f.now, gift.email)
	for _, status := range []string{"", "unknown", "pending", "sending", "skipped"} {
		if changed, err := f.notices.Finish(context.Background(), job, status, f.now); err == nil || changed {
			t.Fatalf("invalid status %q changed=%v err=%v", status, changed, err)
		}
	}
	if changed, err := f.notices.Finish(context.Background(), job, "retry", time.Time{}); err == nil || changed {
		t.Fatalf("retry without time changed=%v err=%v", changed, err)
	}
	wrong := job
	wrong.ClaimToken = strings.Repeat("0", 64)
	if changed, err := f.notices.Finish(context.Background(), wrong, "sent", time.Time{}); err != nil || changed {
		t.Fatalf("wrong token changed=%v err=%v", changed, err)
	}
	if state := f.state(t, gift.email); state.status != "sending" || state.attempts != 1 || state.token.String != job.ClaimToken {
		t.Fatalf("validation mutated claimed notice=%+v", state)
	}
}

// TestWechatRewardNoticeEmailValidation 验证允许单个正常邮箱，拒绝多个目标、展示名称、空白和控制字符。
func TestWechatRewardNoticeEmailValidation(t *testing.T) {
	for _, value := range []string{"hr@example.com", "HR+gift@Example.COM"} {
		if !validWechatRewardNoticeEmail(value) {
			t.Errorf("valid email rejected: %q", value)
		}
	}
	for _, value := range []string{"", "missing-at", " hr@example.com", "hr@example.com ", "Name <hr@example.com>", "a@example.com,b@example.com", "a@example.com\r\nBCC: other@example.com"} {
		if validWechatRewardNoticeEmail(value) {
			t.Errorf("invalid email accepted: %q", value)
		}
	}
}

// TestPostgresWechatRewardNoticeMigrationBridgeAndReplay 验证历史跳过、新旧后端新增默认待发送，及迁移重跑不改变已有状态。
func TestPostgresWechatRewardNoticeMigrationBridgeAndReplay(t *testing.T) {
	db := openWechatTestDatabase(t)
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	schema := pq.QuoteIdentifier("notice_migration_" + fmt.Sprint(time.Now().UnixNano()))
	if _, err = conn.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.ExecContext(ctx, `SET search_path=public`); err != nil {
			t.Errorf("reset isolated migration path: %v", err)
		}
		if _, err := conn.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Errorf("drop isolated migration schema: %v", err)
		}
	})
	if _, err = conn.ExecContext(ctx, `SET search_path=`+schema+`,public`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE wechat_follow_rewards (user_email TEXT PRIMARY KEY,applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO wechat_follow_rewards(user_email) VALUES ('historical@example.com')`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../db/migrations/0086_wechat_follow_reward_notice.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var historicalStatus string
	var historicalNext sql.NullTime
	if err = conn.QueryRowContext(ctx, `SELECT mail_status,mail_next_attempt_at FROM wechat_follow_rewards WHERE user_email='historical@example.com'`).Scan(&historicalStatus, &historicalNext); err != nil || historicalStatus != "skipped" || historicalNext.Valid {
		t.Fatalf("historical state=%s next=%+v err=%v", historicalStatus, historicalNext, err)
	}
	// 模拟仍在运行的旧后端：INSERT没有通知字段，数据库默认值必须桥接到pending。
	if _, err = conn.ExecContext(ctx, `INSERT INTO wechat_follow_rewards(user_email) VALUES ('old-writer@example.com')`); err != nil {
		t.Fatal(err)
	}
	var newStatus string
	var next time.Time
	var attempts int
	if err = conn.QueryRowContext(ctx, `SELECT mail_status,mail_next_attempt_at,mail_attempts FROM wechat_follow_rewards WHERE user_email='old-writer@example.com'`).Scan(&newStatus, &next, &attempts); err != nil || newStatus != "pending" || next.IsZero() || attempts != 0 {
		t.Fatalf("legacy writer state=%s next=%v attempts=%d err=%v", newStatus, next, attempts, err)
	}
	if _, err = conn.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var unchanged bool
	if err = conn.QueryRowContext(ctx, `SELECT
		(SELECT mail_status='skipped' AND mail_next_attempt_at IS NULL FROM wechat_follow_rewards WHERE user_email='historical@example.com')
		AND (SELECT mail_status='pending' AND mail_next_attempt_at=$1 FROM wechat_follow_rewards WHERE user_email='old-writer@example.com')`, next).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("repeated migration changed rows=%v err=%v", !unchanged, err)
	}
	for _, bad := range []string{
		`INSERT INTO wechat_follow_rewards(user_email,mail_status) VALUES ('bad-status@example.com','unknown')`,
		`INSERT INTO wechat_follow_rewards(user_email,mail_attempts) VALUES ('bad-attempts@example.com',-1)`,
		`INSERT INTO wechat_follow_rewards(user_email,mail_status,mail_next_attempt_at) VALUES ('bad-next@example.com','retry',NULL)`,
		`INSERT INTO wechat_follow_rewards(user_email,mail_status) VALUES ('bad-lease@example.com','sending')`,
		`INSERT INTO wechat_follow_rewards(user_email,mail_status) VALUES ('bad-sent@example.com','sent')`,
		`INSERT INTO wechat_follow_rewards(user_email,mail_claim_token) VALUES ('bad-token@example.com','not-a-token')`,
	} {
		if _, err = conn.ExecContext(ctx, bad); err == nil {
			t.Fatalf("missing notice constraint: %s", bad)
		}
	}
	var comments, constraints, indexes int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_attribute a JOIN pg_description d ON d.objoid=a.attrelid AND d.objsubid=a.attnum
		WHERE a.attrelid='wechat_follow_rewards'::regclass AND a.attname LIKE 'mail_%' AND d.description<>''`).Scan(&comments); err != nil || comments != 6 {
		t.Fatalf("notice comments=%d err=%v", comments, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint WHERE conrelid='wechat_follow_rewards'::regclass AND conname LIKE 'wechat_reward_notice_%'`).Scan(&constraints); err != nil || constraints != 6 {
		t.Fatalf("notice constraints=%d err=%v", constraints, err)
	}
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid
		WHERE i.indrelid='wechat_follow_rewards'::regclass AND c.relname LIKE 'idx_wechat_reward_notice_%' AND i.indpred IS NOT NULL`).Scan(&indexes); err != nil || indexes != 2 {
		t.Fatalf("partial indexes=%d err=%v", indexes, err)
	}
}
