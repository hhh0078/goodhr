// 文件作用：用独立无头浏览器及本机无脚本长表格验证原行稳定编号、显示变化和贴底分页，不接触用户页面或招聘账号。
package hliepin

import (
	"context"
	"errors"
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

// serveViewportTable 返回三十行高卡片及表格外贴底分页；仅靠普通页面导航改变显示字段或原行编号，不注入脚本。
func serveViewportTable(w http.ResponseWriter, req *http.Request) {
	page := 1
	if req.URL.Query().Get("page") == "2" {
		page = 2
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>猎聘本机合成验证</title>
<style>body{margin:0;padding:24px 24px 0;font:16px sans-serif}header{height:160px}.ant-table-body{width:min(760px,calc(100vw - 48px))}table{width:100%;border-collapse:collapse}td{height:450px;border:1px solid #ddd;padding:8px;box-sizing:border-box}.new-resume-personal{display:block;width:max-content;padding:8px}.ant-pagination{display:flex;gap:16px;list-style:none;height:24px;padding:0;margin:0}.ant-pagination li,.ant-pagination form{height:24px;margin:0}.ant-pagination button,.ant-pagination a{display:inline-block;box-sizing:border-box;height:24px;font:16px/20px sans-serif;padding:0 8px}</style>
<header>本机合成候选人名单<textarea id="native-keyboard-target" aria-label="原生快捷键测试区">让快捷键只处理输入框光标</textarea></header><div class="ant-table-body"><table><tbody>`)
	replaced, replaceErr := strconv.Atoi(req.URL.Query().Get("replace_row"))
	for index := 0; index < 30; index++ {
		stableID := fmt.Sprintf("page-%d-row-%02d", page, index)
		if replaceErr == nil && replaced >= 0 && replaced < 30 {
			if index == replaced {
				stableID = "replacement-" + stableID
			} else if index == (replaced+5)%30 {
				stableID = fmt.Sprintf("page-%d-row-%02d", page, replaced)
			}
		}
		name, age := "同名候选人", "30岁"
		if req.URL.Query().Get("display") == "changed" {
			name, age = "显示变化后的候选人", "31岁"
		}
		fmt.Fprintf(w, `<tr id="fixture-row-%02d" data-tlg-elem-id="h_pc_search_res_listcard" data-fixture-id="page-%d-row-%02d"><td><input type="hidden" name="res_id_encode" value="%s"><a class="new-resume-personal" href="/selected?page=%d&amp;id=%s"><span class="new-resume-personal-name">%s</span><div class="personal-detail-age">%s</div><div class="synthetic-J1lRR">合成学校 本科</div><div class="new-resume-personal-expect">合成岗位</div></a><span>立即沟通</span></td></tr>`, index, page, index, stableID, page, stableID, name, age)
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
	if req.URL.Query().Get("offscreen") == "1" {
		fmt.Fprint(w, `</ul><footer style="height:2000px;padding-top:1800px;box-sizing:border-box"><p id="empty-viewport-position">候选人全部在屏幕上方</p></footer></html>`)
	} else {
		fmt.Fprint(w, `</ul></html>`)
	}
}

// TestCandidateViewportIsolatedBrowser 用独立Worker验证原行编号、显示变化、错行拒绝、长列表贴底翻页和空视口备用。
func TestCandidateViewportIsolatedBrowser(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("GOODHR_HLIEPIN_VIEWPORT_TEST_WORKER_URL"))
	if base == "" {
		t.Skip("仅在显式指定独立测试Worker时运行，不连接用户浏览器")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Port() == "39881" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("只能连接非39881端口的本机独立测试Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	browser := client.New(base)
	status, err := browser.BrowserStatus(ctx)
	if err != nil || status.Running {
		t.Fatalf("测试Worker必须没有现存浏览器，未接管任何会话：running=%t err=%v", status.Running, err)
	}
	selections := make(chan string, 8)
	pageVisits := make(chan int, 8)
	// 静态链接只向本机服务器报告一次点击，便于核对姓名变化后仍点击原编号，错行时没有额外请求。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("page") == "2" && req.URL.Query().Get("offscreen") != "1" {
			select {
			case pageVisits <- 2:
			default:
				t.Error("本机第二页被重复打开")
			}
		}
		if req.URL.Path == "/selected" {
			select {
			case selections <- req.URL.Query().Get("id"):
			default:
				t.Error("本机候选人链接被重复点击")
			}
		}
		serveViewportTable(w, req)
	}))
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
	setupConfig := cfg
	setupConfig.Behavior.CandidateIdentityField = ""
	if err := common.ScrollToCandidate(ctx, browser, setupConfig, model.Candidate{Index: 8}); err != nil {
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
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: fmt.Sprintf("%s?display=changed#fixture-row-%02d", server.URL, cursor.StartIndex), WaitUntil: "load"}); err != nil {
		t.Fatal(err)
	}
	changed, err := runtime.FindCandidates(ctx, readOnly, cfg, options)
	if err != nil || len(changed) == 0 || changed[0].Name == before[0].Name || !strings.Contains(changed[0].Summary, "31岁") || changed[0].Fields["platform_candidate_id"] != before[0].Fields["platform_candidate_id"] {
		t.Fatalf("没有真正布置姓名年龄变化、稳定编号不变的页面：candidates=%+v err=%v", changed, err)
	}
	// 旧配置作为反例：同一静态页面只改变显示文字，旧文字定位应失败，新稳定编号定位应继续成功。
	legacyIdentityConfig := cfg
	legacyIdentityConfig.Behavior.CandidateIdentityField = ""
	legacyIdentityConfig.Selectors = make(map[string]contract.SelectorSpec, len(cfg.Selectors))
	for key, value := range cfg.Selectors {
		legacyIdentityConfig.Selectors[key] = value
	}
	legacyCheck := cfg.Selectors["candidate.identity_check"]
	legacyCheck.Target = contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ":scope"}}}
	legacyIdentityConfig.Selectors["candidate.identity_check"] = legacyCheck
	legacyIdentity, err := common.CandidateSelector(legacyIdentityConfig, before[0])
	if err != nil {
		t.Fatal(err)
	}
	legacyIdentity.TimeoutMS = 200
	_, legacyIdentityError := readOnly.Read(ctx, contract.ElementReadRequest{Selector: legacyIdentity, Attribute: "data-fixture-id"})
	var identityFailure *contract.WorkerError
	if !errors.As(legacyIdentityError, &identityFailure) || identityFailure.Body.Code != "ELEMENT_NOT_FOUND" {
		t.Fatal("旧文字定位没有在显示变化的夹具中按预期失败", legacyIdentityError)
	}
	t.Log("静态夹具旧姓名年龄/:scope定位失败；随后用同一行稳定编号验证新定位")
	if err := runtime.ScrollToCandidate(ctx, browser, cfg, before[0]); err != nil {
		t.Fatal("姓名年龄显示变化后，稳定编号未变的原行无法定位", err)
	}
	entry, err := common.CandidateActionSelector(cfg, "candidate.open_target", before[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: entry, Verify: &contract.ClickVerification{URLContains: "/selected", TimeoutMS: 3000}}); err != nil {
		t.Fatal("姓名年龄显示变化后，原行入口无法点击", err)
	}
	select {
	case selected := <-selections:
		if selected != before[0].Fields["platform_candidate_id"] {
			t.Fatalf("姓名变化后点击了其他编号：got=%s want=%s", selected, before[0].Fields["platform_candidate_id"])
		}
	default:
		t.Fatal("没有收到原行入口点击回执")
	}
	if len(selections) != 0 {
		t.Fatal("原行入口被点击多次")
	}
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: fmt.Sprintf("%s?display=changed&replace_row=%d#fixture-row-%02d", server.URL, cursor.StartIndex, cursor.StartIndex), WaitUntil: "load"}); err != nil {
		t.Fatal(err)
	}
	otherRow, err := common.IndexedSelector(cfg, "candidate.item", (cursor.StartIndex+5)%30)
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := readOnly.Read(ctx, contract.ElementReadRequest{Selector: cfg.CandidateFields["platform_candidate_id"].WithinSelector(otherRow), Attribute: "value"})
	if err != nil || otherID.Value != before[0].Fields["platform_candidate_id"] {
		t.Fatal("没有把旧编号移到其他行，无法验证禁止跨行追找", err)
	}
	selector.TimeoutMS, entry.TimeoutMS = 200, 200
	if _, err := readOnly.Read(ctx, contract.ElementReadRequest{Selector: selector, Attribute: "data-fixture-id"}); err == nil {
		t.Fatal("原行编号已变化，仍然接受旧候选人")
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: entry}); err == nil || len(selections) != 0 {
		t.Fatal("原行编号变化后仍点击，或跨行找回了别处的旧编号", err)
	}
	t.Log("姓名年龄显示变化仍按原行稳定编号点击一次；原行编号改变即拒绝，旧编号位于其他行也不追找")
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL, WaitUntil: "load"}); err != nil {
		t.Fatal(err)
	}
	keyboardTarget := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "#native-keyboard-target"}}}, State: "visible", Description: "原生输入框快捷键测试区"}
	pageAnchor := cfg.Selectors["candidate.pagination_scroll"]
	full := true
	if _, err := browser.Scroll(ctx, contract.ScrollRequest{Target: &keyboardTarget, WheelAnchor: &pageAnchor, Distance: 2400, MaxAttempts: 20, WaitMS: 120, RequireFull: &full, ViewportMargin: 8}); err != nil {
		t.Fatal("未能用真实滚轮回到页头输入框", err)
	}
	if _, err := browser.Click(ctx, contract.ElementClickRequest{Selector: keyboardTarget}); err != nil {
		t.Fatal("未能聚焦原生输入框", err)
	}
	t.Log("焦点留在原生输入框，分页仍须通过真实滚轮跨越30行450px长表格")
	// 仅恢复旧版参数验证失败边界，不把本机静态夹具当成用户现场完整复现。
	nextPage := cfg.Selectors["candidate.next_page"]
	_, legacyScrollError := browser.Scroll(ctx, contract.ScrollRequest{Target: &nextPage, WheelAnchor: &pageAnchor, Distance: 320, MaxAttempts: 24, WaitMS: 120, ViewportMargin: 24})
	var scrollFailure *contract.WorkerError
	if !errors.As(legacyScrollError, &scrollFailure) || (scrollFailure.Body.Code != "SCROLL_FAILED" && scrollFailure.Body.Code != "SCROLL_NO_PROGRESS") {
		t.Fatal("旧分页参数没有在长页贴底夹具中按预期失败", legacyScrollError)
	}
	currentPage, found, err := common.ReadOptional(ctx, browser, cfg, "candidate.current_page")
	if err != nil || !found || currentPage != "1" || len(pageVisits) != 0 {
		t.Fatalf("旧参数失败时发生了翻页：current=%s visits=%d err=%v", currentPage, len(pageVisits), err)
	}
	if _, err := browser.Scroll(ctx, contract.ScrollRequest{Target: &keyboardTarget, WheelAnchor: &pageAnchor, Distance: 2400, MaxAttempts: 20, WaitMS: 120, RequireFull: &full, ViewportMargin: 8}); err != nil {
		t.Fatal("旧参数反例后未能用真实滚轮回到原页头位置", err)
	}
	t.Logf("静态长页旧参数以%s失败且未点击；已真实滚轮返回页头，再验证新分页参数", scrollFailure.Body.Code)
	advanced, err := runtime.AdvanceCandidateList(ctx, browser, cfg, before, options)
	if err != nil || !advanced {
		t.Fatalf("表格主体外分页栏未能推进：advanced=%t err=%v", advanced, err)
	}
	if len(pageVisits) != 1 {
		t.Fatalf("新分页没有恰好点击一次：第二页请求数=%d", len(pageVisits))
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
	if len(pageVisits) != 1 {
		t.Fatalf("明确末页后仍然重复翻页：第二页请求数=%d", len(pageVisits))
	}
	t.Log("表格主体外分页成功，第二页从行0读取，明确禁用下一页后正常结束")
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "?page=2&offscreen=1", WaitUntil: "load"}); err != nil {
		t.Fatal(err)
	}
	emptyPosition := contract.SelectorSpec{Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: "#empty-viewport-position"}}}, State: "visible", Description: "空视口测试位置"}
	if _, err := browser.Scroll(ctx, contract.ScrollRequest{Target: &emptyPosition, WheelAnchor: &pageAnchor, Distance: 2400, MaxAttempts: 12, WaitMS: 120}); err != nil {
		t.Fatal(err)
	}
	strict := cfg
	strict.Behavior.CandidateViewportFallbackFirst = false
	if err := common.StartCandidateViewport(ctx, readOnly, strict, &model.CandidateListCursor{}); err == nil {
		t.Fatal("备用场景仍有完整卡片，没有正确布置空视口")
	}
	cursor.StartIndex = 8
	if err := common.StartCandidateViewport(ctx, readOnly, cfg, cursor); err != nil || cursor.StartIndex != 0 || !cursor.IdentityByIndex {
		t.Fatalf("空视口没有使用当前页首行备用起点：cursor=%+v err=%v", cursor, err)
	}
	first, err := runtime.FindCandidates(ctx, readOnly, cfg, options)
	if err != nil || len(first) == 0 || first[0].Fields["platform_candidate_id"] != "page-2-row-00" {
		t.Fatalf("备用起点没有读到当前页第一位：candidates=%+v err=%v", first, err)
	}
	if err := runtime.ScrollToCandidate(ctx, browser, cfg, first[0]); err != nil {
		t.Fatal("备用首位候选人无法通过真实滚轮定位", err)
	}
	t.Log("当前屏幕没有可见候选人时，备用起点为当前页第一位，随后真实滚轮可正常定位")
}
