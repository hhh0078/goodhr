// Package platform 本文件提供仅定位显式授权候选人会话的真页检查，默认跳过，禁止输入、按键、发送、导航和关闭页面。
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// bossInspectOnlyBrowser 仅允许联系人入口和已限定身份的姓名点击，其余发送类与导航类操作全部拒绝。
type bossInspectOnlyBrowser struct {
	*client.Client
	allowedClicks map[string]bool
}

// Click 仅透传真实配置中的联系人入口及姓名动作，不能借定位检查点击发送按钮。
func (b *bossInspectOnlyBrowser) Click(ctx context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if !b.allowedClicks[request.Selector.Description] {
		return contract.ClickResult{}, fmt.Errorf("定位检查禁止其它点击动作")
	}
	return b.Client.Click(ctx, request)
}

// Input 拒绝任何输入，避免本测试生成或发送候选人正文。
func (b *bossInspectOnlyBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("定位检查禁止输入")
}

// PressKey 拒绝任何按键，避免Enter等快捷键触发发送。
func (b *bossInspectOnlyBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("定位检查禁止按键")
}

// OpenPage 拒绝导航，不替换当前浏览器页面。
func (b *bossInspectOnlyBrowser) OpenPage(context.Context, contract.PageOpenRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, fmt.Errorf("定位检查禁止打开页面")
}

// UsePage 拒绝切换页面，目标页由独占浏览器的执行者预先选定。
func (b *bossInspectOnlyBrowser) UsePage(context.Context, contract.PageUseRequest) (contract.PageInfo, error) {
	return contract.PageInfo{}, fmt.Errorf("定位检查禁止切换页面")
}

// ClosePage 拒绝关闭页面，检查完成后保留现场。
func (b *bossInspectOnlyBrowser) ClosePage(context.Context) error {
	return fmt.Errorf("定位检查禁止关闭页面")
}

// TestBossLiveInspectContract 仅显式启用时对显式授权候选人的原生身份调用真实Inspector，不读取聊天正文或发送消息。
func TestBossLiveInspectContract(t *testing.T) {
	if os.Getenv("GOODHR_BOSS_LIVE_INSPECT") != "1" {
		t.Skip("仅由独占浏览器的执行者启用真页定位检查")
	}
	workerURL := strings.TrimSpace(os.Getenv("GOODHR_BOSS_LIVE_INSPECT_WORKER_URL"))
	parsed, err := url.Parse(workerURL)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		t.Fatal("必须明确指定不含凭据或其它路由的本机Worker地址")
	}
	cfg, err := LoadConfig("boss")
	if err != nil {
		t.Fatal(err)
	}
	browser := &bossInspectOnlyBrowser{Client: client.New(workerURL), allowedClicks: map[string]bool{cfg.Selectors["message.entry"].Description: true, cfg.Selectors["message.contact_click_target"].Description: true}}
	item := model.Conversation{
		Name:                strings.TrimSpace(os.Getenv("GOODHR_BOSS_LIVE_INSPECT_CANDIDATE_NAME")),
		PlatformCandidateID: strings.TrimSpace(os.Getenv("GOODHR_BOSS_LIVE_INSPECT_CANDIDATE_ID")),
		PlatformThreadID:    strings.TrimSpace(os.Getenv("GOODHR_BOSS_LIVE_INSPECT_THREAD_ID")),
	}
	if item.Name == "" || strings.ContainsAny(item.Name, "*＊") || item.PlatformCandidateID == "" || item.PlatformThreadID == "" {
		t.Fatal("必须通过环境变量提供显式授权候选人的完整姓名、原生编号和唯一会话")
	}
	item.Key = item.PlatformThreadID
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	runtime := boss.NewRuntime()
	started := time.Now()
	snapshot, err := runtime.InspectAutoReplyContact(ctx, browser, cfg, item)
	t.Logf("Inspector耗时=%s", time.Since(started))
	if err != nil {
		t.Logf("原始逻辑错误类型=%T；错误=%s", err, bossInspectErrorLine(err.Error()))
		var workerErr *contract.WorkerError
		if errors.As(err, &workerErr) {
			var details struct {
				OriginalError string `json:"original_error"`
				ClickStarted  *bool  `json:"click_started"`
				Clicked       *bool  `json:"clicked"`
			}
			_ = json.Unmarshal(workerErr.Body.Details, &details)
			t.Logf("Worker status=%d code=%s action=%s step=%s trace=%s retryable=%v message=%s original_error=%s click_started=%s clicked=%s", workerErr.Status, workerErr.Body.Code, workerErr.Body.Action, workerErr.Body.Step, workerErr.Body.TraceID, workerErr.Body.Retryable, bossInspectErrorLine(workerErr.Body.Message), bossInspectErrorLine(details.OriginalError), bossInspectFlag(details.ClickStarted), bossInspectFlag(details.Clicked))
		}
		t.Fatal("定位检查未通过，现场保留，未输入或发送消息")
	}
	if snapshot.CandidateName != item.Name || snapshot.PlatformCandidateID != item.PlatformCandidateID || snapshot.PlatformThreadID != item.PlatformThreadID || len(snapshot.Messages) != 0 {
		t.Fatal("Inspector未返回精确目标身份或意外读取历史，禁止继续")
	}
	t.Logf("Inspector成功：姓名=%s 原生候选编号=%s 会话编号=%s；无历史读取，无发送", snapshot.CandidateName, snapshot.PlatformCandidateID, snapshot.PlatformThreadID)
}

// bossInspectErrorLine 仅保留错误首行，避免Playwright后续调用日志夹带页面正文。
func bossInspectErrorLine(value string) string {
	return strings.TrimSpace(strings.SplitN(strings.ReplaceAll(value, "\r\n", "\n"), "\n", 2)[0])
}

// bossInspectFlag 区分未提供的点击标记与实际false，不打印指针地址冒充诊断值。
func bossInspectFlag(value *bool) string {
	if value == nil {
		return "未提供"
	}
	return fmt.Sprint(*value)
}
