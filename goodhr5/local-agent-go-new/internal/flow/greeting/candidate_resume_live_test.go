// Package greeting 文件作用：在已登录前程无忧验证20人分批、真实滚动、SQLite重开与安全回退，禁止真实发送和写入用户任务数据库。
package greeting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// candidateResumeLiveBrowser 只允许本轮真实名单读取与滚轮，记录整批请求的上限及耗时。
type candidateResumeLiveBrowser struct {
	*client.Client
	fullReads   int
	maxItems    int
	maxReadTime time.Duration
	scrolls     int
}

// FindAll 核对所有字段读取均未超过20人，不输出人选姓名和简历内容。
func (b *candidateResumeLiveBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if len(req.Fields) > 0 && req.MaxItems > 20 {
		return nil, fmt.Errorf("本轮只允许最多20人的候选人小批读取")
	}
	began := time.Now()
	rows, err := b.Client.FindAll(ctx, req)
	if len(req.Fields) > 0 {
		b.fullReads++
		b.maxItems = max(b.maxItems, req.MaxItems)
		b.maxReadTime = max(b.maxReadTime, time.Since(began))
	}
	return rows, err
}

// Scroll 透传正式真实滚轮，检查恢复不依赖脚本滚动。
func (b *candidateResumeLiveBrowser) Scroll(ctx context.Context, req contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls++
	return b.Client.Scroll(ctx, req)
}

// Click 拒绝点击，避免名单验证意外联系候选人。
func (*candidateResumeLiveBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, fmt.Errorf("只读断点验收禁止点击")
}

// Input 拒绝输入，保留用户筛选和页面草稿。
func (*candidateResumeLiveBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("只读断点验收禁止输入")
}

// PressKey 拒绝按键，避免回车或快捷键发送。
func (*candidateResumeLiveBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("只读断点验收禁止按键")
}

