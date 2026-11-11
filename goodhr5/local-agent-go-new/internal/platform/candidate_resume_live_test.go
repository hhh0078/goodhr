// Package platform_test 文件作用：在已登录平台当前页面只读验证断点序列化、重建运行时和恢复，不发送消息或修改筛选。
package platform_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestCandidateResumeExistingPageLive 仅在明确指定现有Worker和平台时运行，合成完成标记仅在测试内存中，不写用户数据库。
func TestCandidateResumeExistingPageLive(t *testing.T) {
	id := os.Getenv("GOODHR_RESUME_LIVE_PLATFORM")
	base := os.Getenv("GOODHR_RESUME_LIVE_WORKER_URL")
	if id == "" || base == "" {
		t.Skip("需已登录目标平台，真实任务已停止并明确指定当前Worker后启用")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" {
		t.Fatal("仅允许明确指定本机Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := platform.LoadConfig(id)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxItems = 3
	runtime, err := platform.RuntimeFor(id)
	if err != nil {
		t.Fatal(err)
	}
	resumer, supported := runtime.(model.CandidateListResumer)
	if !supported {
		t.Skip("此平台从当前屏幕开始，不使用历史断点")
	}
	browser := client.New(base)
	cursor := &model.CandidateListCursor{}
	if _, err := resumer.RestoreCandidateList(ctx, browser, cfg, model.CandidateResumePoint{}, cursor); err != nil {
		t.Fatal(err)
	}
	rows, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5, Cursor: cursor})
	if err != nil || len(rows) < 3 {
		t.Fatalf("当前页面三张卡片未读完整：数量%d 错误%v", len(rows), err)
	}
	point, ok := resumer.CandidateResumePoint(rows[1])
	if !ok {
		t.Fatal("没有建立可靠断点")
	}
	serialized, err := json.Marshal(point)
	if err != nil {
		t.Fatal(err)
	}
	var restored model.CandidateResumePoint
	if err := json.Unmarshal(serialized, &restored); err != nil {
		t.Fatal(err)
	}
	fresh, err := platform.RuntimeFor(id)
	if err != nil {
		t.Fatal(err)
	}
	freshResume := fresh.(model.CandidateListResumer)
	nextCursor := &model.CandidateListCursor{}
	found, err := freshResume.RestoreCandidateList(ctx, browser, cfg, restored, nextCursor)
	if err != nil || !found {
		t.Fatalf("真实页面恢复失败：found=%v err=%v", found, err)
	}
	next, err := fresh.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5, Cursor: nextCursor})
	if err != nil || len(next) < 2 {
		t.Fatalf("恢复后读取失败：%v", err)
	}
	if id == "51job" {
		if nextCursor.StartIndex != rows[1].Index+1 || next[0].Fingerprint != rows[2].Fingerprint {
			t.Fatal("前程无忧未从最后完成者的下一人继续")
		}
	} else {
		overlap, ok := freshResume.CandidateResumePoint(next[0])
		if !ok || overlap != point || next[1].Fingerprint != rows[2].Fingerprint {
			t.Fatal("没有恢复到原锚点及下一人")
		}
	}
	t.Logf("%s真实页面只读通过：断点序列化后重建运行时，恢复行号%d；下一人一致；未点击、未发消息、未写用户数据库", id, nextCursor.StartIndex)
	missing := restored
	missing.CandidateID = strings.Repeat("9", 80)
	fallback, err := platform.RuntimeFor(id)
	if err != nil {
		t.Fatal(err)
	}
	fallbackCursor := &model.CandidateListCursor{StartIndex: 999}
	found, err = fallback.(model.CandidateListResumer).RestoreCandidateList(ctx, browser, cfg, missing, fallbackCursor)
	if err != nil || found || fallbackCursor.StartIndex != 0 {
		t.Fatalf("缺失断点未回到第0行：found=%v cursor=%+v err=%v", found, fallbackCursor, err)
	}
	head, err := fallback.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5, Cursor: fallbackCursor})
	if err != nil || len(head) != len(rows) {
		t.Fatalf("缺失断点回退后读取失败：数量%d 错误%v", len(head), err)
	}
	for i := range rows {
		if head[i].Fingerprint != rows[i].Fingerprint {
			t.Fatal("缺失断点回退后的首批名单不一致")
		}
	}
	t.Logf("%s真实页面缺失断点回退通过：从第0行读回原首批%d人", id, len(head))
}
