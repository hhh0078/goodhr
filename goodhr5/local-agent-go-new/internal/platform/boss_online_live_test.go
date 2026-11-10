// Package platform 本文件提供仅限显式授权候选人会话的BOSS在线资料真页验收，默认跳过，只在内存检查截图数量与大小。
package platform

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/platform/boss"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestBossOnlineProfileLive 由当前浏览器执行者显式开启，只核实显式授权候选人身份、读取在线截图并复核原聊天，不调用AI或保存截图。
func TestBossOnlineProfileLive(t *testing.T) {
	if os.Getenv("GOODHR_BOSS_ONLINE_LIVE") != "1" {
		t.Skip("仅由持有当前BOSS浏览器的执行者显式开启授权候选人在线资料验收")
	}
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_BOSS_ONLINE_WORKER_URL"))
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("真页验收必须明确指定本机Worker地址，不能带凭据或其它路由")
	}
	snapshot := model.AutoReplyConversationSnapshot{
		CandidateName:         strings.TrimSpace(os.Getenv("GOODHR_BOSS_ONLINE_CANDIDATE_NAME")),
		PlatformCandidateID:   strings.TrimSpace(os.Getenv("GOODHR_BOSS_ONLINE_CANDIDATE_ID")),
		PlatformThreadID:      strings.TrimSpace(os.Getenv("GOODHR_BOSS_ONLINE_THREAD_ID")),
		CommunicationPosition: strings.TrimSpace(os.Getenv("GOODHR_BOSS_ONLINE_POSITION")),
	}
	if snapshot.CandidateName == "" || strings.ContainsAny(snapshot.CandidateName, "*＊") || snapshot.PlatformCandidateID == "" || snapshot.PlatformThreadID == "" {
		t.Fatal("必须通过环境变量提供显式授权候选人的完整姓名、原生编号和唯一会话，不使用当前第一位候选人")
	}
	cfg, err := LoadConfig("boss")
	if err != nil {
		t.Fatal("BOSS平台内置配置尚未准备好")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	browser := client.New(workerURL)
	runtime := boss.NewRuntime()
	started := time.Now()
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatal("开始前显式授权候选人聊天核身未通过，未读取资料")
	}
	profile, err := runtime.ReadAutoReplyOnlineProfile(ctx, browser, cfg, snapshot)
	if err != nil {
		t.Fatal("在线资料实际读取或恢复未通过，未输出正文或图片")
	}
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatal("结束后原聊天核身未通过，未输出正文或图片")
	}
	if profile.Mode != "ai" || profile.CandidateName != snapshot.CandidateName || profile.RawText != "" || !strings.HasPrefix(profile.SourceKey, "online-image:") || len(profile.Screenshots) == 0 {
		t.Fatal("BOSS资料必须为当前候选人的AI截图模式，不得冒用DOM正文或附件")
	}
	total := 0
	for _, picture := range profile.Screenshots {
		if len(picture) == 0 {
			t.Fatal("在线截图有空图片，未输出或保存图片")
		}
		total += len(picture)
	}
	t.Logf("图片数=%d，总字节=%d，总耗时=%s", len(profile.Screenshots), total, time.Since(started))
}
