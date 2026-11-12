// 本文件用内存和独立Redis实例验证短码碰撞、回调幂等、跨实例共享和并发单次消费。
package httpapi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestWechatLoginCacheAtomic 验证生产Lua脚本与开发缓存具有相同的一次认领及消费语义。
func TestWechatLoginCacheAtomic(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			c := newWechatLoginCache(nil, func() time.Time { return now })
			other := c
			if mode == "redis" {
				client := wechatLoginTestRedis(t)
				c.client = client
				other = newWechatLoginCache(nil, func() time.Time { return now })
				other.client = client
			}
			ctx := context.Background()
			ticket := strings.Repeat("a", 64)
			code := wechatBindingCodeHash("12345678")
			value := wechatLoginChallenge{TicketHash: ticket, CodeHash: code, CreatedAt: now.Unix(), ExpiresAt: now.Add(wechatLoginTTL).Unix(), AppID: "wx-test"}
			if ok, err := c.Create(ctx, value, ""); err != nil || !ok {
				t.Fatal(ok, err)
			}
			collision := value
			collision.TicketHash = strings.Repeat("b", 64)
			if ok, err := other.Create(ctx, collision, ""); err != nil || ok {
				t.Fatal("短码碰撞覆盖旧页面", ok, err)
			}
			if ok, err := other.Approve(ctx, code, "wx-test", "owner", now.Add(-time.Second).Unix()); err != nil || ok {
				t.Fatal("旧消息被接受", ok, err)
			}
			for i := 0; i < 2; i++ {
				if ok, err := other.Approve(ctx, code, "wx-test", "owner", now.Unix()); err != nil || !ok {
					t.Fatal("正确回调或重放失败", ok, err)
				}
			}
			if ok, err := c.Approve(ctx, code, "wx-test", "other", now.Unix()); err != nil || ok {
				t.Fatal("另一微信覆盖页面", ok, err)
			}
			if value, err := c.Read(ctx, ticket, false); err != nil || value.OpenID != "owner" {
				t.Fatal(value, err)
			}
			var successes atomic.Int32
			var group sync.WaitGroup
			for i := 0; i < 10; i++ {
				group.Add(1)
				go func() {
					defer group.Done()
					value, err := other.Read(ctx, ticket, true)
					if err == nil && value.OpenID == "owner" {
						successes.Add(1)
					} else if !errors.Is(err, ErrNotFound) {
						t.Errorf("并发消费异常：%+v %v", value, err)
					}
				}()
			}
			group.Wait()
			if successes.Load() != 1 {
				t.Fatalf("登录凭据被消费%d次", successes.Load())
			}
			if ok, err := c.Approve(ctx, code, "wx-test", "owner", now.Unix()); err != nil || ok {
				t.Fatal("消费后又被回调复活", ok, err)
			}
			if ok, err := c.Create(ctx, value, ""); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if err := other.Cancel(ctx, ticket); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Read(ctx, ticket, false); !errors.Is(err, ErrNotFound) {
				t.Fatal("刷新后旧凭据仍有效", err)
			}
			if ok, err := c.Approve(ctx, code, "wx-test", "owner", now.Unix()); err != nil || ok {
				t.Fatal("刷新后旧码仍可被认领", ok, err)
			}
		})
	}
}

// wechatLoginTestRedis 启动只监听临时Unix套接字的无持久化Redis，结束后只清理本测试创建的进程与目录。
func wechatLoginTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("当前环境没有本地redis-server，内存回归仍执行")
	}
	dir, err := os.MkdirTemp("/tmp", "goodhr-wx-redis-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "redis.sock")
	cmd := exec.Command(binary, "--port", "0", "--unixsocket", socket, "--save", "", "--appendonly", "no")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 50; i++ {
		if client.Ping(ctx).Err() == nil {
			return client
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("隔离Redis未启动")
	return nil
}

// TestWechatLoginReplaceAndGeneration 验证Redis和内存换码均先保住旧码，正常换码超过五次仍可用且防刷窗口有效。
func TestWechatLoginReplaceAndGeneration(t *testing.T) {
	for _, mode := range []string{"memory", "redis"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC()
			c := newWechatLoginCache(nil, func() time.Time { return now })
			if mode == "redis" {
				c.client = wechatLoginTestRedis(t)
			}
			ctx := context.Background()
			old := wechatLoginChallenge{TicketHash: strings.Repeat("a", 64), CodeHash: wechatBindingCodeHash("1234"), AppID: "wx-test", CreatedAt: now.Unix(), ExpiresAt: now.Add(wechatLoginTTL).Unix()}
			if ok, err := c.Create(ctx, old, ""); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if ok, err := c.HasCode(ctx, old.CodeHash, old.AppID); err != nil || !ok {
				t.Fatal("有效码未识别", ok, err)
			}
			if ok, err := c.HasCode(ctx, old.CodeHash, "other-app"); err != nil || ok {
				t.Fatal("跨公众号识别登录码", ok, err)
			}
			next := old
			next.TicketHash = strings.Repeat("b", 64)
			if ok, err := c.Create(ctx, next, old.TicketHash); err != nil || ok {
				t.Fatal("碰撞不应创建新码", ok, err)
			}
			if _, err := c.Read(ctx, old.TicketHash, false); err != nil {
				t.Fatal("失败换码丢失原码", err)
			}
			next.CodeHash = wechatBindingCodeHash("5678")
			if ok, err := c.Create(ctx, next, old.TicketHash); err != nil || !ok {
				t.Fatal(ok, err)
			}
			if _, err := c.Read(ctx, old.TicketHash, false); !errors.Is(err, ErrNotFound) {
				t.Fatal("成功换码后旧码未失效", err)
			}
			if ok, err := c.Approve(ctx, next.CodeHash, next.AppID, "existing-follower", now.Unix()); err != nil || !ok {
				t.Fatal(ok, err)
			}
			for i := 0; i < 60; i++ {
				if ok, err := c.TakeGeneration(ctx, "test-ip"); err != nil || !ok {
					t.Fatalf("第%d次生成错误受限：%v %v", i+1, ok, err)
				}
			}
			if ok, err := c.TakeGeneration(ctx, "test-ip"); err != nil || ok {
				t.Fatal("未保留防刷限制", ok, err)
			}
			if _, err := c.Read(ctx, next.TicketHash, false); err != nil {
				t.Fatal("防刷限制影响现有码", err)
			}
			if ok, err := c.TakeGeneration(ctx, "other-ip"); err != nil || !ok {
				t.Fatal("其他来源受限", ok, err)
			}
		})
	}
}
