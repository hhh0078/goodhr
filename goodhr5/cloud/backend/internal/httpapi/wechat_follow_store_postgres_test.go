// 本文件在显式配置的本机隔离 PostgreSQL 中验证真实迁移、事务、唯一约束和隐私清理；未配置时跳过。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// prefixedWechatFollowStore 为共享数据库测试隔离邮箱与码摘要，不修改生产存储的接口或规则。
type prefixedWechatFollowStore struct {
	WechatFollowStore
	prefix string
}

// email 将测试邮箱放入独立命名空间，避免永久领取账本影响其他测试。
func (s prefixedWechatFollowStore) email(email string) string { return s.prefix + email }

// hash 将有效测试码放入独立命名空间，原始无效输入继续交给生产校验拒绝。
func (s prefixedWechatFollowStore) hash(hash string) string {
	if _, err := normalizeWechatCodeHash(hash); err != nil {
		return hash
	}
	return wechatTestCodeHash(s.prefix + hash)
}

// CreateBinding 保存当前测试范围内的邮箱与码摘要。
func (s prefixedWechatFollowStore) CreateBinding(ctx context.Context, email, hash string, expiresAt time.Time) error {
	return s.WechatFollowStore.CreateBinding(ctx, s.email(email), s.hash(hash), expiresAt)
}

// BindingStatus 查询当前测试范围内的邮箱状态。
func (s prefixedWechatFollowStore) BindingStatus(ctx context.Context, email string) (WechatBindingStatus, error) {
	return s.WechatFollowStore.BindingStatus(ctx, s.email(email))
}

// BindCode 使用同一测试范围内的摘要执行真实数据库绑定。
func (s prefixedWechatFollowStore) BindCode(ctx context.Context, appID, openID, hash string, now time.Time, options ...WechatBindOptions) (WechatBindResult, error) {
	return s.WechatFollowStore.BindCode(ctx, appID, openID, s.hash(hash), now, options...)
}

