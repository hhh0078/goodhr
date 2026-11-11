// Package boss 文件作用：验证从屏幕中部启动及重开，不查旧断点、不滚动，且保留同名同龄定位；默认不接触真实账号。
package boss

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// viewportBrowser 以固定纵向卡片模拟真实几何，字段读取与位置查询都保留原始索引。
type viewportBrowser struct {
	model.Browser
	count, start int
	inset        int
	findErr      error
	refreshed    bool
	prefixRead   bool
	fullStarts   []int
	scrolls      int
}

// Find 返回模拟卡片边界，禁止通过测试桩偷偷修改滚动位置。
func (b *viewportBrowser) Find(_ context.Context, req contract.ElementFindRequest) (contract.FindResult, error) {
	if b.findErr != nil {
		return contract.FindResult{}, b.findErr
	}
	i := *req.Selector.Target.Index
	y := float64((i-b.start)*200 + b.inset - 50)
	if b.refreshed && b.prefixRead {
		y += 10
	}
	return contract.FindResult{View: contract.ElementView{Visible: true, Box: contract.ElementBox{X: 10, Y: y, Width: 800, Height: 200}, Viewport: contract.ViewportSize{Width: 1200, Height: 900}}}, nil
}

// FindAll 返回原序号及合成姓名；前方和当前的同名者必须被区分。
func (b *viewportBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if len(req.Fields) > 1 {
		b.fullStarts = append(b.fullStarts, req.StartIndex)
	} else if len(req.Fields) > 0 {
		b.prefixRead = true
	}
	var rows []contract.FindAllItem
	for i := req.StartIndex; i < min(b.count, req.StartIndex+req.MaxItems); i++ {
		name := fmt.Sprintf("合成人选%d", i)
		if i == 1 || i == b.start {
			name = "同名人选"
		}
		rows = append(rows, contract.FindAllItem{Index: i, TotalMatches: b.count, Text: name + " 30岁 本科", Fields: map[string]string{"name": name, "card_text": "普通合成卡片 30岁", "education": "30岁 本科"}})
	}
	return rows, nil
}

// Scroll 任何启动期间的滚动都直接报错，确保改动没有继续寻找旧人选。
func (b *viewportBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	b.scrolls++
	return contract.ScrollResult{}, errors.New("启动不允许滚动")
}

// TestStartsAtCurrentViewport 验证屏幕中间启动后首位真实序号、同名序号与后续批次；新任务跟随新视口，不追旧断点。
func TestStartsAtCurrentViewport(t *testing.T) {
	cfg := recommendationConfig(t)
	cfg.MaxItems = 3
	cfg.CandidateBatchSize = 3
	b := &viewportBrowser{count: 120, start: 45, inset: cfg.Behavior.CandidateTopInset}
	rt := NewRuntime()
	cursor := &model.CandidateListCursor{}
	rows, err := rt.FindCandidates(context.Background(), b, cfg, model.CandidateListOptions{Cursor: cursor})
	if err != nil || len(rows) != 3 || rows[0].Index != 45 || rows[0].IdentityOccurrence != 1 || b.scrolls != 0 {
		t.Fatalf("未从屏幕当前首位开始：%v %+v", err, rows)
	}
	action, err := common.CandidateSelector(cfg, rows[0])
	if err != nil || *action.Target.Index != 1 {
		t.Fatal("当前同名者误指向屏幕上方的人", err)
	}
	cursor.StartIndex = 48
	rows, err = rt.FindCandidates(context.Background(), b, cfg, model.CandidateListOptions{Cursor: cursor})
	if err != nil || rows[0].Index != 48 {
		t.Fatal("后续批次又回到初始屏幕", err)
	}
	b.start = 70
	b.prefixRead = false
	fresh := NewRuntime()
	cursor = &model.CandidateListCursor{}
	rows, err = fresh.FindCandidates(context.Background(), b, cfg, model.CandidateListOptions{Cursor: cursor})
	if err != nil || rows[0].Index != 70 || b.scrolls != 0 {
		t.Fatal("新任务仍追旧位置", err)
	}
	if _, ok := any(fresh).(model.CandidateListResumer); ok {
		t.Fatal("BOSS仍暴露历史断点恢复能力")
	}
	for _, start := range b.fullStarts {
		if start < 45 {
			t.Fatal("读取并处理了屏幕上方的整批候选人")
		}
	}
}

// TestViewportReadFailuresDoNotFallBackToHead 验证未加载、位置读取错误或名单刷新均明确停止，不伪装从第0人开始。
func TestViewportReadFailuresDoNotFallBackToHead(t *testing.T) {
	for _, kind := range []string{"空列表", "查找失败", "刷新"} {
		t.Run(kind, func(t *testing.T) {
			cfg := recommendationConfig(t)
			b := &viewportBrowser{count: 20, start: 10, inset: cfg.Behavior.CandidateTopInset}
			switch kind {
			case "空列表":
				b.count = 0
			case "查找失败":
				b.findErr = errors.New("合成读取中断")
			case "刷新":
				b.refreshed = true
			}
			cursor := &model.CandidateListCursor{StartIndex: 999}
			_, err := NewRuntime().FindCandidates(context.Background(), b, cfg, model.CandidateListOptions{Cursor: cursor})
			if err == nil || cursor.StartIndex != 999 || b.scrolls != 0 || len(b.fullStarts) != 0 {
				t.Fatal("未确认位置却从头或滚动", err)
			}
			if kind == "查找失败" && !strings.Contains(err.Error(), "合成读取中断") {
				t.Fatal("原错误丢失")
			}
		})
	}
}
