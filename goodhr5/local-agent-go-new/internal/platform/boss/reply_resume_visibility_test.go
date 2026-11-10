// Package boss 本文件验证附件被固定候选头裁剪时只纠正一次，滚后必须重新核实身份、岗位、删除边界和同一文件来源。
package boss

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// bossResumeVisibilityBrowser 以实测裁剪几何模拟一次真实滚轮；所有鼠标点击和下载均只计数，不连接账号。
type bossResumeVisibilityBrowser struct {
	*bossAttachmentPathBrowser
	target, after, viewport contract.ElementView
	scrollRequests          []contract.ScrollRequest
	onWheel                 func()
	findErr, scrollErr      error
	page                    string
}

// Find 仅返回明确配置的附件按钮或真实裁剪视口，缺少配置或其它目标立即失败。
func (b *bossResumeVisibilityBrowser) Find(_ context.Context, request contract.ElementFindRequest) (contract.FindResult, error) {
	if b.findErr != nil {
		return contract.FindResult{}, b.findErr
	}
	view := b.target
	if request.Selector.Description == "viewport" {
		view = b.viewport
	} else if request.Selector.Description != "pdf-entry" {
		return contract.FindResult{}, errors.New("测试只允许读附件和视口几何")
	}
	return contract.FindResult{View: view}, nil
}

// Scroll 记录唯一滚轮请求后才改变几何或会话，禁止测试隐藏第二次滚动。
func (b *bossResumeVisibilityBrowser) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrollRequests = append(b.scrollRequests, request)
	if len(b.scrollRequests) > 1 {
		return contract.ScrollResult{}, errors.New("不得补滚")
	}
	if b.scrollErr != nil {
		return contract.ScrollResult{}, b.scrollErr
	}
	b.target = b.after
	if b.onWheel != nil {
		b.onWheel()
	}
	return contract.ScrollResult{Scrolled: true, Attempts: 1}, nil
}

// ListPages 在滚后可模拟原标签变化，证明fresh pin不能偷偷恢复原页。
func (b *bossResumeVisibilityBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	page := "https://example.test/chat"
	if b.page != "" {
		page = b.page
	}
	return contract.PageListResult{Pages: []contract.PageInfo{{URL: page, Current: true}}}, nil
}

// WatchDownload 仅让采集入口通过能力检测；错误路径若触发监听即视为测试失败。
func (b *bossResumeVisibilityBrowser) WatchDownload(string, string, ...string) (<-chan contract.DownloadReceipt, func(), error) {
	return nil, nil, errors.New("几何守卫失败后不允许进入下载")
}

// bossResumeVisibilityFixture 使用真实源按钮、固定头下方裁剪视口和滚后安全位置构造确定性环境。
func bossResumeVisibilityFixture(t *testing.T) (*bossResumeVisibilityBrowser, model.Config, model.AutoReplyConversationSnapshot) {
	t.Helper()
	base, cfg, snapshot := bossAttachmentPathFixture(t, false)
	cfg.Selectors["message.history_viewport"] = bossReplySelector("viewport")
	window := contract.ViewportSize{Width: 1100, Height: 760}
	target := contract.ElementView{Box: contract.ElementBox{X: 612, Y: 353, Width: 234, Height: 34}, Viewport: window, Visible: true, Enabled: true, InViewport: true}
	after := target
	after.Box.Y = 480
	viewport := contract.ElementView{Box: contract.ElementBox{X: 521, Y: 380, Width: 458, Height: 288}, Viewport: window, Visible: true, Enabled: true, InViewport: true}
	return &bossResumeVisibilityBrowser{bossAttachmentPathBrowser: base, target: target, after: after, viewport: viewport}, cfg, snapshot
}

// TestBossResumeVisibilityKeepsUsableCenter 验证中心已经在裁剪区域内时零滚动、不额外读正文或改动既有来源定位。
func TestBossResumeVisibilityKeepsUsableCenter(t *testing.T) {
	for _, top := range []float64{366.5, 480} { // 前者为实测半滚后仅中心进入视口，不能继续要求整个按钮可见。
		b, cfg, snapshot := bossResumeVisibilityFixture(t)
		b.target.Box.Y = top
		pinned, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		reads := b.bodyReads
		got, err := prepareBossResumeSourceVisibility(context.Background(), b, pinned, snapshot, cfg.MessagesURL)
		if err != nil || len(b.scrollRequests) != 0 || b.bodyReads != reads || got.Selectors["message.resume_attachment_entry"].Description != "pdf-entry" || b.clicks != 0 {
			t.Fatalf("正常附件错误滚动或重复读正文：scrolls=%d reads=%d err=%v", len(b.scrollRequests), b.bodyReads, err)
		}
	}
}

// TestBossResumeVisibilityRepinsAfterOneWheel 验证真实遮挡样本只请求一次有界滚轮，并使用滚后新总数重建同一来源行。
func TestBossResumeVisibilityRepinsAfterOneWheel(t *testing.T) {
	b, cfg, snapshot := bossResumeVisibilityFixture(t)
	b.after.Box.Y = b.target.Box.Y + 154*0.68/2 // 原生滚动仅产生半幅位移，即使Worker取最小随机距离仍须由中心后置校验判断。
	pinned, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	b.onWheel = func() {
		b.messages = append(b.messages, bossTestMessage("05:02", "item-friend", "新增但未改变附件的消息"))
	}
	got, err := prepareBossResumeSourceVisibility(context.Background(), b, pinned, snapshot, cfg.MessagesURL)
	if err != nil || len(b.scrollRequests) != 1 || b.bodyReads != 2 || b.clicks != 0 {
		t.Fatalf("没有单次滚轮后fresh pin：requests=%d body=%d err=%v", len(b.scrollRequests), b.bodyReads, err)
	}
	req := b.scrollRequests[0]
	if req.Distance != -154 || req.MaxAttempts != 1 || req.WaitMS != 250 || req.RequireFull != nil || req.WheelAnchor == nil || req.WheelAnchor.Description != "viewport" || req.Target != nil || req.ProgressTarget != nil {
		t.Fatal("滚轮未限定真实视口、无目标最小gap裁剪、一次或实测纠正方向")
	}
	entry := got.Selectors["message.resume_attachment_entry"]
	if len(entry.Parents) < 2 || !strings.Contains(entry.Parents[1].Selectors[0].Value, "nth-child(4)") || !strings.Contains(entry.Parents[1].Selectors[0].Value, "nth-child(5)") {
		t.Fatal("滚后继续使用旧总行数，未重新定位来源")
	}
}

