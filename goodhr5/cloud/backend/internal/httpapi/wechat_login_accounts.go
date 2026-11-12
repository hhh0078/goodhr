// 本文件按经过微信签名验证的身份查找或创建免邮箱账号，持久指纹保证取消关注后再次登录仍回到原账号。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const wechatAccountDomain = "wechat.goodhr.invalid"

// WechatLoginAccountStore 只接受服务端已验证的微信身份，绝不按客户端提交的邮箱自动合并账号。
type WechatLoginAccountStore interface {
	FindWechatLogin(ctx context.Context, appID, openID string) (string, error)
	ResolveWechatLogin(ctx context.Context, appID, openID string) (string, error)
}

// FindWechatLogin 只查已存在的微信账号，不在协议确认前创建用户。
func (s *MemoryWechatFollowStore) FindWechatLogin(ctx context.Context, appID, openID string) (string, error) {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	for email, binding := range s.bindings {
		if binding.appID == appID && binding.openID == openID {
			return email, nil
		}
	}
	if email := s.loginIdentities[wechatFollowKey(appID, openID)]; email != "" {
		return email, nil
	}
	return "", ErrNotFound
}

// FindWechatLogin 只读查询已绑定账号或永久身份关联，用于识别已同意协议的老用户。
func (s *PostgresWechatFollowStore) FindWechatLogin(ctx context.Context, appID, openID string) (string, error) {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var email string
	err = s.db.QueryRowContext(ctx, `SELECT email FROM (
	 SELECT email,0 AS priority FROM users WHERE wechat_app_id=$1 AND wechat_openid=$2
	 UNION ALL SELECT user_email,1 FROM wechat_login_identities WHERE app_id=$1 AND openid_fingerprint=$3
	) identities ORDER BY priority LIMIT 1`, appID, openID, wechatOpenIDFingerprint(openID)).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return email, err
}

// isWechatAccount 判断内部免邮箱账号标识；此标识仅用于兼容现有账号关联，不是真实收件地址。
func isWechatAccount(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@"+wechatAccountDomain)
}

// accountDisplayName 为免邮箱账号返回可读名称，不在界面上展示内部关联标识。
func accountDisplayName(email string) string {
	if isWechatAccount(email) {
		return "微信用户"
	}
	return email
}

// accountLoginIdentity 区分真实邮箱和公众号账号，供界面选择合适的账号说明。
func accountLoginIdentity(email string) string {
	if isWechatAccount(email) {
		return "wechat"
	}
	return "email"
}

// newWechatAccountEmail 生成不可猜测的内部账号标识，不将原始 OpenID 暴露在页面或邮箱字段中。
func newWechatAccountEmail() (string, error) {
	nonce, err := newWechatMessageNonce()
	if err != nil {
		return "", err
	}
	return "wx-" + nonce[:40] + "@" + wechatAccountDomain, nil
}

// ResolveWechatLogin 优先复用当前已验证的邮箱绑定，其次读取永久身份映射；仅第一次注册创建新账号。
func (s *MemoryWechatFollowStore) ResolveWechatLogin(ctx context.Context, appID, openID string) (string, error) {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return "", err
	}
	key := wechatFollowKey(appID, openID)
	for email, binding := range s.bindings {
		if binding.appID == appID && binding.openID == openID {
			s.loginIdentities[key] = email
			return email, nil
		}
	}
	if email := s.loginIdentities[key]; email != "" {
		return email, nil
	}
	email, err := newWechatAccountEmail()
	if err != nil {
		return "", err
	}
	s.loginIdentities[key] = email
	s.bindings[email] = wechatCurrentBinding{appID: appID, openID: openID}
	return email, nil
}

// ResolveWechatLogin 在微信身份锁内复用已绑定账号或创建免邮箱账号，并保存取消关注后仍有效的身份指纹。
func (s *PostgresWechatFollowStore) ResolveWechatLogin(ctx context.Context, appID, openID string) (string, error) {
	appID, openID, err := normalizeWechatIdentity(appID, openID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	fingerprint := wechatOpenIDFingerprint(openID)
	if err = lockWechatFollowIdentity(ctx, tx, appID, fingerprint); err != nil {
		return "", err
	}
	var email string
	err = tx.QueryRowContext(ctx, `SELECT email FROM users WHERE wechat_app_id=$1 AND wechat_openid=$2`, appID, openID).Scan(&email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if email == "" {
		err = tx.QueryRowContext(ctx, `SELECT user_email FROM wechat_login_identities WHERE app_id=$1 AND openid_fingerprint=$2`, appID, fingerprint).Scan(&email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	if email == "" {
		email, err = newWechatAccountEmail()
		if err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO users(email,wechat_app_id,wechat_openid,wechat_bound_at) VALUES($1,$2,$3,NOW())`, email, appID, openID); err != nil {
			return "", err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wechat_login_identities(app_id,openid_fingerprint,user_email) VALUES($1,$2,$3)
	 ON CONFLICT(app_id,openid_fingerprint) DO UPDATE SET user_email=EXCLUDED.user_email`, appID, fingerprint, email); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return email, nil
}
