// 文件作用：用独立无头浏览器及本机无脚本表格验证猎聘当前视口起点、同名行身份和表格外分页，不接触用户页面或招聘账号。
package hliepin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// viewportReadOnlyBrowser 在起点确认阶段拒绝任何交互，避免测试把预先滚动或点击当成只读起跑。
type viewportReadOnlyBrowser struct{ model.Browser }

// Scroll 拒绝起点确认阶段的真实滚轮。
func (b viewportReadOnlyBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, fmt.Errorf("当前视口起点确认不得滚动")
}

// Click 拒绝起点确认阶段的点击。
func (b viewportReadOnlyBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, fmt.Errorf("当前视口起点确认不得点击")
}

// Input 拒绝起点确认阶段的输入。
func (b viewportReadOnlyBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("当前视口起点确认不得输入")
}

// PressKey 拒绝起点确认阶段的按键。
func (b viewportReadOnlyBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("当前视口起点确认不得按键")
}

// serveViewportTable 返回只有静态HTML的两页同名表格；分页表单故意位于表格主体外，复现分页与列表滚动区域不同的布局。
func serveViewportTable(w http.ResponseWriter, req *http.Request) {
	page := 1
	if req.URL.Query().Get("page") == "2" {
		page = 2
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>猎聘本机合成验证</title>
<style>body{margin:0;padding:24px;font:16px sans-serif}header{height:160px}.ant-table-body{width:min(760px,calc(100vw - 48px))}table{width:100%;border-collapse:collapse}td{height:158px;border:1px solid #ddd;padding:8px;box-sizing:border-box}.new-resume-personal{padding:8px}.ant-pagination{display:flex;gap:16px;list-style:none;padding:24px 0;margin:0}button,a{font:inherit;padding:8px}footer{height:80px}</style>
<header>本机合成候选人名单</header><div class="ant-table-body"><table><tbody>`)
	for index := 0; index < 16; index++ {
		fmt.Fprintf(w, `<tr data-tlg-elem-id="h_pc_search_res_listcard" data-fixture-id="page-%d-row-%02d"><td><input type="hidden" name="res_id_encode" value="page-%d-row-%02d"><div class="new-resume-personal"><span class="new-resume-personal-name">同名候选人</span><div class="personal-detail-age">30岁</div><div class="synthetic-J1lRR">合成学校 本科</div><div class="new-resume-personal-expect">合成岗位</div></div><span>立即沟通</span></td></tr>`, page, index, page, index)
	}
	fmt.Fprint(w, `</tbody></table></div><ul class="ant-pagination">`)
	for number := 1; number <= 2; number++ {
		class := "ant-pagination-item"
		if number == page {
			class += " ant-pagination-item-active"
		}
		fmt.Fprintf(w, `<li class="%s"><a href="?page=%d">%d</a></li>`, class, number, number)
	}
	if page == 1 {
		fmt.Fprint(w, `<li class="ant-pagination-next"><form method="get"><button name="page" value="2">下一页</button></form></li>`)
	} else {
		fmt.Fprint(w, `<li class="ant-pagination-next ant-pagination-disabled" aria-disabled="true"><button disabled>下一页</button></li>`)
	}
	fmt.Fprint(w, `</ul><footer>本机合成名单结束</footer></html>`)
}

// TestCandidateViewportIsolatedBrowser 显式连接独立Worker，在全新Profile验证中部只读起跑、同名行核对、翻页归零和明确末页。
func TestCandidateViewportIsolatedBrowser(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("GOODHR_HLIEPIN_VIEWPORT_TEST_WORKER_URL"))
	if base == "" {
		t.Skip("仅在显式指定独立测试Worker时运行，不连接用户浏览器")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Port() == "39881" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("只能连接非39881端口的本机独立测试Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	browser := client.New(base)
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.Running {
		t.Fatalf("测试Worker必须没有现存浏览器，未接管任何会话：running=%t err=%v", status.Running, err)
	}
	server := httptest.NewServer(http.HandlerFunc(serveViewportTable))
	defer server.Close()
	profileDir, downloadsDir := t.TempDir(), t.TempDir()
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if _, err := browser.StopBrowser(cleanupCtx); err != nil {
			t.Errorf("独立测试浏览器清理失败：%v", err)
		}
	})
	headless := true
	if _, err := browser.StartBrowser(ctx, contract.BrowserStartRequest{
		Headless: &headless, UserDataDir: profileDir, DownloadsPath: downloadsDir,
		URL: server.URL, WaitUntil: "domcontentloaded", Locale: "zh-CN", Timezone: "Asia/Shanghai",
	}); err != nil {
		t.Fatal("启动独立无头浏览器失败", err)
	}
	cfg := pagingConfig(t)
	cfg.MaxItems = 4
	runtime := NewRuntime()
	if !common.CandidatePageUsesViewport(cfg, cfg.EntryURL) {
		t.Fatal("实际猎聘配置没有启用找人页当前视口起点")
	}
	if err := common.ScrollToCandidate(ctx, browser, cfg, model.Candidate{Index: 8}); err != nil {
		t.Fatal("通过真实滚轮布置表格中部失败", err)
	}
	readOnly := viewportReadOnlyBrowser{Browser: browser}
	cursor := &model.CandidateListCursor{}
	if err := common.StartCandidateViewport(ctx, readOnly, cfg, cursor); err != nil {
		t.Fatal("只读定位当前首张完整行失败", err)
	}
	if cursor.StartIndex <= 0 {
		t.Fatal("中部起跑错误地回到了第一行")
	}
	beforeView, err := common.CandidateViewport(ctx, readOnly, cfg, cursor.StartIndex)
	if err != nil || !common.CandidateFullyVisible(beforeView, cfg) {
		t.Fatal("起点行没有完整显示", err)
	}
	options := model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5}
	before, err := runtime.FindCandidates(ctx, readOnly, cfg, options)
	if err != nil || len(before) == 0 || before[0].Index != cursor.StartIndex || !before[0].IdentityByIndex {
		t.Fatalf("当前行读取失败：cursor=%+v candidates=%+v err=%v", cursor, before, err)
	}
	selector, err := common.CandidateSelector(cfg, before[0])
	if err != nil {
		t.Fatal("生成当前同名行身份核对选择器失败", err)
	}
	identity, err := readOnly.Read(ctx, contract.ElementReadRequest{Selector: selector, Attribute: "data-fixture-id"})
	wantID := "page-1-row-" + fmt.Sprintf("%02d", cursor.StartIndex)
	if err != nil || identity.Value != wantID {
		t.Fatalf("同名者身份核对选错行：got=%s want=%s err=%v", identity.Value, wantID, err)
	}
	afterView, err := common.CandidateViewport(ctx, readOnly, cfg, cursor.StartIndex)
	if err != nil || afterView.Box != beforeView.Box {
		t.Fatalf("只读起跑改变了页面位置：before=%+v after=%+v err=%v", beforeView.Box, afterView.Box, err)
	}
	t.Logf("真实滚轮布置后，从原始第%d行只读起跑；同名行身份=%s，位置保持不变", cursor.StartIndex+1, identity.Value)
	advanced, err := runtime.AdvanceCandidateList(ctx, browser, cfg, before, options)
	if err != nil || !advanced {
		t.Fatalf("表格主体外分页栏未能推进：advanced=%t err=%v", advanced, err)
	}
	after, err := runtime.FindCandidates(ctx, browser, cfg, options)
	if err != nil || len(after) == 0 || cursor.StartIndex != 0 || after[0].Index != 0 || after[0].Fields["platform_candidate_id"] != "page-2-row-00" {
		t.Fatalf("新页未从首行处理：cursor=%+v candidates=%+v err=%v", cursor, after, err)
	}
	current, _, err := common.ReadOptional(ctx, browser, cfg, "candidate.current_page")
	if err != nil || current != strconv.Itoa(2) {
		t.Fatalf("实际页码没有切到2：page=%s err=%v", current, err)
	}
	advanced, err = runtime.AdvanceCandidateList(ctx, browser, cfg, after, options)
	if err != nil || advanced {
		t.Fatalf("明确末页没有正常结束：advanced=%t err=%v", advanced, err)
	}
	t.Log("表格主体外分页成功，第二页从行0读取，明确禁用下一页后正常结束")
}
