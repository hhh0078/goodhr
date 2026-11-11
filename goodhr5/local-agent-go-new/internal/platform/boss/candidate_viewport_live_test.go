// Package boss 文件作用：显式连接已登录BOSS推荐页验证当前视口启动和重开，拒绝一切滚轮、点击、输入与发送，不写用户进度。
package boss

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/client"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// viewportLiveBrowser 拦住所有交互，记录实际原生几何，证明启动没有滚动找旧断点。
type viewportLiveBrowser struct {
	*client.Client
	initial map[int]contract.ElementBox
}

// Find 保留第一次真实元素边界，供读取完成后复核当前屏幕位置没有改变。
func (b *viewportLiveBrowser) Find(ctx context.Context, req contract.ElementFindRequest) (contract.FindResult, error) {
	result, err := b.Client.Find(ctx, req)
	if err == nil && req.Selector.Target.Index != nil {
		i := *req.Selector.Target.Index
		if _, ok := b.initial[i]; !ok {
			b.initial[i] = result.View.Box
		}
	}
	return result, err
}

// Scroll 拒绝滚动，启动期间如仍追历史断点便直接失败。
func (*viewportLiveBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{}, fmt.Errorf("当前视口启动验收不允许滚轮")
}

// Click 拒绝点击，防止只读测试联系候选人。
func (*viewportLiveBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	return contract.ClickResult{}, fmt.Errorf("当前视口启动验收不允许点击")
}

// Input 拒绝输入，保留用户岗位与筛选。
func (*viewportLiveBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	return contract.InputResult{}, fmt.Errorf("当前视口启动验收不允许输入")
}

// PressKey 拒绝按键，防止意外发送。
func (*viewportLiveBrowser) PressKey(context.Context, contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	return contract.KeyboardPressResult{}, fmt.Errorf("当前视口启动验收不允许按键")
}

// TestCurrentViewportExistingWorkerLive 只在显式启用且任务已停止时运行，验证原生真页第一次及重建任务均从屏幕当前位置读取。
func TestCurrentViewportExistingWorkerLive(t *testing.T) {
	base := os.Getenv("GOODHR_BOSS_VIEWPORT_LIVE_WORKER_URL")
	if base == "" {
		t.Skip("需用户已登录BOSS且任务停止，显式指定现有本机Worker")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("必须指定本机Worker")
	}
	cfg := recommendationConfig(t)
	cfg.MaxItems = 3
	cfg.CandidateBatchSize = 3
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := &viewportLiveBrowser{Client: client.New(base), initial: map[int]contract.ElementBox{}}
	// 显式准备一个中部位置；此步只用于测试布置，真正启动阶段的包装器仍拒绝任何滚轮。
	if raw := os.Getenv("GOODHR_BOSS_VIEWPORT_LIVE_PREPARE_INDEX"); raw != "" {
		index, err := strconv.Atoi(raw)
		if err != nil || index < 1 || index > 50 {
			t.Fatal("中部测试序号仅允许1至50")
		}
		if err := common.ScrollToCandidate(ctx, b.Client, cfg, model.Candidate{Index: index}); err != nil {
			t.Fatal("准备中部测试位置失败", err)
		}
	}
	current, err := common.UniqueCurrentPageURL(ctx, b)
	if err != nil || !common.PageURLMatches(current, cfg.EntryURL) {
		t.Fatal("当前不在唯一BOSS推荐页，不操作", err)
	}
	var first model.Candidate
	for round := 0; round < 2; round++ {
		cursor := &model.CandidateListCursor{}
		rows, err := NewRuntime().FindCandidates(ctx, b, cfg, model.CandidateListOptions{Cursor: cursor, ReadConcurrency: 5})
		if err != nil || len(rows) == 0 || rows[0].Index != cursor.StartIndex {
			t.Fatal("没有从当前视口启动", err)
		}
		if round == 0 {
			first = rows[0]
		} else if rows[0].Index != first.Index || rows[0].Fingerprint != first.Fingerprint {
			t.Fatal("重开任务后视口起点不同")
		}
		after, err := candidateViewport(ctx, b, cfg, rows[0].Index)
		if err != nil || after.Box != b.initial[rows[0].Index] || !candidateIntersectsViewport(after, cfg) {
			t.Fatal("启动时改变了当前屏幕位置", err)
		}
		t.Logf("真实BOSS第%d次启动：从当前屏幕第%d行读取%d人，卡片边界未动；不回补、不查旧断点、无滚轮或发送", round+1, cursor.StartIndex, len(rows))
	}
}
