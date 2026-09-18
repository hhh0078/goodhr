// Package hliepin 文件作用：验证延迟推广弹框、遮挡恢复、完整失败清理及防止重复开聊。
package hliepin

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// recoveryBrowser 模拟职位弹框被推广、聊天和联系人侧栏遮挡的页面。
type recoveryBrowser struct {
	model.Browser
	panels         map[string]bool
	clicks         int
	esc            int
	blocked        bool
	contacted      bool
	contactOnClick bool
	alwaysFail     bool
	promotionAt    time.Time
	cleared        bool
}

// recoveryConfig 使用逻辑键模拟平台配置，不依赖真实招聘页面。
func recoveryConfig() model.Config {
	cfg := model.Config{Name: "猎聘", Selectors: map[string]contract.SelectorSpec{}}
	for _, key := range []string{"candidate.item", "candidate.greet", "candidate.continue", "candidate.greet_modal", "candidate.greet_promotion_modal", "candidate.greet_promotion_close", "candidate.chat_modal", "candidate.chat_close", "candidate.contact_drawer", "candidate.contact_drawer_close"} {
		cfg.Selectors[key] = selectorForGreetingTest(key)
	}
	return cfg
}

// FindAll 返回弹层和沟通按钮状态，并模拟一秒后才出现的推广弹框。
func (b *recoveryBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	key := req.Selector.Description
	if key == "candidate.greet_promotion_modal" && !b.promotionAt.IsZero() && time.Now().After(b.promotionAt) && !b.cleared {
		b.panels[key] = true
	}
	if b.panels[key] || (key == "candidate.continue" && b.contacted) {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// Click 首次开聊模拟出现遮挡，清理后第二次才显示职位弹框。
func (b *recoveryBrowser) Click(_ context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	switch req.Selector.Description {
	case "candidate.greet":
		b.clicks++
		if b.contactOnClick {
			b.contacted = true
			return contract.ClickResult{}, errors.New("已直接沟通，没有职位弹框")
		}
		if (b.blocked && b.clicks == 1) || b.alwaysFail {
			b.panels["candidate.greet_promotion_modal"] = true
			b.panels["candidate.chat_modal"] = true
			b.panels["candidate.contact_drawer"] = true
			return contract.ClickResult{}, errors.New("未出现职位弹框")
		}
		if b.panels["candidate.chat_modal"] || b.panels["candidate.contact_drawer"] {
			return contract.ClickResult{}, errors.New("遗留弹层尚未清理")
		}
		b.panels["candidate.greet_modal"] = true
	case "candidate.chat_close":
		b.panels["candidate.chat_modal"] = false
	case "candidate.contact_drawer_close":
		b.panels["candidate.contact_drawer"] = false
	case "candidate.greet_promotion_close":
		return contract.ClickResult{}, errors.New("关闭按钮不可见")
	}
	return contract.ClickResult{Clicked: true}, nil
}

// PressKey 模拟 Escape 按最上层弹框顺序关闭。
func (b *recoveryBrowser) PressKey(_ context.Context, req contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	if req.Key == "Escape" {
		b.esc++
		for _, key := range []string{"candidate.greet_promotion_modal", "candidate.greet_modal"} {
			if b.panels[key] {
				b.panels[key] = false
				if key == "candidate.greet_promotion_modal" {
					b.cleared = true
				}
				break
			}
		}
	}
	return contract.KeyboardPressResult{Pressed: true}, nil
}

// TestPromotionAppearsAfterOneSecond 验证发送后延迟一秒出现且关闭按钮不可用的弹框可由 Escape 清理。
func TestPromotionAppearsAfterOneSecond(t *testing.T) {
	b := &recoveryBrowser{panels: map[string]bool{}, promotionAt: time.Now().Add(time.Second)}
	if err := closePostGreetPromotion(context.Background(), b, recoveryConfig(), postGreetPromotionPollAttempts); err != nil {
		t.Fatal(err)
	}
	if !b.cleared || b.esc != 1 {
		t.Fatalf("延迟弹框没有关闭：%+v", b)
	}
}

// TestGreetingRetriesAfterClearingAllBlockers 验证推广、聊天和联系人侧栏清理后只重试一次。
func TestGreetingRetriesAfterClearingAllBlockers(t *testing.T) {
	b := &recoveryBrowser{panels: map[string]bool{}, blocked: true}
	if err := openGreetingModal(context.Background(), b, recoveryConfig(), model.Candidate{Index: 0}); err != nil {
		t.Fatal(err)
	}
	if b.clicks != 2 || !b.panels["candidate.greet_modal"] || b.panels["candidate.chat_modal"] || b.panels["candidate.contact_drawer"] {
		t.Fatalf("恢复步骤不完整：%+v", b)
	}
}

// TestGreetingDoesNotRetryContactedCandidate 验证页面已出现继续沟通时不重复点击立即沟通。
func TestGreetingDoesNotRetryContactedCandidate(t *testing.T) {
	b := &recoveryBrowser{panels: map[string]bool{}, contacted: true}
	err := openGreetingModal(context.Background(), b, recoveryConfig(), model.Candidate{Index: 0})
	if !errors.Is(err, model.ErrCandidateAlreadyContacted) || b.clicks != 0 {
		t.Fatalf("重复开聊保护未生效：%v %+v", err, b)
	}
}

// TestGreetingStopsRetryAfterContact 验证第一次点击改变沟通状态后不会再次点击。
func TestGreetingStopsRetryAfterContact(t *testing.T) {
	b := &recoveryBrowser{panels: map[string]bool{}, contactOnClick: true, promotionAt: time.Now().Add(time.Second)}
	err := openGreetingModal(context.Background(), b, recoveryConfig(), model.Candidate{Index: 0})
	if !errors.Is(err, model.ErrCandidateAlreadyContacted) || b.clicks != 1 || !b.cleared {
		t.Fatalf("点击后的延迟清理和重复开聊保护未生效：%v %+v", err, b)
	}
}

// TestGreetingFailureCleansEveryPanel 验证重试仍失败后所有已知弹层都清理且不会无限重试。
func TestGreetingFailureCleansEveryPanel(t *testing.T) {
	b := &recoveryBrowser{panels: map[string]bool{}, alwaysFail: true}
	err := NewRuntime().GreetCandidate(context.Background(), b, recoveryConfig(), model.Candidate{Index: 0}, model.GreetRequest{})
	if err == nil || b.clicks != 2 {
		t.Fatalf("应有限重试并返回错误：%v %+v", err, b)
	}
	for key, opened := range b.panels {
		if opened {
			t.Fatalf("弹框没有清理：%s", key)
		}
	}
}

// TestPromotionWaitHonorsCancellation 验证延迟观察期间停止任务能立即退出。
func TestPromotionWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &recoveryBrowser{panels: map[string]bool{}}
	if err := closePostGreetPromotion(ctx, b, recoveryConfig(), postGreetPromotionPollAttempts); !errors.Is(err, context.Canceled) {
		t.Fatalf("未响应取消：%v", err)
	}
}
