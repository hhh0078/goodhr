// Package job51 文件作用：验证卡片索引尾缀剥离、换序后的同人去重，以及同名同龄人选的精确属性定位。
package job51

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type identityScopeStub struct {
	model.Browser
	source    string
	scrolls   []contract.ScrollRequest
	positions []float64
	width     float64
	views     []contract.ElementView
}

// Read 返回合成列表HTML，包含公开卡片编号，不执行页面脚本。
func (s *identityScopeStub) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{Value: s.source}, nil
}

// Scroll 记录最终真实滚轮目标，核对它使用精确编号而不是旧的同名序号。
func (s *identityScopeStub) Scroll(_ context.Context, request contract.ScrollRequest) (contract.ScrollResult, error) {
	s.scrolls = append(s.scrolls, request)
	if request.Target != nil && len(s.views) > 0 {
		view := s.views[0]
		s.views = s.views[1:]
		return contract.ScrollResult{After: &view}, nil
	}
	y := 300.0
	if request.Target != nil && len(s.positions) > 0 {
		y = s.positions[0]
		s.positions = s.positions[1:]
	}
	return contract.ScrollResult{After: &contract.ElementView{Box: contract.ElementBox{X: 20, Y: y, Width: 700, Height: 200}, Viewport: contract.ViewportSize{Width: s.width, Height: 1040}, Visible: true}}, nil
}

// TestActionScrollFullyExposesButton 验证215.3小数卡片顶边可接受，窄窗口继续对右侧实际按钮做完整可见滚动。
func TestActionScrollFullyExposesButton(t *testing.T) {
	for _, width := range []float64{900, 1600} {
		x := 811.0
		if width == 1600 {
			x = 997
		}
		viewport := contract.ViewportSize{Width: width, Height: 816}
		browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, views: []contract.ElementView{
			{Box: contract.ElementBox{X: 88, Y: 215.3, Width: 1032, Height: 158}, Viewport: viewport, Visible: true},
			{Box: contract.ElementBox{X: x, Y: 250.7, Width: 80, Height: 32}, Viewport: viewport, Visible: true, FullyInViewport: true},
		}}
		if err := NewRuntime().scrollCandidateAction(context.Background(), browser, loadPositionConfig(t), greetCandidateFixture(), "candidate.greet"); err != nil {
			t.Fatal(err)
		}
		if len(browser.scrolls) != 2 || browser.scrolls[1].RequireFull == nil || !*browser.scrolls[1].RequireFull || browser.scrolls[1].ViewportMargin != 48 {
			t.Fatal("宽卡片相交后仍必须完整露出实际操作按钮")
		}
	}
}

// TestActionStillOutsideViewportStops 验证按钮仅横向部分露出时拒绝继续点击，不能把可探测当作可操作。
func TestActionStillOutsideViewportStops(t *testing.T) {
	for _, box := range []contract.ElementBox{{X: 997, Y: 250, Width: 80, Height: 32}, {X: -10, Y: 250, Width: 80, Height: 32}} {
		viewport := contract.ViewportSize{Width: 900, Height: 816}
		browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, views: []contract.ElementView{
			{Box: contract.ElementBox{X: 88, Y: 215.3, Width: 1032, Height: 158}, Viewport: viewport, Visible: true},
			{Box: box, Viewport: viewport, Visible: true},
		}}
		if err := NewRuntime().scrollCandidateAction(context.Background(), browser, loadPositionConfig(t), greetCandidateFixture(), "candidate.greet"); err == nil {
			t.Fatal("按钮没完整露出时必须停止")
		}
	}
}

// TestActionAvoidsTopAndBottomOverlays 验证两种窗口宽度均用真实竖向滚轮避让上下浮层，每次复核保持第10张的精确编号。
func TestActionAvoidsTopAndBottomOverlays(t *testing.T) {
	var source strings.Builder
	for index := 0; index <= 10; index++ {
		fmt.Fprintf(&source, `<li id="RESUME_CARD_%d%d">合成人选 28岁</li>`, 200000+index, index)
	}
	for _, width := range []float64{900, 1600} {
		for _, initialY := range []float64{38, 720} {
			viewport := contract.ViewportSize{Width: width, Height: 816}
			browser := &identityScopeStub{source: source.String(), views: []contract.ElementView{
				{Box: contract.ElementBox{X: 88, Y: 215.3, Width: 1032, Height: 158}, Viewport: viewport, Visible: true},
				{Box: contract.ElementBox{X: width - 90, Y: initialY, Width: 80, Height: 32}, Viewport: viewport, Visible: true},
				{Box: contract.ElementBox{X: width - 90, Y: 450, Width: 80, Height: 32}, Viewport: viewport, Visible: true},
			}}
			candidate := model.Candidate{Fields: map[string]string{"platform_candidate_id": "200010"}}
			if err := NewRuntime().scrollCandidateAction(context.Background(), browser, loadPositionConfig(t), candidate, "candidate.greet"); err != nil {
				t.Fatal(err)
			}
			if len(browser.scrolls) != 4 || browser.scrolls[2].Target != nil || browser.scrolls[2].MaxAttempts != 1 || (browser.scrolls[2].Distance > 0) != (initialY > 528) {
				t.Fatal("上下安全区校正必须是有限的真实竖向滚轮")
			}
			for _, index := range []int{1, 3} {
				request := browser.scrolls[index]
				if request.RequireFull == nil || !*request.RequireFull || request.ViewportMargin != 48 {
					t.Fatal("校正后的动作仍须核对同一张卡片，横向留白不能跟着扩大")
				}
				for _, variant := range request.Target.Variants() {
					bound := false
					for _, parent := range variant.Parents {
						if parent.Attributes["id"] == "RESUME_CARD_20001010" && parent.Index != nil && *parent.Index == 10 {
							bound = true
						}
					}
					if !bound {
						t.Fatal("每条备用按钮路径都必须保留同一候选人的当前序号和精确编号")
					}
				}
			}
		}
	}
}