// openWechatTestDatabase 仅允许专用本机测试库，防止集成测试意外连接或迁移生产数据库。
func openWechatTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("GOODHR_WECHAT_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GOODHR_WECHAT_TEST_PG_DSN is not configured")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Port() != "55439" || parsed.Path != "/goodhr_wechat_test" {
		t.Fatal("wechat integration tests require the isolated local goodhr_wechat_test database on port 55439")
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	db, err := (Config{PostgresDSN: dsn}).PostgresDB()
	if restoreErr := os.Chdir(workingDirectory); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newPostgresWechatFixture 为一组合同测试创建隔离身份，并共享真实 PostgreSQL 会员存储。
func newPostgresWechatFixture(t *testing.T, db *sql.DB) wechatStoreFixture {
	t.Helper()
	prefix := fmt.Sprintf("wechat-%d-", time.Now().UnixNano())
	store := prefixedWechatFollowStore{WechatFollowStore: NewPostgresWechatFollowStore(db), prefix: prefix}
	subscriptions := NewPostgresSubscriptionStore(db)
	now := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	return wechatStoreFixture{
		store: store, appID: prefix + "app", now: now,
		seed: func(email string, subscription Subscription) {
			if _, err := ensureUserID(context.Background(), db, store.email(email)); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(subscription)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE users SET subscription=$2::jsonb WHERE email=$1`, store.email(email), string(payload)); err != nil {
				t.Fatal(err)
			}
		},
		read: func(email string) Subscription {
			item, err := subscriptions.UserSubscription(store.email(email))
			if err != nil {
				t.Fatal(err)
			}
			return item
		},
	}
}

// TestPostgresWechatFollowStoreContract 验证与内存一致的业务边界，包括二十四个真实并发事务。
func TestPostgresWechatFollowStoreContract(t *testing.T) {
	db := openWechatTestDatabase(t)
	runWechatStoreContract(t, func(t *testing.T) wechatStoreFixture { return newPostgresWechatFixture(t, db) })
}

// TestPostgresWechatFollowPreservesJSONAndPermanentLedger 验证额外订阅字段、原始身份清理和账号删除后防重复记录。
func TestPostgresWechatFollowPreservesJSONAndPermanentLedger(t *testing.T) {
	db := openWechatTestDatabase(t)
	store := NewPostgresWechatFollowStore(db)
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	email, appID, openID := "wechat-ledger-"+stamp+"@example.com", "wx-ledger-"+stamp, "private-openid-"+stamp
	now := time.Now().UTC().Truncate(time.Microsecond)
	code := wechatTestCodeHash("ledger-" + stamp)
	if err := store.CreateBinding(context.Background(), email, code, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET subscription=$2::jsonb WHERE email=$1`, email,
		fmt.Sprintf(`{"member_type":"plus","expires_at":%q,"legacy_metadata":{"campaign":"keep-me"}}`, now.Add(20*24*time.Hour).Format(time.RFC3339Nano))); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFollow(context.Background(), appID, openID, true, now); err != nil {
		t.Fatal(err)
	}
	result, err := store.BindCode(context.Background(), appID, openID, code, now)
	if err != nil || !result.GiftGranted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertWechatSubscription(t, result.Subscription, memberTypePlus, now.Add(22*24*time.Hour))
	var rawSubscription string
	if err = db.QueryRow(`SELECT subscription::text FROM users WHERE email=$1`, email).Scan(&rawSubscription); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawSubscription, "keep-me") {
		t.Fatalf("extra subscription fields lost: %s", rawSubscription)
	}
	adminUsers, err := NewPostgresAdminUserStore(db).ListUsers(AdminUserListQuery{Query: email, Page: 1, PageSize: 20})
	if err != nil || len(adminUsers.Users) != 1 || !adminUsers.Users[0].HasOpenID {
		t.Fatalf("admin users=%+v err=%v", adminUsers, err)
	}
	if err = store.RecordFollow(context.Background(), appID, openID, false, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var rawOpenID sql.NullString
	if err = db.QueryRow(`SELECT wechat_openid FROM users WHERE email=$1`, email).Scan(&rawOpenID); err != nil {
		t.Fatal(err)
	}
	if rawOpenID.Valid {
		t.Fatal("raw OpenID retained after unsubscribe")
	}
	for _, table := range []string{"wechat_follow_states", "wechat_follow_binding_codes", "wechat_follow_rewards"} {
		var stored string
		if err = db.QueryRow(`SELECT COALESCE(json_agg(row_to_json(records))::text,'[]') FROM ` + table + ` records`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, openID) {
			t.Fatalf("raw OpenID persisted in %s", table)
		}
	}
	if _, err = db.Exec(`DELETE FROM users WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	status, err := store.BindingStatus(context.Background(), email)
	if err != nil || status.Bound || !status.GiftGranted {
		t.Fatalf("deleted user status=%+v err=%v", status, err)
	}
	code = wechatTestCodeHash("ledger-recreate-" + stamp)
	if err = store.CreateBinding(context.Background(), email, code, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordFollow(context.Background(), appID, openID, true, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	result, err = store.BindCode(context.Background(), appID, openID, code, now.Add(3*time.Second))
	if err != nil || result.GiftGranted {
		t.Fatalf("recreated account result=%+v err=%v", result, err)
	}
}

// TestPostgresWechatFollowTransactionRollback 验证会员数据错误不会消费码或留下奖励，修复后原码仍可使用。
func TestPostgresWechatFollowTransactionRollback(t *testing.T) {
	db := openWechatTestDatabase(t)
	store := NewPostgresWechatFollowStore(db)
	stamp := fmt.Sprintf("%d", time.Now().UnixNano())
	email, appID, openID := "wechat-rollback-"+stamp+"@example.com", "wx-rollback-"+stamp, "openid-rollback-"+stamp
	now := time.Now().UTC().Truncate(time.Microsecond)
	code := wechatTestCodeHash(stamp)
	if err := store.CreateBinding(context.Background(), email, code, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordFollow(context.Background(), appID, openID, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET subscription='{"member_type":"plus","expires_at":"bad-date"}'::jsonb WHERE email=$1`, email); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindCode(context.Background(), appID, openID, code, now); err == nil {
		t.Fatal("invalid subscription should fail")
	}
	var consumed bool
	if err := db.QueryRow(`SELECT completed_at IS NOT NULL FROM wechat_follow_binding_codes WHERE code_hash=$1`, code).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	status, err := store.BindingStatus(context.Background(), email)
	if err != nil || consumed || status.Bound || status.GiftGranted {
		t.Fatalf("rollback consumed=%v status=%+v err=%v", consumed, status, err)
	}
	if _, err = db.Exec(`UPDATE users SET subscription=jsonb_build_object('member_type','plus','expires_at',$2::text) WHERE email=$1`, email, now.Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	result, err := store.BindCode(context.Background(), appID, openID, code, now)
	if err != nil || !result.GiftGranted {
		t.Fatalf("retry result=%+v err=%v", result, err)
	}
	if _, err = store.BindCode(context.Background(), appID, "wrong-openid", code, now); !errors.Is(err, ErrWechatBindingExpired) {
		t.Fatalf("mismatch err=%v", err)
	}
}

// TestPostgresWechatCompactStoreContract 用隔离PostgreSQL验证短码碰撞、回收时序与动态账本的真实事务。
func TestPostgresWechatCompactStoreContract(t *testing.T) {
	db := openWechatTestDatabase(t)
	runWechatCompactStoreContract(t, func(t *testing.T) wechatStoreFixture { return newPostgresWechatFixture(t, db) })
}
