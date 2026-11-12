// 本文件在专用隔离PostgreSQL验证公众号身份的真实迁移、唯一归属、并发注册与取消关注后的账号保留。
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestWechatLoginPostgres 验证同一微信并发登录只创建一个账号，已有邮箱绑定优先，取消关注不丢账号。
func TestWechatLoginPostgres(t *testing.T) {
	db := openWechatTestDatabase(t)
	store := NewPostgresWechatFollowStore(db)
	ctx := context.Background()
	appID := fmt.Sprintf("wechat-login-test-%d", time.Now().UnixNano())
	openID := "new-openid"
	if _, err := store.FindWechatLogin(ctx, appID, openID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未注册身份不应被查到：%v", err)
	}
	var group sync.WaitGroup
	emails := make(chan string, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			email, err := store.ResolveWechatLogin(ctx, appID, openID)
			if err != nil {
				t.Errorf("并发注册失败：%v", err)
				return
			}
			emails <- email
		}()
	}
	group.Wait()
	close(emails)
	unique := map[string]bool{}
	for email := range emails {
		unique[email] = true
		if !isWechatAccount(email) {
			t.Fatal(email)
		}
	}
	if len(unique) != 1 {
		t.Fatalf("同一微信生成了%d个账号", len(unique))
	}
	var original string
	for email := range unique {
		original = email
	}
	if err := store.RecordFollow(ctx, appID, openID, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if email, err := store.FindWechatLogin(ctx, appID, openID); err != nil || email != original {
		t.Fatalf("取消关注后只读查询丢失账号：%q %v", email, err)
	}
	if email, err := store.ResolveWechatLogin(ctx, appID, openID); err != nil || email != original {
		t.Fatalf("取消关注后原账号丢失：%q %v", email, err)
	}
	bound := fmt.Sprintf("bound-%d@example.com", time.Now().UnixNano())
	if _, err := db.Exec(`INSERT INTO users(email,wechat_app_id,wechat_openid,wechat_bound_at) VALUES($1,$2,$3,NOW())`, bound, appID, "bound-openid"); err != nil {
		t.Fatal(err)
	}
	if email, err := store.FindWechatLogin(ctx, appID, "bound-openid"); err != nil || email != bound {
		t.Fatalf("只读查询没有识别现有邮箱绑定：%q %v", email, err)
	}
	if email, err := store.ResolveWechatLogin(ctx, appID, "bound-openid"); err != nil || email != bound {
		t.Fatalf("没有复用现有邮箱绑定：%q %v", email, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM wechat_login_identities WHERE app_id=$1`, appID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	var missing int
	if err := db.QueryRow(`SELECT count(*) FROM pg_attribute WHERE attrelid='wechat_login_identities'::regclass AND attnum>0 AND NOT attisdropped AND COALESCE(col_description(attrelid,attnum),'')=''`).Scan(&missing); err != nil || missing != 0 {
		t.Fatalf("新表字段缺少中文备注：%d %v", missing, err)
	}
	if strings.Contains(original, openID) {
		t.Fatal("内部账号泄露原始微信标识")
	}
	targets, err := NewPostgresEmailCampaignStore(db).FindTargetUsers(EmailTargetFilter{})
	if err != nil {
		t.Fatal(err)
	}
	foundBound := false
	for _, target := range targets {
		if isWechatAccount(target.Email) {
			t.Fatal("邮件群发选中了免邮箱账号", target.Email)
		}
		foundBound = foundBound || target.Email == bound
	}
	if !foundBound {
		t.Fatal("普通邮箱用户被邮件群发错误排除")
	}
}
