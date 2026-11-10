// Package platform 本文件提供仅限显式授权候选人的BOSS真页只读预检，不点击、输入、滚轮、切页或发送消息。
package platform

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/boss"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossReadonlyLivePreflight 仅在显式指定本机Worker和显式授权候选人身份时读取当前聊天，默认测试自动跳过。
func TestBossReadonlyLivePreflight(t *testing.T) {
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_BOSS_READONLY_WORKER_URL"))
	if workerURL == "" {
		t.Skip("真页预检仅由持有当前浏览器的执行者显式启用")
	}
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("只读预检只接受明确指定的本机Worker地址")
	}
	snapshot := model.AutoReplyConversationSnapshot{
		CandidateName:         strings.TrimSpace(os.Getenv("GOODHR_BOSS_READONLY_CANDIDATE_NAME")),
		PlatformCandidateID:   strings.TrimSpace(os.Getenv("GOODHR_BOSS_READONLY_CANDIDATE_ID")),
		PlatformThreadID:      strings.TrimSpace(os.Getenv("GOODHR_BOSS_READONLY_THREAD_ID")),
		CommunicationPosition: strings.TrimSpace(os.Getenv("GOODHR_BOSS_READONLY_POSITION")),
	}
	if snapshot.CandidateName == "" || strings.ContainsAny(snapshot.CandidateName, "*＊") || snapshot.PlatformCandidateID == "" || snapshot.PlatformThreadID == "" {
		t.Fatal("必须通过环境变量提供显式授权候选人的完整姓名、原生编号和会话编号")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := loadBossActionConfig(t)
	browser := client.New(workerURL)
	runtime := boss.NewRuntime()
	started := time.Now()
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatalf("读取前核身未通过：%v", err)
	}
	t.Logf("读取前核身耗时：%s", time.Since(started))
	fields := map[string]contract.SelectorSpec{}
	for _, key := range []string{"body_class", "message_text", "message_time", "resume_card", "common_card"} {
		fields[key] = cfg.ConversationFields[key]
	}
	started = time.Now()
	rows, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: cfg.Selectors["message.item"], Fields: fields, MaxItems: 8, ExpectedMissing: true})
	if err != nil {
		t.Fatalf("只读消息窗口失败：%v", err)
	}
	// 不输出正文、联系方式或来源URL，只报告数量和真实读取耗时。
	t.Logf("只读消息窗口：%d条，耗时%s", len(rows), time.Since(started))
	started = time.Now()
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatalf("读取后核身未通过：%v", err)
	}
	t.Logf("读取后核身耗时：%s", time.Since(started))
}