// TestActionUnsafeBottomStops 验证底部遮挡始终不消失时最多校正三轮，窗口安全区放不下按钮则不校正也不点击。
func TestActionUnsafeBottomStops(t *testing.T) {
	for _, height := range []float64{420, 816} {
		viewport := contract.ViewportSize{Width: 900, Height: height}
		views := []contract.ElementView{{Box: contract.ElementBox{X: 88, Y: 215.3, Width: 1032, Height: 158}, Viewport: viewport, Visible: true}}
		for index := 0; index < 4; index++ {
			views = append(views, contract.ElementView{Box: contract.ElementBox{X: 811, Y: height - 96, Width: 80, Height: 32}, Viewport: viewport, Visible: true})
		}
		browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, views: views}
		if err := NewRuntime().scrollCandidateAction(context.Background(), browser, loadPositionConfig(t), greetCandidateFixture(), "candidate.greet"); err == nil {
			t.Fatal("底部按钮不安全时必须停止")
		}
		wantScrolls := 8
		if height == 420 {
			wantScrolls = 2
		}
		if len(browser.scrolls) != wantScrolls {
			t.Fatalf("安全校正次数失控：height=%v scrolls=%d", height, len(browser.scrolls))
		}
	}
}

// TestActionSmallBottomGapKeepsWheelStep 验证离安全底边仅差10.2像素时仍有足够真实滚轮步长，且以最终位置而非请求距离判断安全。
func TestActionSmallBottomGapKeepsWheelStep(t *testing.T) {
	viewport := contract.ViewportSize{Width: 900, Height: 816}
	browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, views: []contract.ElementView{
		{Box: contract.ElementBox{X: 88, Y: 215.3, Width: 1032, Height: 158}, Viewport: viewport, Visible: true},
		{Box: contract.ElementBox{X: 811, Y: 489.2, Width: 41, Height: 49}, Viewport: viewport, Visible: true},
		{Box: contract.ElementBox{X: 811, Y: 429.2, Width: 41, Height: 49}, Viewport: viewport, Visible: true},
	}}
	if err := NewRuntime().scrollCandidateAction(context.Background(), browser, loadPositionConfig(t), greetCandidateFixture(), "candidate.greet"); err != nil {
		t.Fatal(err)
	}
	if len(browser.scrolls) != 4 || browser.scrolls[2].Target != nil || browser.scrolls[2].Distance != 120 || browser.scrolls[2].MaxAttempts != 1 {
		t.Fatal("小距离滚轮不能衰减到一直差一点，也不能跳过最终安全区确认")
	}
}

