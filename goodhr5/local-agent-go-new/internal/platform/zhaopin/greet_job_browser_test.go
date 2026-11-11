// Package zhaopin_test 文件作用：借用现有Worker在独立本机标签重现旧选岗加载竞态，随后验证新流程；不对招聘页注入脚本。
package zhaopin_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/platform/zhaopin"
)

// TestGreetingJobLoadingExistingWorker 在隔离的本机测试页复现真实页面观察到的延迟骨架和选项替换，找到岗位后出现加载空窗，新流程只提交一次。
func TestGreetingJobLoadingExistingWorker(t *testing.T) {
	workerURL := os.Getenv("GOODHR_ZHAOPIN_LOADING_TEST_WORKER_URL")
	if workerURL == "" {
		t.Skip("只在用户招聘任务已停止且明确指定本机现有Worker时启用")
	}
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("必须指定不含凭据的本机Worker地址")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	browser := client.New(workerURL)
	pages, err := browser.ListPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := ""
	for _, p := range pages.Pages {
		if p.Current {
			original = p.PageID
		}
	}
	if original == "" {
		t.Fatal("无法保存原标签页")
	}
	var confirmed, options atomic.Int32
	// JavaScript是本机测试页面自身的实现；自动化只调用Worker标准Locator、鼠标和键盘。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/confirmed" {
			confirmed.Add(1)
			return
		}
		if r.URL.Path == "/option" {
			options.Add(1)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><style>body{font:18px sans-serif;margin:40px}.km-modal--open{position:fixed;left:150px;top:80px;background:white;width:600px;height:400px;border:1px solid #888;padding:24px}.jsn-job-selector-popper{position:fixed;left:190px;top:240px;width:550px;background:white;border:1px solid #aaa;padding:12px}input{width:480px;height:32px}.jsn-job-selector__option{display:block;padding:12px}.buttons{position:absolute;bottom:20px}button{padding:8px}</style><div class="search-resume-list"><div class="search-resume-item__inner"><span class="talent-basic-info__name--inner">合成候选人</span><span>26岁</span><button class="large-screen-btn" id="greet" onclick="dialog.className='km-modal--open';dialog.style.display='block'">打招呼</button></div></div><div id="dialog" style="display:none"><div class="resume-buttons-chat__title">选择沟通职位</div><input class="jsn-job-selector__input" placeholder="请选择沟通职位" readonly onclick="popper.style.display='block';render()"><div class="jsn-job-selector__label"><span class="jsn-job-selector__option--title" id="selected"></span></div><div class="buttons"><button onclick="dialog.style.display='none'">取消</button><button onclick="fetch('/confirmed');dialog.style.display='none';greet.textContent='继续沟通'">确定</button></div></div><div class="jsn-job-selector-popper" id="popper" style="display:none"><input placeholder="职位名称 / 发布地" oninput="reload()"><div id="results"></div></div><script>const name=%q;let debounce;function render(){results.innerHTML='<a class="jsn-job-selector__option" onclick="choose()"><span class="jsn-job-selector__option--title" title="'+name+'">'+name+'</span></a>'}function choose(){fetch('/option');selected.title=name;selected.textContent=name;popper.style.display='none'}function reload(){clearTimeout(debounce);debounce=setTimeout(()=>{results.innerHTML='<div class="jsn-job-selector-loading">加载中</div>';setTimeout(render,900)},350)}document.addEventListener('keydown',e=>{if(e.key==='Escape')popper.style.display='none'})</script>`, greetingJobName)
	}))
	defer server.Close()
	newTab := true
	fixture, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/search", NewTab: &newTab, WaitUntil: "domcontentloaded"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := browser.UsePage(clean, contract.PageUseRequest{PageID: fixture.PageID}); err == nil {
			_ = browser.ClosePage(clean)
		}
		_, _ = browser.UsePage(clean, contract.PageUseRequest{PageID: original})
	}()
	cfg, err := platform.LoadConfig("zhaopin")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ManualSearchURL = server.URL + "/search"
	cfg.EntryURL = server.URL + "/recommend"
	candidate := model.Candidate{Index: 0, Name: "合成候选人", IdentityTexts: []string{"合成候选人", "26岁"}}
	// 精确重放修复前的选岗顺序：输入后仅做一次唯一性探测就点击，不等异步加载。
	if err := common.CandidateAction(ctx, browser, cfg, candidate, "candidate.greet"); err != nil {
		t.Fatal(err)
	}
	if err := common.ClickRequired(ctx, browser, cfg, "candidate.greet_job_open"); err != nil {
		t.Fatal(err)
	}
	if err := common.InputRequired(ctx, browser, cfg, "candidate.greet_job_input", common.PositionSearchQuery(greetingJobName)); err != nil {
		t.Fatal(err)
	}
	old := cfg.Selectors["candidate.greet_job_name"]
	exact := true
	old.Target.Text = greetingJobName
	old.Target.ExactText = &exact
	found, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: old, MaxItems: 2, ExpectedMissing: true, ProbeOnly: true})
	if err != nil || len(found) != 1 {
		t.Fatalf("旧流程基线未找到唯一岗位：%v %d", err, len(found))
	}
	// 观察旧流程唯一性检查与点击之间的竞态窗口；不能要求每台Mac都随机撞中Windows的同一原生时序。
	sawLoading := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		skeleton := cfg.Selectors["candidate.greet_job_loading"]
		skeleton.TimeoutMS = 1
		loading, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: skeleton, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
		if err != nil && !common.IsElementMissing(err) {
			t.Fatal(err)
		}
		if len(loading) > 0 {
			old.TimeoutMS = 1
			missing, err := browser.FindAll(ctx, contract.ElementFindAllRequest{Selector: old, MaxItems: 1, ExpectedMissing: true, ProbeOnly: true})
			if err != nil && !common.IsElementMissing(err) {
				t.Fatal(err)
			}
			if len(missing) == 0 {
				sawLoading = true
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !sawLoading || options.Load() != 0 || confirmed.Load() != 0 {
		t.Fatalf("未观察到查找后加载空窗：loading=%v options=%d confirmed=%d", sawLoading, options.Load(), confirmed.Load())
	}
	t.Log("加载空窗已复现：唯一岗位已找到，随后骨架出现、旧目标消失；本机页没有选岗或提交")
	// 重载仅本机测试页，真实招聘标签、筛选和登录资料原样保留。
	if _, err := browser.OpenPage(ctx, contract.PageOpenRequest{URL: server.URL + "/search-fixed", WaitUntil: "domcontentloaded"}); err != nil {
		t.Fatal(err)
	}
	cfg.ManualSearchURL = server.URL + "/search-fixed"
	runtime := zhaopin.NewRuntime()
	if err := runtime.SelectPosition(ctx, browser, cfg, model.Position{Name: greetingJobName}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.GreetCandidate(ctx, browser, cfg, candidate, model.GreetRequest{}); err != nil {
		t.Fatal(err)
	}
	if options.Load() != 1 || confirmed.Load() != 1 {
		t.Fatalf("新流程发生重复：选项%d，提交%d", options.Load(), confirmed.Load())
	}
	t.Log("新流程通过：等待加载及稳定，唯一完整岗位选中、回显核对、提交均一次，并确认继续沟通")
}
