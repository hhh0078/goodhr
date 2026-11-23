// 文件作用：验证起点查询只发一次无字段请求，仅显式开启的空窗口可回退首行，读取错误和未知结果不推进游标。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// viewportProbeBrowser 校验只读协议并返回预设窗口结果，不提供任何输入或滚动能力。
type viewportProbeBrowser struct {
	model.Browser
	t     *testing.T
	items []contract.FindAllItem
	calls int
	err   error
}

// FindAll 确认公共流程一次请求即完成定位，并传递平台配置中的避让范围。
func (b *viewportProbeBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	b.calls++
	if !req.ProbeOnly || req.MaxItems != 1 || len(req.Fields) != 0 || req.ViewportStart == nil || req.ViewportStart.TopInset != 100 || req.ViewportStart.BottomInset != 40 {
		b.t.Fatal("起点定位未使用单次只读协议")
	}
	return b.items, b.err
}

// TestViewportFallbackFirstOnlyWhenConfiguredAndEmpty 验证仅显式开启且只读结果为空时回退首行，读取失败及无效结果均保留原游标和错误。
func TestViewportFallbackFirstOnlyWhenConfiguredAndEmpty(t *testing.T) {
	readErr := errors.New("合成读取失败")
	for _, tc := range []struct {
		name     string
		fallback bool
		items    []contract.FindAllItem
		readErr  error
		wantErr  bool
	}{
		{name: "默认关闭保持错误", wantErr: true},
		{name: "开启后空窗口从首行开始", fallback: true},
		{name: "读取失败不得回退", fallback: true, readErr: readErr, wantErr: true},
		{name: "读取超时不得回退", fallback: true, readErr: context.DeadlineExceeded, wantErr: true},
		{name: "旧组件不得回退", fallback: true, items: []contract.FindAllItem{{Index: 0, TotalMatches: 10}}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := model.Config{
				Behavior:  model.Behavior{CandidateViewportTopInset: 100, CandidateViewportBottomInset: 40, CandidateViewportFallbackFirst: tc.fallback},
				Selectors: map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}}}},
			}
			browser := &viewportProbeBrowser{t: t, items: tc.items, err: tc.readErr}
			cursor := model.CandidateListCursor{StartIndex: 55, IdentityOccurrences: map[string]int{"旧位置": 2}}
			err := StartCandidateViewport(context.Background(), browser, cfg, &cursor)
			if browser.calls != 1 || (err != nil) != tc.wantErr {
				t.Fatalf("起点应只读一次并保持错误规则：calls=%d err=%v", browser.calls, err)
			}
			if tc.readErr != nil && !errors.Is(err, tc.readErr) {
				t.Fatalf("读取错误没有保留：got=%v want=%v", err, tc.readErr)
			}
			if tc.wantErr {
				if cursor.StartIndex != 55 || cursor.IdentityByIndex || cursor.IdentityOccurrences["旧位置"] != 2 {
					t.Fatalf("失败结果错误改变了游标：%+v", cursor)
				}
			} else if cursor.StartIndex != 0 || !cursor.IdentityByIndex || len(cursor.IdentityOccurrences) != 0 {
				t.Fatalf("首行备用起点没有重置旧游标和身份顺序：%+v", cursor)
			}
		})
	}
}

// TestViewportUsesOneRead 验证首张完整卡片由Worker一次返回，保留真实行号而不读取姓名或上方卡片。
func TestViewportUsesOneRead(t *testing.T) {
	cfg := model.Config{Behavior: model.Behavior{CandidateViewportTopInset: 100, CandidateViewportBottomInset: 40}, Selectors: map[string]contract.SelectorSpec{"candidate.item": {Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "css", Value: ".fixture"}}}}}}
	view := contract.ElementView{Visible: true, Box: contract.ElementBox{X: 10, Y: 200, Width: 500, Height: 180}, Viewport: contract.ViewportSize{Width: 1200, Height: 900}}
	for _, tc := range []struct {
		name    string
		rows    []contract.FindAllItem
		success bool
	}{
		{"正确起点", []contract.FindAllItem{{Index: 701, TotalMatches: 869, View: &view}}, true},
		{"空窗口", nil, false}, {"旧组件", []contract.FindAllItem{{Index: 0, TotalMatches: 869}}, false},
		{"无效序号", []contract.FindAllItem{{Index: 869, TotalMatches: 869, View: &view}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &viewportProbeBrowser{t: t, items: tc.rows}
			cursor := model.CandidateListCursor{StartIndex: 55}
			err := StartCandidateViewport(context.Background(), b, cfg, &cursor)
			if b.calls != 1 || (err == nil) != tc.success {
				t.Fatalf("只读起点结果不正确: %v", err)
			}
			if tc.success {
				if cursor.StartIndex != 701 || !cursor.IdentityByIndex {
					t.Fatal("未保留真实起点和行内身份核对")
				}
			} else if cursor.StartIndex != 55 {
				t.Fatal("失败结果错误推进了游标")
			}
		})
	}
}

// TestViewportRejectsNilCursor 验证未知读取位置直接返回错误，不操作浏览器或触发空指针。
func TestViewportRejectsNilCursor(t *testing.T) {
	if err := StartCandidateViewport(context.Background(), nil, model.Config{}, nil); err == nil {
		t.Fatal("空游标被接受")
	}
}