// TestJob51ResumeSQLiteExistingWorkerLive 使用测试库模拟完成标记，重开库后验证中间位置续读，并在当前条件允许时滚动加载至少100个唯一人选。
func TestJob51ResumeSQLiteExistingWorkerLive(t *testing.T) {
	base := os.Getenv("GOODHR_JOB51_RESUME_LIVE_WORKER_URL")
	if base == "" {
		t.Skip("需用户登录前程无忧且岗位停止后显式指定现有本机Worker")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("只能使用明确指定的本机Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	browser := &candidateResumeLiveBrowser{Client: client.New(base)}
	pages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := ""
	for _, page := range pages.Pages {
		if page.Current {
			current = page.URL
		}
	}
	active, err := url.Parse(current)
	if err != nil || active.Hostname() != "mall.51job.com" {
		t.Fatal("当前不在前程无忧，不操作")
	}
	cfg, err := platform.LoadConfig("51job")
	if err != nil {
		t.Fatal(err)
	}
	probe := cfg.Selectors["candidate.item"]
	probe.TimeoutMS = 1000
	loaded, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: probe, MaxItems: 1, ProbeOnly: true})
	if err != nil || len(loaded) != 1 {
		t.Fatalf("没有读到当前名单：%v", err)
	}
	initialTotal := loaded[0].TotalMatches
	storePath := filepath.Join(t.TempDir(), "live-resume.db")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	// 此账号和路径只用于测试设置键；不会打开真实Profile，也不会调用云端或AI。
	prepared := shared.PreparedTask{Session: cloud.UserSession{UserID: "live-resume-fixture"}, ProfilePath: "live-resume-fixture", Position: cloud.PositionSnapshot{ID: "live-resume-position", Name: "只读断点验收"}, Platform: cfg}
	runtime, err := platform.RuntimeFor("51job")
	if err != nil {
		t.Fatal(err)
	}
	flow := &Flow{Store: store}
	cursor := &model.CandidateListCursor{}
	session, err := flow.prepareCandidateResume(ctx, browser, prepared, runtime, cursor)
	if err != nil {
		t.Fatal(err)
	}
	first, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
	if err != nil || len(first) != 20 {
		t.Fatalf("首批20人未读完整：数量%d 错误%v", len(first), err)
	}
	for _, candidate := range first {
		if err := session.complete(ctx, candidate, nil); err != nil {
			t.Fatal(err)
		}
	}
	last := first[len(first)-1]
	nextSelector, err := common.IndexedSelector(cfg, "candidate.item", last.Index+1)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := browser.Read(ctx, contract.ElementReadRequest{Selector: nextSelector, Attribute: "id"})
	if err != nil || expected.Value == "" {
		t.Fatalf("没确认下一人公开编号：%v", err)
	}
	if err := runtime.ScrollToCandidate(ctx, browser, cfg, last); err != nil {
		t.Fatalf("真实滚动到名单中间失败：%v", err)
	}
	t.Logf("当前名单%d人；已读首批%d人并真实滚到中间；模拟进度只保存在测试库", initialTotal, len(first))
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fresh, err := platform.RuntimeFor("51job")
	if err != nil {
		t.Fatal(err)
	}
	freshFlow := &Flow{Store: reopened}
	freshCursor := &model.CandidateListCursor{}
	recovered, err := freshFlow.prepareCandidateResume(ctx, browser, prepared, fresh, freshCursor)
	if err != nil || recovered.record.Point != session.record.Point || freshCursor.StartIndex != last.Index+1 {
		t.Fatalf("SQLite重开后恢复失败：cursor%+v err%v", freshCursor, err)
	}
	next, err := fresh.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
	if err != nil || len(next) == 0 || next[0].Fields["card_dom_id"] != expected.Value {
		t.Fatalf("续读没有接上正确下一人：数量%d 错误%v", len(next), err)
	}
	t.Logf("进度库关闭重开后从行号%d继续，下一人公开编号一致，续读%d人", freshCursor.StartIndex, len(next))
	// 只在测试设置中放入缺失编号，真实页面不做删除或任何DOM修改。
	missing := recovered.record
	missing.Point.CandidateID = strings.Repeat("9", 80)
	raw, err := json.Marshal(missing)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.SetSetting(ctx, recovered.key, string(raw)); err != nil {
		t.Fatal(err)
	}
	fallback, err := platform.RuntimeFor("51job")
	if err != nil {
		t.Fatal(err)
	}
	fallbackCursor := &model.CandidateListCursor{StartIndex: 999}
	back, err := freshFlow.prepareCandidateResume(ctx, browser, prepared, fallback, fallbackCursor)
	if err != nil || back.record.Point.CandidateID != "" || fallbackCursor.StartIndex != 0 {
		t.Fatalf("断点缺失未从头回退：%+v %v", fallbackCursor, err)
	}
	head, err := fallback.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: fallbackCursor, ReadConcurrency: 5})
	if err != nil || len(head) != 20 {
		t.Fatalf("回退后没有保持20人小批：%d %v", len(head), err)
	}
	for i := range head {
		if head[i].Fingerprint != first[i].Fingerprint {
			t.Fatal("从头回退后名单不一致")
		}
	}
	t.Log("找不到断点时已安全回到第0行，仍只读20人")
	seen := make(map[string]bool)
	for _, rows := range [][]model.Candidate{first, next} {
		for _, candidate := range rows {
			if seen[candidate.Fingerprint] {
				t.Fatal("续读出现重复候选人")
			}
			seen[candidate.Fingerprint] = true
		}
	}
	batch := next
	for round := 0; len(seen) < 100 && round < 6; round++ {
		advanced, err := fresh.AdvanceCandidateList(ctx, browser, cfg, batch, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
		if err != nil {
			t.Fatal(err)
		}
		if !advanced {
			t.Logf("当前筛选已到列表底部，累计核对%d个唯一人选", len(seen))
			break
		}
		batch, err = fresh.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
		if err != nil || len(batch) == 0 {
			t.Fatalf("推进后没有读到新批次：%v", err)
		}
		for _, candidate := range batch {
			if seen[candidate.Fingerprint] {
				t.Fatal("加载下一批重复或倒回开头")
			}
			seen[candidate.Fingerprint] = true
		}
		t.Logf("当前批%d人，累计%d个唯一人选，读取起点%d", len(batch), len(seen), freshCursor.StartIndex)
	}
	if browser.maxItems != 20 || browser.scrolls == 0 {
		t.Fatal("没有覆盖20人读取和真实滚轮")
	}
	t.Logf("验收通过：初始已加载%d，核对唯一人选%d，字段读取%d次，最大单次%d人，最长单次读取%s；未发送、未改筛选、未写用户数据库", initialTotal, len(seen), browser.fullReads, browser.maxItems, browser.maxReadTime)
}
