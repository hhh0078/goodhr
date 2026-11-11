// Package common 文件作用：验证同排卡片高度或横向位置不同时，仍选择当前屏幕内第一张完整卡片，不滚动找旧人选。
package common

import (
	"context"
	"fmt"
	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
	"testing"
)

// viewportGridBrowser 返回一张顶部残边、一张超高卡片及同排完整卡片。
type viewportGridBrowser struct{ model.Browser }

// Find 以标准几何返回合成卡片，读取期间不改变位置。
func (viewportGridBrowser) Find(_ context.Context, req contract.ElementFindRequest) (contract.FindResult, error) {
	boxes := []contract.ElementBox{{X: 10, Y: 60, Width: 500, Height: 180}, {X: 10, Y: 260, Width: 500, Height: 700}, {X: 550, Y: 260, Width: 500, Height: 180}, {X: 10, Y: 960, Width: 500, Height: 180}}
	return contract.FindResult{View: contract.ElementView{Visible: true, Box: boxes[*req.Selector.Target.Index], Viewport: contract.ViewportSize{Width: 1200, Height: 900}}}, nil
}

// FindAll 保留原始序号并提供同名计数所需最小姓名字段。
func (viewportGridBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	var rows []contract.FindAllItem
	for i := req.StartIndex; i < min(4, req.StartIndex+req.MaxItems); i++ {
		rows = append(rows, contract.FindAllItem{Index: i, TotalMatches: 4, Text: "合成人选 30岁", Fields: map[string]string{"name": fmt.Sprintf("合成人选%d", i)}})
	}
	return rows, nil
}

// TestViewportSkipsIncompleteGridCard 验证超高首卡不能挡住同一行后续完整卡片的起跑位置。
func TestViewportSkipsIncompleteGridCard(t *testing.T) {
	cfg := model.Config{Behavior: model.Behavior{CandidateViewportTopInset: 100}, Selectors: map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}}}}, CandidateFields: map[string]contract.SelectorSpec{"name": {}}}
	cursor := &model.CandidateListCursor{}
	if err := StartCandidateViewport(context.Background(), viewportGridBrowser{}, cfg, cursor); err != nil || cursor.StartIndex != 2 {
		t.Fatal("没有选中同排完整卡片", err, cursor.StartIndex)
	}
}
