// Package platform_test 文件作用：验证三平台大名单启动不读取上方姓名，行内身份定位不会依赖旧同名序号。
package platform_test

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/common"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// prefixForbiddenBrowser 模拟5000张卡片、视口位于1250处，任何文字回读都明确失败。
type prefixForbiddenBrowser struct {
	model.Browser
	top, geometryCalls int
}

// Find 只提供标准几何边界，卡片1250仅露边缘，1251完整显示。
func (b *prefixForbiddenBrowser) Find(_ context.Context, r contract.ElementFindRequest) (contract.FindResult, error) {
	b.geometryCalls++
	i := *r.Selector.Target.Index
	return contract.FindResult{View: contract.ElementView{Visible: true, Box: contract.ElementBox{X: 10, Y: float64((i-1250)*200 + b.top - 50), Width: 800, Height: 180}, Viewport: contract.ViewportSize{Width: 1200, Height: 900}}}, nil
}

// FindAll 仅允许不读取任何字段的列表数量探测，复现原先回读上方姓名会出错的情况。
func (b *prefixForbiddenBrowser) FindAll(_ context.Context, r contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if !r.ProbeOnly || len(r.Fields) > 0 {
		return nil, errors.New("禁止读取上方候选人文字")
	}
	return []contract.FindAllItem{{Index: 0, TotalMatches: 5000}}, nil
}

// TestViewportStartNeverReadsOldNames 验证BOSS、智联和无忧从大名单中部启动只查询少量几何，且动作按真实行号再核对身份。
func TestViewportStartNeverReadsOldNames(t *testing.T) {
	for _, id := range []string{"boss", "zhaopin", "51job"} {
		t.Run(id, func(t *testing.T) {
			cfg, err := platform.LoadConfig(id)
			if err != nil {
				t.Fatal(err)
			}
			b := &prefixForbiddenBrowser{top: cfg.Behavior.CandidateViewportTopInset}
			cursor := &model.CandidateListCursor{}
			if err = common.StartCandidateViewport(context.Background(), b, cfg, cursor); err != nil {
				t.Fatal(err)
			}
			if cursor.StartIndex != 1251 || !cursor.IdentityByIndex || len(cursor.IdentityOccurrences) != 0 || b.geometryCalls > 18 {
				t.Fatalf("启动仍依赖名字统计或位置有误：%+v calls=%d", cursor, b.geometryCalls)
			}
			c := model.Candidate{Index: 1251, IdentityByIndex: true, IdentityTexts: []string{"合成人选", "30岁"}, IdentityOccurrence: 999}
			spec, err := common.CandidateSelector(cfg, c)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range spec.Variants() {
				if len(v.Parents) == 0 || v.Parents[len(v.Parents)-1].Index == nil || *v.Parents[len(v.Parents)-1].Index != 1251 || v.Target.Index != nil || len(v.Target.Texts) != 2 {
					t.Fatal("按真实行号核对身份的约束丢失")
				}
			}
		})
	}
}
