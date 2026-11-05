// Package job51 文件作用：验证前程无忧招呼只有收到卡片状态证据才计为成功，避免未配置、重复或无确认的发送。
package job51

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type greetBrowserStub struct {
	model.Browser
	contacted         bool
	confirmAfterClick bool
	clicks            int
	afterClickErr     error
	blocked           bool
}

// Read 仅返回合成主卡片HTML，供稳定编号重定位使用。
func (s *greetBrowserStub) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	return contract.ReadResult{Value: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`}, nil
}

// Scroll 返回合成的安全卡片位置，不执行真实滚轮。
func (s *greetBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{After: &contract.ElementView{Box: contract.ElementBox{X: 20, Y: 300, Width: 700, Height: 200}, Viewport: contract.ViewportSize{Width: 900, Height: 1040}, Visible: true}}, nil
}

// greetCandidateFixture 提供合成稳定简历编号，不沿用会变化的DOM索引后缀。
func greetCandidateFixture() model.Candidate {
	return model.Candidate{Name: "合成人选", Fields: map[string]string{"platform_candidate_id": "100001"}}
}

// FindAll 只模拟被测卡片是否出现已沟通按钮，不查询真实页面。
func (s *greetBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Description == "前程无忧招呼职位选择层" {
		return nil, nil
	}
	if request.Selector.Description == "前程无忧聊天点数浮层" {
		if s.blocked {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	}
	if s.clicks > 0 && s.afterClickErr != nil {
		return nil, s.afterClickErr
	}
	if s.contacted {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// TestGreetReadFailureStopsImmediately 验证点击后读取状态失败也作为发送未确认，主流程不能继续消耗其他候选人的招呼额度。
func TestGreetReadFailureStopsImmediately(t *testing.T) {
	cfg := loadPositionConfig(t)
	state := cfg.Selectors["candidate.greet"]
	state.Target.Text = "合成已沟通"
	cfg.Selectors["candidate.continue"] = state
	browser := &greetBrowserStub{afterClickErr: errors.New("合成状态读取中断")}
	err := NewRuntime().GreetCandidate(context.Background(), browser, cfg, greetCandidateFixture(), model.GreetRequest{})
	if !errors.Is(err, model.ErrGreetingUnconfirmed) || browser.clicks != 1 {
		t.Fatalf("发送后不确定必须立即停止：%v", err)
	}
}

// Click 模拟一次招呼操作及可选的成功卡片状态。
func (s *greetBrowserStub) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	s.clicks++
	s.contacted = s.confirmAfterClick
	return contract.ClickResult{}, nil
}

// TestGreetRequiresConfiguredEvidence 验证成功状态未配置时拒绝发送，不能把点击成功当成招呼成功。
func TestGreetRequiresConfiguredEvidence(t *testing.T) {
	cfg := loadPositionConfig(t)
	delete(cfg.Selectors, "candidate.continue")
	browser := &greetBrowserStub{}
	err := NewRuntime().GreetCandidate(context.Background(), browser, cfg, greetCandidateFixture(), model.GreetRequest{})
	if !errors.Is(err, model.ErrGreetingUnconfirmed) || browser.clicks != 0 {
		t.Fatalf("缺少确认配置时不应发送：%v", err)
	}
}

// TestGreetChecksCardState 验证已沟通直接跳过、实际状态更新才成功，无状态更新的点击仍返回错误。
func TestGreetChecksCardState(t *testing.T) {
	cfg := loadPositionConfig(t)
	// 测试合成状态沿用正式卡片动作结构，真实状态文字由页面取证后配置。
	state := cfg.Selectors["candidate.greet"]
	state.Target.Text = "合成已沟通"
	cfg.Selectors["candidate.continue"] = state
	for _, item := range []struct {
		contacted, confirm bool
		wantClicks         int
		wantAlready        bool
	}{{true, false, 0, true}, {false, true, 1, false}, {false, false, 1, false}} {
		browser := &greetBrowserStub{contacted: item.contacted, confirmAfterClick: item.confirm}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		err := NewRuntime().GreetCandidate(ctx, browser, cfg, greetCandidateFixture(), model.GreetRequest{})
		cancel()
		if browser.clicks != item.wantClicks {
			t.Fatalf("招呼次数不正确：%d", browser.clicks)
		}
		if item.wantAlready && !errors.Is(err, model.ErrCandidateAlreadyContacted) {
			t.Fatalf("已沟通不应重复发送：%v", err)
		}
		if item.confirm && err != nil {
			t.Fatal(err)
		}
		if !item.confirm && !item.wantAlready && err == nil {
			t.Fatal("点击后没有状态证据，不能算成功")
		}
	}
}

// TestGreetBlockerNeverRefreshesOrSends 验证积分提示挡住按钮时停止，不刷新名单，也不自动重发。
func TestGreetBlockerNeverRefreshesOrSends(t *testing.T) {
	browser := &greetBrowserStub{blocked: true}
	err := NewRuntime().GreetCandidate(context.Background(), browser, loadPositionConfig(t), greetCandidateFixture(), model.GreetRequest{})
	if !errors.Is(err, model.ErrGreetingUnconfirmed) || browser.clicks != 0 {
		t.Fatal("积分提示未关闭时不能自动点击或发送")
	}
}
