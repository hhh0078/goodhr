// Package greeting 文件作用：在隔离Worker和500张静态合成卡片上验证扫描250人后模拟超时、SQLite重开、补失败者及接着第251人读取，不访问用户账号。
package greeting

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"goodhr5/local-agent-go-new/internal/storage"
)

// longListTimeoutBrowser 在新批次注入一次只读超时，检验已完成进度不会因新批未返回而丢失。
type longListTimeoutBrowser struct {
	model.Browser
	timedOut bool
}

// FindAll 只模拟第251人开始的字段读取中断，列表探测和其他读取仍走真实浏览器。
func (b *longListTimeoutBrowser) FindAll(ctx context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.StartIndex == 250 && len(req.Fields) > 0 {
		b.timedOut = true
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Message: "合成下一批读取超时"}}
	}
	return b.Browser.FindAll(ctx, req)
}

// TestLongListResumeIsolatedWorker 在显式本机浏览器目录启用，真实读取500张静态卡片中的250人，再核对跨运行补处理及第251人续读。
func TestLongListResumeIsolatedWorker(t *testing.T) {
	browserDir := os.Getenv("GOODHR_JOB51_LONG_LIST_BROWSER_DIR")
	if browserDir == "" {
		t.Skip("需显式指定已安装的Camoufox目录，使用全新隔离Profile")
	}
	project, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	dir := t.TempDir()
	logFile, err := os.Create(filepath.Join(dir, "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	command := exec.Command("node", filepath.Join(project, "worker/dist/main.js"))
	addon := filepath.Join(browserDir, "Camoufox.app/Contents/Resources/addons/UBO")
	command.Env = append(os.Environ(), fmt.Sprintf("GOODHR_WORKER_PORT=%d", port), "CAMOUFOX_INSTALL_DIR="+browserDir, "GOODHR_UBO_EXTENSION_PATH="+addon)
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	native := client.New(fmt.Sprintf("http://127.0.0.1:%d", port))
	for round := 0; ; round++ {
		if err := native.Health(ctx); err == nil {
			break
		}
		if round >= 80 {
			t.Fatal("隔离Worker没有启动，请检查构建依赖")
		}
		time.Sleep(100 * time.Millisecond)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = native.StopBrowser(cleanup)
	}()
	var cards strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&cards, `<li class="card-item" id="RESUME_CARD_%d%d"><div class="info-name">合成人选%d</div><div class="info-item" title="30岁 本科">30岁 本科</div><div class="work-education">合成大学 计算机</div><div class="box-content">仅本机测试的合成经历%d</div></li>`, 1000000+i, i, i, i)
	}
	html := `<!doctype html><meta charset="utf-8"><style>body{margin:0}.card-item{height:180px;padding:20px;border:1px solid #ddd;display:flex;gap:20px}.info-name{width:150px}</style><div class="talent-box"><div class="listBox"><div class="resume-module"><ul class="resume-card">` + cards.String() + `</ul></div></div></div>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		_, _ = fmt.Fprint(w, html)
	}))
	defer server.Close()
	headless := true
	if _, err := native.StartBrowser(ctx, contract.BrowserStartRequest{Headless: &headless, URL: server.URL, UserDataDir: filepath.Join(dir, "profile"), DownloadsPath: filepath.Join(dir, "downloads")}); err != nil {
		t.Fatal(err)
	}
	browser := &candidateResumeLiveBrowser{Client: native}
	cfg, err := platform.LoadConfig("51job")
	if err != nil {
		t.Fatal(err)
	}
	prepared := shared.PreparedTask{Session: cloud.UserSession{UserID: "isolated-owner"}, ProfilePath: "isolated-profile", Position: cloud.PositionSnapshot{ID: "isolated-position"}, Platform: cfg}
	storePath := filepath.Join(dir, "resume.db")
	store, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, _ := platform.RuntimeFor("51job")
	cursor := &model.CandidateListCursor{}
	flow := &Flow{Store: store}
	session, err := flow.prepareCandidateResume(ctx, browser, prepared, runtime, cursor)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < 250; start += 20 {
		cursor.StartIndex = start
		window := cfg
		window.MaxItems = min(20, 250-start)
		rows, err := runtime.FindCandidates(ctx, browser, window, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
		if err != nil || len(rows) != window.MaxItems {
			t.Fatalf("读取%d起批次失败：数量%d %v", start, len(rows), err)
		}
		for _, row := range rows {
			var failure error
			if row.Index == 3 || row.Index == 170 {
				failure = fmt.Errorf("合成详情未完成")
			}
			if err := session.complete(ctx, row, failure); err != nil {
				t.Fatal(err)
			}
		}
	}
	if session.record.Point.CandidateID != "1000249" || len(session.record.Pending) != 2 {
		t.Fatal("早期失败仍冻结了第250人进度")
	}
	cursor.StartIndex = 250
	timeoutBrowser := &longListTimeoutBrowser{Browser: browser}
	if _, err := runtime.FindCandidates(ctx, timeoutBrowser, cfg, model.CandidateListOptions{Cursor: cursor}); err == nil || !timeoutBrowser.timedOut {
		t.Fatal("未注入合成下一批超时")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fresh, _ := platform.RuntimeFor("51job")
	freshCursor := &model.CandidateListCursor{}
	freshFlow := &Flow{Store: reopened}
	recovered, err := freshFlow.prepareCandidateResume(ctx, browser, prepared, fresh, freshCursor)
	if err != nil || freshCursor.StartIndex != 250 {
		t.Fatalf("重开库后没有恢复250行：%+v %v", freshCursor, err)
	}
	for _, want := range []int{3, 170} {
		rows, retrying, err := recovered.retryCandidates(ctx, browser, cfg, fresh, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
		if err != nil || !retrying || len(rows) != 1 || rows[0].Index != want {
			t.Fatalf("未正确补处理%d：%v %v", want, retrying, err)
		}
		if err := recovered.complete(ctx, rows[0], nil); err != nil {
			t.Fatal(err)
		}
	}
	_, retrying, err := recovered.retryCandidates(ctx, browser, cfg, fresh, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
	if err != nil || retrying || freshCursor.StartIndex != 250 || len(recovered.record.Pending) != 0 || recovered.record.Point.CandidateID != "1000249" {
		t.Fatalf("补处理后未回最新位置：%+v %v", freshCursor, err)
	}
	rows, err := fresh.FindCandidates(ctx, browser, cfg, model.CandidateListOptions{Cursor: freshCursor, ReadConcurrency: 5})
	if err != nil || len(rows) != 20 || rows[0].Index != 250 || rows[0].Fields["platform_candidate_id"] != "1000250" {
		t.Fatalf("未从第251人继续：数量%d %v", len(rows), err)
	}
	t.Logf("隔离真实浏览器通过：500张合成卡片读完250人，模拟下一批超时后重开SQLite，先补第4和171人，再从第251人读取20人；最长字段批次%s；未使用用户账号或发送", browser.maxReadTime)
}
