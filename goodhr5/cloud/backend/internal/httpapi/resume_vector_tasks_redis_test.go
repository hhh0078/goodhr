// 本文件仅启动自有回环端口Redis临时进程，验证真实Lua任务租约与并发限制，绝不读取环境中的业务Redis。
package httpapi

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// vectorRedisForTest 启动无持久化的独立Redis并在测试结束关闭，不允许借用或清空现有Redis实例。
func vectorRedisForTest(t *testing.T) *redis.Client {
	t.Helper()
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("未安装redis-server，跳过独立Redis集成测试")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	process := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir(), "--protected-mode", "yes")
	process.Stdout, process.Stderr = io.Discard, io.Discard
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Signal(os.Interrupt); _ = process.Wait() })
	client := redis.NewClient(&redis.Options{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if client.Ping(context.Background()).Err() == nil {
			return client
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("独立Redis没有启动成功")
	return nil
}

// TestResumeVectorTasksRedis 验证多个服务实例共享全局两名额、账号幂等、固定七日期限和过期执行者拒绝。
func TestResumeVectorTasksRedis(t *testing.T) {
	client := vectorRedisForTest(t)
	firstStore, secondStore := &redisResumeVectorTasks{client: client}, &redisResumeVectorTasks{client: client}
	ctx := context.Background()
	for _, owner := range []string{"first", "second", "third"} {
		now := time.Now()
		task := resumeVectorTask{ID: vectorTaskID(), Owner: owner, TenantID: "tenant", Status: "queued", Stage: "ready", CreatedAt: now, ExpiresAt: now.Add(resumeVectorTaskTTL)}
		if _, err := firstStore.Reserve(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	first, ok, err := firstStore.Claim(ctx, "first")
	if err != nil || !ok {
		t.Fatalf("领取第一个=%v", err)
	}
	if _, ok, err = secondStore.Claim(ctx, "second"); err != nil || !ok {
		t.Fatalf("领取第二个=%v", err)
	}
	if _, ok, err = secondStore.Claim(ctx, "third"); err != nil || ok {
		t.Fatalf("跨实例并发限制错误=%v", err)
	}
	duplicate, err := secondStore.Reserve(ctx, resumeVectorTask{ID: "duplicate", Owner: "first", Status: "queued", ExpiresAt: time.Now().Add(resumeVectorTaskTTL)})
	if err != nil || duplicate.ID != first.ID {
		t.Fatal("重复账号任务未复用")
	}
	before := client.PTTL(ctx, vectorTaskKey("first")).Val()
	expected := first.Version
	first.Version++
	first.Status = "completed"
	first.Stage = "done"
	first.LeaseUntil = 0
	if ok, err = firstStore.Save(ctx, first, expected); err != nil || !ok {
		t.Fatalf("保存终态=%v", err)
	}
	after := client.PTTL(ctx, vectorTaskKey("first")).Val()
	if before <= 0 || after <= 0 || after > before+10*time.Millisecond {
		t.Fatal("保存任务延长或丢失了固定保留期")
	}
	if _, ok, err = secondStore.Claim(ctx, "third"); err != nil || !ok {
		t.Fatalf("终态没有释放全局名额=%v", err)
	}
	if ok, err = firstStore.Save(ctx, first, expected); err != nil || ok {
		t.Fatal("旧版本任务覆盖了新回执")
	}
	pending, err := firstStore.Pending(ctx)
	if err != nil || len(pending) != 2 {
		t.Fatalf("恢复列表数量=%d err=%v", len(pending), err)
	}
	if _, err = firstStore.Get(ctx, "unknown"); err != ErrNotFound {
		t.Fatal("未知账号读到了任务")
	}
}
