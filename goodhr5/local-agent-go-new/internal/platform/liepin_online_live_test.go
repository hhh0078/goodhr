// Package platform 本文件提供显式授权的猎聘企业端真页在线资料验收，默认跳过，不发送消息、不下载附件。
package platform

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/platform/liepin"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestLiepinOnlineProfileLive 仅在持有当前浏览器的执行者明确开启并指定目标身份时点击头部、读取DOM并安全恢复。
func TestLiepinOnlineProfileLive(t *testing.T) {
	if os.Getenv("GOODHR_LIEPIN_ONLINE_LIVE") != "1" {
		t.Skip("仅由持有当前猎聘浏览器的执行者显式开启真页验收")
	}
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_LIEPIN_ONLINE_WORKER_URL"))
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("真页验收必须明确指定本机Worker地址，不能带凭据或其它路由")
	}
	snapshot := model.AutoReplyConversationSnapshot{
		CandidateName:         strings.TrimSpace(os.Getenv("GOODHR_LIEPIN_ONLINE_CANDIDATE_NAME")),
		PlatformCandidateID:   strings.TrimSpace(os.Getenv("GOODHR_LIEPIN_ONLINE_CANDIDATE_ID")),
		PlatformThreadID:      strings.TrimSpace(os.Getenv("GOODHR_LIEPIN_ONLINE_THREAD_ID")),
		CommunicationPosition: strings.TrimSpace(os.Getenv("GOODHR_LIEPIN_ONLINE_POSITION")),
	}
	if snapshot.CandidateName == "" || snapshot.PlatformCandidateID == "" || snapshot.PlatformThreadID == "" {
		t.Fatal("必须明确指定候选人姓名、原生编号和会话编号，不能按当前第一位猜测")
	}
	cfg, err := LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	browser := client.New(workerURL)
	runtime := liepin.NewRuntime()
	started := time.Now()
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatalf("开始前目标聊天核身未通过：%v", err)
	}
	profile, err := runtime.ReadAutoReplyOnlineProfile(ctx, browser, cfg, snapshot)
	if err != nil {
		t.Fatalf("在线资料实际读取或恢复未通过：%v", err)
	}
	if profile.Mode != "dom" || len(profile.Screenshots) != 0 || profile.CandidateName != snapshot.CandidateName || strings.TrimSpace(profile.RawText) == "" {
		t.Fatal("在线资料须为DOM、无截图、姓名精确一致且正文非空；不输出正文")
	}
	if err := runtime.VerifyAutoReplyConversationIdentity(ctx, browser, cfg, snapshot); err != nil {
		t.Fatalf("结束后原聊天核身未通过：%v", err)
	}
	missing := []string{}
	for _, field := range []struct {
		name    string
		present bool
	}{
		{"姓名", strings.TrimSpace(profile.CandidateName) != ""},
		{"年龄", profile.Age != nil && *profile.Age > 0 && *profile.Age <= 120},
		{"学历", strings.TrimSpace(profile.EducationLevel) != ""},
		{"最早公司", strings.TrimSpace(profile.EarliestCompanyName) != ""},
		{"最早公司年份", profile.EarliestCompanyYear != nil && *profile.EarliestCompanyYear >= 1900 && *profile.EarliestCompanyYear <= time.Now().Year()},
		{"最早学校", strings.TrimSpace(profile.EarliestSchoolName) != ""},
		{"最早学校年份", profile.EarliestSchoolYear != nil && *profile.EarliestSchoolYear >= 1900 && *profile.EarliestSchoolYear <= time.Now().Year()},
	} {
		if !field.present {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("在线资料必需字段未齐全：%s；未输出字段值或正文", strings.Join(missing, "、"))
	}
	fields := 0
	for _, present := range []bool{profile.CandidateName != "", profile.Gender != "", profile.Age != nil, profile.EducationLevel != "", profile.EarliestCompanyName != "", profile.EarliestCompanyYear != nil, profile.EarliestSchoolName != "", profile.EarliestSchoolYear != nil} {
		if present {
			fields++
		}
	}
	t.Logf("模式=%s，已知字段数=%d，原聊天恢复已核实，总耗时=%s", profile.Mode, fields, time.Since(started))
}