// TestSameCandidateAcrossIndexesKeepsFingerprint 验证同一简历从0换到1/10/49时身份不变，剥除完整索引而不是固定切一位。
func TestSameCandidateAcrossIndexesKeepsFingerprint(t *testing.T) {
	for _, index := range []int{0, 1, 10, 49} {
		raw := "RESUME_CARD_100001" + strconv.Itoa(index)
		browser := &positionBrowserStub{cards: []contract.FindAllItem{{Index: index, Text: "合成人选 28岁", Fields: map[string]string{"name": "合成人选", "age": "28", "platform_candidate_id": raw}}}}
		candidates, err := NewRuntime().FindCandidates(context.Background(), browser, loadPositionConfig(t), model.CandidateListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(candidates) != 1 || candidates[0].Fingerprint != "51job_100001" || candidates[0].Fields["platform_candidate_id"] != "100001" || candidates[0].Fields["card_dom_id"] != raw {
			t.Fatal("换序不能把同一人认成新人")
		}
	}
}

// TestReorderedSameNamesUseUniqueCurrentDOMID 验证同名同龄人选换序后，原候选人按稳定编号找到新位置，且不污染共享配置。
func TestReorderedSameNamesUseUniqueCurrentDOMID(t *testing.T) {
	cfg := loadPositionConfig(t)
	candidate := model.Candidate{Index: 0, Name: "合成人选", IdentityTexts: []string{"合成人选", "28岁"}, Fields: map[string]string{"platform_candidate_id": "100001", "card_dom_id": "RESUME_CARD_1000010"}}
	browser := &identityScopeStub{source: `<li id="RESUME_CARD_2000020">合成人选 28岁</li><li id="RESUME_CARD_1000011">合成人选 28岁</li>`}
	if err := NewRuntime().ScrollToCandidate(context.Background(), browser, cfg, candidate); err != nil {
		t.Fatal(err)
	}
	if len(browser.scrolls) != 1 {
		t.Fatal("目标不唯一时不能滚动")
	}
	target := browser.scrolls[0].Target.Target
	if target.Attributes["id"] != "RESUME_CARD_1000011" || target.Index == nil || *target.Index != 1 || len(target.Texts) != 0 {
		t.Fatal("动作必须绑定当前唯一DOM编号，不能继续按旧的姓名序号定位")
	}
	if cfg.Selectors["candidate.item"].Target.Attributes["id"] != "" || candidate.Fields["card_dom_id"] != "RESUME_CARD_1000010" {
		t.Fatal("临时定位不能污染原配置或原候选人快照")
	}
}

// TestScopeUsesCurrentTenthIndex 验证属性核验位于取序号之后时，仍按新名单第十张定位，再核对精确DOM编号。
func TestScopeUsesCurrentTenthIndex(t *testing.T) {
	var source strings.Builder
	for index := 0; index < 10; index++ {
		fmt.Fprintf(&source, `<li id="RESUME_CARD_%d%d">合成人选 28岁</li>`, 200000+index, index)
	}
	source.WriteString(`<li id="RESUME_CARD_10000110">合成人选 28岁</li>`)
	browser := &identityScopeStub{source: source.String()}
	if err := NewRuntime().ScrollToCandidate(context.Background(), browser, loadPositionConfig(t), model.Candidate{Name: "合成人选", Fields: map[string]string{"platform_candidate_id": "100001"}}); err != nil {
		t.Fatal(err)
	}
	target := browser.scrolls[0].Target.Target
	if target.Index == nil || *target.Index != 10 || target.Attributes["id"] != "RESUME_CARD_10000110" {
		t.Fatal("不能先把index置0再校验非首张卡片属性")
	}
}

// TestStickyHeaderUsesVerticalWheelOnly 验证窄宽窗口都只通过负向真实滚轮避开固定页头，不放大左右留白。
func TestStickyHeaderUsesVerticalWheelOnly(t *testing.T) {
	for _, width := range []float64{900, 1600} {
		browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, positions: []float64{38, 232}, width: width}
		if err := NewRuntime().ScrollToCandidate(context.Background(), browser, loadPositionConfig(t), model.Candidate{Name: "合成人选", Fields: map[string]string{"platform_candidate_id": "100001"}}); err != nil {
			t.Fatal(err)
		}
		if len(browser.scrolls) != 3 || browser.scrolls[1].Target != nil || browser.scrolls[1].Distance >= 0 || browser.scrolls[1].MaxAttempts != 1 {
			t.Fatal("固定页头避让必须用单次负向真实滚轮并复核")
		}
		for _, request := range browser.scrolls {
			if request.Target != nil && request.ViewportMargin != 48 {
				t.Fatal("不能给左右也强塞200像素留白")
			}
		}
	}
}

// TestStickyHeaderStillBlockedStops 验证真实滚轮后仍被固定页头覆盖时停止，不继续点击或发送。
func TestStickyHeaderStillBlockedStops(t *testing.T) {
	browser := &identityScopeStub{source: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`, positions: []float64{38, 38, 38, 38}}
	if err := NewRuntime().ScrollToCandidate(context.Background(), browser, loadPositionConfig(t), model.Candidate{Name: "合成人选", Fields: map[string]string{"platform_candidate_id": "100001"}}); err == nil {
		t.Fatal("卡片仍被遮挡时必须停止")
	}
}

// TestIdentityScopeRejectsAmbiguousOrChangedFormats 验证重复简历、尾缀不符和目标消失时均不执行页面动作。
func TestIdentityScopeRejectsAmbiguousOrChangedFormats(t *testing.T) {
	for _, source := range []string{`<li id="RESUME_CARD_1000010"></li><li id="RESUME_CARD_1000011"></li>`, `<li id="RESUME_CARD_1000019"></li>`, `<li id="RESUME_CARD_2000020"></li>`} {
		browser := &identityScopeStub{source: source}
		err := NewRuntime().ScrollToCandidate(context.Background(), browser, loadPositionConfig(t), model.Candidate{Fields: map[string]string{"platform_candidate_id": "100001"}})
		if err == nil || len(browser.scrolls) != 0 {
			t.Fatal("身份不能唯一确认时必须停下")
		}
	}
}
