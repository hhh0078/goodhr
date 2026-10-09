// 本文件用 PostgreSQL 事务保存公众号关注绑定及永久领取账本，确保取消重关与并发回调不会重复赠送。
package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"time"

	"github.com/lib/pq"
)

// PostgresWechatFollowStore 共享云端用户表，只在当前绑定中保留原始 OpenID。
type PostgresWechatFollowStore struct {
	db *sql.DB
}

// NewPostgresWechatFollowStore 创建生产公众号存储，迁移由现有启动入口统一执行。
func NewPostgresWechatFollowStore(db *sql.DB) *PostgresWechatFollowStore {
	return &PostgresWechatFollowStore{db: db}
}

// CreateBinding 锁定用户后使旧未完成码失效，并原子保存新的 SHA256 码摘要。
func (s *PostgresWechatFollowStore) CreateBinding(ctx context.Context, email, codeHash string, expiresAt time.Time) error {
	email, codeHash, err := normalizeWechatBindingInput(email, codeHash)
	if err != nil {
		return err
	}
	if expiresAt.IsZero() {
		return ErrWechatBindingExpired
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockWechatBindingCode(ctx, tx, codeHash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (email) VALUES ($1) ON CONFLICT (email) DO NOTHING`, email); err != nil {
		return err
	}
	var userID string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&userID); err != nil {
		return err
	}
	createdAt := wechatBindingCreatedAt(expiresAt)
	var savedHash string
	if err = tx.QueryRowContext(ctx, `
		INSERT INTO wechat_follow_binding_codes (code_hash,user_id,user_email,expires_at,created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (code_hash) DO UPDATE SET user_id=EXCLUDED.user_id,user_email=EXCLUDED.user_email,
			expires_at=EXCLUDED.expires_at,created_at=EXCLUDED.created_at,invalidated_at=NULL,
			completed_at=NULL,completed_app_id=NULL,completed_openid_fingerprint=NULL
		WHERE wechat_follow_binding_codes.expires_at<=EXCLUDED.created_at
		RETURNING code_hash
	`, codeHash, userID, email, expiresAt, createdAt).Scan(&savedHash); errors.Is(err, sql.ErrNoRows) {
		return ErrWechatBindingCodeConflict
	} else if err != nil {
		return err
	}
	// 先占用候选码再作废其他仍有效旧码，避免活跃码碰撞与过期交叉回收锁住对方历史行。
	if _, err = tx.ExecContext(ctx, `
		UPDATE wechat_follow_binding_codes SET invalidated_at=$2
		WHERE user_email=$1 AND code_hash<>$3 AND completed_at IS NULL AND invalidated_at IS NULL AND expires_at>$2
	`, email, createdAt, codeHash); err != nil {
		return err
	}
	return tx.Commit()
}

// BindingStatus 只读取当前原始绑定和邮箱历史奖励，未绑定用户也能查到曾经领取过的记录。
func (s *PostgresWechatFollowStore) BindingStatus(ctx context.Context, email string) (WechatBindingStatus, error) {
	email, ok := normalizeEmail(email)
	if !ok {
		return WechatBindingStatus{}, errors.New("账号邮箱不正确")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var status WechatBindingStatus
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(u.wechat_openid,'') <> '',
			EXISTS (SELECT 1 FROM wechat_follow_rewards r WHERE r.user_email=$1),
			COALESCE((SELECT r.duration_days FROM wechat_follow_rewards r WHERE r.user_email=$1),0),
			COALESCE(f.followed,false),
			COALESCE(u.wechat_openid,'')
		FROM (SELECT $1::text AS email) identity
		LEFT JOIN users u ON u.email=identity.email
		LEFT JOIN wechat_follow_states f ON f.app_id=u.wechat_app_id
			AND f.openid_fingerprint=encode(sha256(convert_to(u.wechat_openid,'UTF8')),'hex')
	`, email).Scan(&status.Bound, &status.GiftGranted, &status.GiftDays, &status.Followed, &status.OpenID)
	return status, err
}

// RecordFollow 串行处理同一微信的事件，只有新事件能修改状态和清理当前绑定。
func (s *PostgresWechatFollowStore) RecordFollow(ctx context.Context, appID, openID string, followed bool, eventAt time.Time) error {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return err
	}
	if eventAt.IsZero() {
		return errors.New("关注事件缺少时间")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	fingerprint := wechatOpenIDFingerprint(openID)
	if err = lockWechatFollowIdentity(ctx, tx, appID, fingerprint); err != nil {
		return err
	}
	var recorded time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO wechat_follow_states (app_id,openid_fingerprint,followed,event_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (app_id,openid_fingerprint) DO UPDATE
		SET followed=EXCLUDED.followed,event_at=EXCLUDED.event_at
		WHERE EXCLUDED.event_at > wechat_follow_states.event_at
			OR (EXCLUDED.event_at=wechat_follow_states.event_at AND wechat_follow_states.followed AND NOT EXCLUDED.followed)
		RETURNING event_at
	`, appID, fingerprint, followed, eventAt).Scan(&recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if !followed {
		if _, err = tx.ExecContext(ctx, `
			UPDATE users SET wechat_app_id=NULL,wechat_openid=NULL,wechat_bound_at=NULL
			WHERE wechat_app_id=$1 AND wechat_openid=$2
		`, appID, openID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BindCode 将用户行锁、关注验证、消费绑定码和奖励入账放进同一个事务。
func (s *PostgresWechatFollowStore) BindCode(ctx context.Context, appID, openID, codeHash string, now time.Time, options ...WechatBindOptions) (WechatBindResult, error) {
	appID, openID, codeHash, err := normalizeWechatBindCodeInput(appID, openID, codeHash, now)
	if err != nil {
		return WechatBindResult{}, err
	}
	option, err := normalizeWechatBindOptions(options)
	if err != nil {
		return WechatBindResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WechatBindResult{}, err
	}
	defer tx.Rollback()
	if err = lockWechatBindingCode(ctx, tx, codeHash); err != nil {
		return WechatBindResult{}, err
	}
	var email string
	if err = tx.QueryRowContext(ctx, `SELECT user_email FROM wechat_follow_binding_codes WHERE code_hash=$1`, codeHash).Scan(&email); errors.Is(err, sql.ErrNoRows) {
		return WechatBindResult{}, ErrWechatBindingExpired
	} else if err != nil {
		return WechatBindResult{}, err
	}
	fingerprint := wechatOpenIDFingerprint(openID)
	if err = lockWechatFollowIdentity(ctx, tx, appID, fingerprint); err != nil {
		return WechatBindResult{}, err
	}
	var userID, boundAppID, boundOpenID string
	var rawSubscription []byte
	if err = tx.QueryRowContext(ctx, `
		SELECT id::text,subscription,COALESCE(wechat_app_id,''),COALESCE(wechat_openid,'')
		FROM users WHERE email=$1 FOR UPDATE
	`, email).Scan(&userID, &rawSubscription, &boundAppID, &boundOpenID); errors.Is(err, sql.ErrNoRows) {
		return WechatBindResult{}, ErrWechatBindingExpired
	} else if err != nil {
		return WechatBindResult{}, err
	}
	current, err := parseSubscription(rawSubscription)
	if err != nil {
		return WechatBindResult{}, err
	}
	var expiresAt, createdAt time.Time
	var completedAt sql.NullTime
	var invalidated bool
	var completedAppID, completedFingerprint string
	if err = tx.QueryRowContext(ctx, `
		SELECT expires_at,created_at,completed_at,invalidated_at IS NOT NULL,
			COALESCE(completed_app_id,''),COALESCE(completed_openid_fingerprint,'')
		FROM wechat_follow_binding_codes WHERE code_hash=$1 FOR UPDATE
	`, codeHash).Scan(&expiresAt, &createdAt, &completedAt, &invalidated, &completedAppID, &completedFingerprint); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WechatBindResult{}, ErrWechatBindingExpired
		}
		return WechatBindResult{}, err
	}
	if invalidated || !now.Before(expiresAt) || wechatMessagePredatesBinding(option.MessageTime, createdAt) {
		return WechatBindResult{}, ErrWechatBindingExpired
	}
	alreadyBound := boundOpenID != ""
	if completedAt.Valid && (completedAppID != appID || completedFingerprint != fingerprint || boundAppID != appID || boundOpenID != openID) {
		return WechatBindResult{}, ErrWechatBindingExpired
	}
	if option.ReplayOnly && !completedAt.Valid {
		return WechatBindResult{}, ErrWechatBindingRateLimited
	}
	var followed bool
	err = tx.QueryRowContext(ctx, `
		SELECT followed FROM wechat_follow_states WHERE app_id=$1 AND openid_fingerprint=$2
	`, appID, fingerprint).Scan(&followed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !followed) {
		return WechatBindResult{}, ErrWechatFollowRequired
	}
	if err != nil {
		return WechatBindResult{}, err
	}
	if alreadyBound && (boundAppID != appID || boundOpenID != openID) {
		return WechatBindResult{}, ErrWechatOpenIDConflict
	}
	var boundElsewhere bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM users WHERE id<>$1 AND wechat_app_id=$2 AND wechat_openid=$3)
	`, userID, appID, openID).Scan(&boundElsewhere); err != nil {
		return WechatBindResult{}, err
	}
	if boundElsewhere {
		return WechatBindResult{}, ErrWechatOpenIDConflict
	}
	if completedAt.Valid {
		if err = tx.Commit(); err != nil {
			return WechatBindResult{}, err
		}
		return WechatBindResult{AlreadyBound: true, Subscription: current}, nil
	}
	next := calculateWechatFollowGift(current, now, option.RewardDays)
	var grantedEmail string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO wechat_follow_rewards (
			user_id,user_email,app_id,openid_fingerprint,duration_days,member_type,subscription_expires_at,applied_at,
			mail_status,mail_next_attempt_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'pending',$8)
		ON CONFLICT DO NOTHING RETURNING user_email
	`, userID, email, appID, fingerprint, option.RewardDays, next.MemberType, next.ExpiresAt, now).Scan(&grantedEmail)
	granted := err == nil
	if !granted && !errors.Is(err, sql.ErrNoRows) {
		return WechatBindResult{}, err
	}
	if !granted {
		next = current
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE users SET wechat_app_id=$2,wechat_openid=$3,wechat_bound_at=$4,
			subscription=CASE WHEN $5 THEN subscription || jsonb_build_object('member_type',$6::text,'expires_at',$7::text) ELSE subscription END
		WHERE id=$1
	`, userID, appID, openID, now, granted, next.MemberType, next.ExpiresAt.UTC().Format(time.RFC3339Nano)); err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return WechatBindResult{}, ErrWechatOpenIDConflict
		}
		return WechatBindResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE wechat_follow_binding_codes SET completed_at=$2,completed_app_id=$3,completed_openid_fingerprint=$4
		WHERE code_hash=$1
	`, codeHash, now, appID, fingerprint); err != nil {
		return WechatBindResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return WechatBindResult{}, err
	}
	return WechatBindResult{AlreadyBound: alreadyBound, GiftGranted: granted, Subscription: next}, nil
}

// lockWechatBindingCode 串行化同一码的创建、过期回收与消费，防止读取旧邮箱后绑定到新挑战。
func lockWechatBindingCode(ctx context.Context, tx *sql.Tx, codeHash string) error {
	sum := sha256.Sum256([]byte("wechat-binding-code\x00" + codeHash))
	key := int64(binary.BigEndian.Uint64(sum[:8]))
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key)
	return err
}

// lockWechatFollowIdentity 对同一公众号用户加事务锁，统一关注事件和绑定的锁顺序，避免互相死锁。
func lockWechatFollowIdentity(ctx context.Context, tx *sql.Tx, appID, fingerprint string) error {
	sum := sha256.Sum256([]byte(appID + "\x00" + fingerprint))
	key := int64(binary.BigEndian.Uint64(sum[:8]))
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key)
	return err
}