// TestBossResumeVisibilityFailsClosed 验证不动、换人、同名错编号、换岗、原页变化、来源变化和删除边界均不补滚或点击。
func TestBossResumeVisibilityFailsClosed(t *testing.T) {
	for _, mode := range []string{"no_move", "other_name", "same_name_other_id", "job", "page", "closed", "source", "deleted", "wheel_error", "find_error", "missing_viewport"} {
		t.Run(mode, func(t *testing.T) {
			b, cfg, snapshot := bossResumeVisibilityFixture(t)
			pinned, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			b.onWheel = func() {
				switch mode {
				case "other_name":
					b.values["name"] = "其他候选人"
				case "same_name_other_id":
					b.values["candidate"] = "get_resume_999999999"
				case "job":
					b.values["position"] = "别的岗位"
				case "page":
					b.page = "https://example.test/elsewhere"
				case "closed":
					delete(b.values, "name")
				case "source":
					b.messages[1] = bossResumePDFMessage("05:00", "不同文件.pdf")
				}
			}
			switch mode {
			case "no_move":
				b.after = b.target
			case "deleted":
				messages, _ := bossMessages(b.messages, time.Now())
				snapshot.HistoryAfter = messages[1].SentAt
			case "wheel_error":
				b.scrollErr = errors.New("滚轮动作未确认")
			case "find_error":
				b.findErr = errors.New("按钮位置未读到")
			case "missing_viewport":
				delete(pinned.Selectors, "message.history_viewport")
			}
			_, err = prepareBossResumeSourceVisibility(context.Background(), b, pinned, snapshot, cfg.MessagesURL)
			if err == nil || len(b.scrollRequests) > 1 || b.clicks != 0 {
				t.Fatalf("不可信来源越过位置守卫：scrolls=%d clicks=%d err=%v", len(b.scrollRequests), b.clicks, err)
			}
		})
	}
}

// TestBossResumeVisibilityBlocksCollectBeforePreview 验证单次纠正未奏效时整个采集入口不会触发预览或下载点击。
func TestBossResumeVisibilityBlocksCollectBeforePreview(t *testing.T) {
	b, cfg, snapshot := bossResumeVisibilityFixture(t)
	b.after = b.target
	pinned, err := pinBossResumeSource(context.Background(), b, cfg, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	_, err = collectBossAttachment(context.Background(), b, pinned, snapshot)
	if err == nil || len(b.scrollRequests) != 1 || b.clicks != 0 {
		t.Fatalf("位置未纠正却打开预览或补滚：scrolls=%d clicks=%d err=%v", len(b.scrollRequests), b.clicks, err)
	}
}

// TestBossResumeVisibilityRepinsLoadedOlderSource 验证滚后可从已加载全窗口定位尾八条之前的文件，但来源消失也绝不补滚历史。
func TestBossResumeVisibilityRepinsLoadedOlderSource(t *testing.T) {
	for _, removed := range []bool{false, true} {
		b, cfg, snapshot := bossAttachmentPathFixture(t, true)
		if removed {
			b.messages[1] = bossResumePDFMessage("04:41", "替换后的别份文件.pdf")
		}
		_, err := pinBossResumeSourceWindow(context.Background(), b, cfg, snapshot, false)
		if (err != nil) != removed || b.scrolls != 0 || b.clicks != 0 {
			t.Fatalf("已加载旧来源定位触发滚轮或误认文件：removed=%v scrolls=%d err=%v", removed, b.scrolls, err)
		}
	}
}

// TestBossResumeClippedGeometryGuards 验证最大距离、窗口裁剪、非法几何、禁用按钮和横向越界均有确定边界。
func TestBossResumeClippedGeometryGuards(t *testing.T) {
	for _, mode := range []string{"above_far", "below_far", "horizontal", "nan", "inf", "hidden", "disabled", "empty", "resized", "viewport_offscreen"} {
		t.Run(mode, func(t *testing.T) {
			b, _, _ := bossResumeVisibilityFixture(t)
			want, wantErr := 0, true
			switch mode {
			case "above_far":
				b.target.Box.Y, want, wantErr = -500, -240, false
			case "below_far":
				b.target.Box.Y, want, wantErr = 1500, 240, false
			case "horizontal":
				b.target.Box.X = 10
			case "nan":
				b.viewport.Box.Y = math.NaN()
			case "inf":
				b.target.Box.Width = math.Inf(1)
			case "hidden":
				b.target.Visible = false
			case "disabled":
				b.target.Enabled = false
			case "empty":
				b.viewport.Box.Height = 0
			case "resized":
				b.viewport.Viewport.Width++
			case "viewport_offscreen":
				b.viewport.Box.Y = 800
			}
			got, err := bossResumeClippedDistance(b.target, b.viewport)
			if (err != nil) != wantErr || got != want {
				t.Fatalf("几何边界错误：distance=%d err=%v", got, err)
			}
		})
	}
}
