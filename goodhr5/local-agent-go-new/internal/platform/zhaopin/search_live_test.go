// Package zhaopin_test 文件作用：显式复用已登录智联搜索页与现有Worker，验证真实翻页一次成功及游标清零，不发送或修改筛选。
package zhaopin_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// searchPagingLiveBrowser 仅放行与配置完全一致的下一页点击，禁止输入及键盘操作。
type searchPagingLiveBrowser struct {
	*client.Client
	next   contract.SelectorSpec
	clicks int
}

// Click 只允许单次点击真实下一页，不得借测试调用招呼或其他业务按钮。
func (b *searchPagingLiveBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	if !reflect.DeepEqual(req.Selector, b.next) || b.clicks != 0 {
		return contract.ClickResult{}, fmt.Errorf("翻页验收只允许点击已配置的下一页一次")
	}
	b.clicks++
	return b.Client.Click(ctx, req)
}

// Input 拒绝输入，确保测试保留现有搜索条件及全部聊天草稿。
func (*searchPagingLiveBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("翻页验收禁止输入")
}

// PressKey 拒绝按键，避免回车发送或改变页面条件。
func (*searchPagingLiveBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("翻页验收禁止按键")
}

// TestSearchPagingExistingWorkerLive 核对当前页码后调用正式平台翻页，要求页码加一、新名单出现、断点游标清零且只有一次点击。
func TestSearchPagingExistingWorkerLive(t *testing.T) {
	base := os.Getenv("GOODHR_ZHAOPIN_PAGING_LIVE_WORKER_URL")
	if base == "" {
		t.Skip("需用户准备已登录搜索结果并显式指定现有Worker")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("只能使用明确指定的本机Worker")
	}
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	browser := &searchPagingLiveBrowser{Client: client.New(base), next: cfg.Selectors["candidate.next_page"]}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := ""
	for _, p := range pages.Pages {
		if p.Current {
			current = p.URL
		}
	}
	if current != cfg.ManualSearchURL {
		t.Fatal("当前不在已配置的智联搜索页，不操作")
	}
	// readPage 只读取可见分页栏的HTML，不读取候选人隐藏状态。
	readPage := func() int {
		r, err := browser.Read(ctx, contract.ElementReadRequest{Selector: cfg.Selectors["candidate.pagination"], Property: "html"})
		if err != nil {
			t.Fatal(err)
		}
		m := regexp.MustCompile(`class="[^"]*km-pagination__pager--current[^"]*"[^>]*>\s*([0-9]+)\s*<`).FindStringSubmatch(r.Value)
		if len(m) != 2 {
			t.Fatal("当前可见页码没有确认，不继续")
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	pageBefore := readPage()
	cfg.MaxItems = 3
	runtime, err := platform.RuntimeFor("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	before, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{ReadConcurrency: 5})
	if err != nil || len(before) == 0 {
		t.Fatalf("翻页前名单未读到：%v", err)
	}
	cursor := &model.CandidateListCursor{StartIndex: 17}
	changed, err := runtime.AdvanceCandidateList(ctx, browser, cfg, before, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
	if err != nil || !changed || browser.clicks != 1 || cursor.StartIndex != 0 {
		t.Fatalf("正式翻页未完成：changed=%v clicks=%d cursor=%+v err=%v", changed, browser.clicks, cursor, err)
	}
	pageAfter := readPage()
	if pageAfter != pageBefore+1 {
		t.Fatalf("没有到相邻下一页：%d→%d", pageBefore, pageAfter)
	}
	rows, err := runtime.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
	if err != nil || len(rows) == 0 || rows[0].Index != 0 {
		t.Fatalf("下一页没有从第0行读取：%v", err)
	}
	t.Logf("智联真页翻页通过：%d→%d，下一页点击%d次，名单已变化，游标清零，读取%d人；未发招呼、未输入、未改筛选", pageBefore, pageAfter, browser.clicks, len(rows))
}
