// Package hliepin 文件作用：验证开聊动作与结果验证分离，延迟成功时不重复提交。
package hliepin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// confirmationBrowser 模拟点击完成但弹框延迟关闭、或候选人已经变为继续沟通。
type confirmationBrowser struct {
	model.Browser
	clicks     int
	probes     int
	hideAfter  int
	contacted  bool
	clickError error
}

// Click 记录唯一一次提交，并返回待验证的结果。
func (b *confirmationBrowser) Click(context.Context, contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks++
	return contract.ClickResult{}, b.clickError
}

// FindAll 返回当前候选人的沟通状态或职位弹框状态。
func (b *confirmationBrowser) FindAll(_ context.Context, req contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if req.Selector.Description == "candidate.continue" {
		if b.contacted {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
		return nil, nil
	}
	b.probes++
	if b.hideAfter > 0 && b.probes >= b.hideAfter {
		return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	}
	return []contract.FindAllItem{{Index: 0}}, nil
}

// TestConfirmGreetingRecoversWithoutClickingAgain 验证弹框迟关或已沟通时只读复查，始终只点击一次。
func TestConfirmGreetingRecoversWithoutClickingAgain(t *testing.T) {
	for _, contacted := range []bool{false, true} {
		b := &confirmationBrowser{hideAfter: 2, contacted: contacted, clickError: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_VERIFICATION_FAILED"}}}
		cfg := recoveryConfig()
		cfg.Selectors["candidate.greet_without_job"] = selectorForGreetingTest("candidate.greet_without_job")
		if err := confirmGreeting(context.Background(), b, cfg, model.Candidate{Index: 2}, "candidate.greet_without_job", "不选择职位开聊"); err != nil {
			t.Fatal(err)
		}
		if b.clicks != 1 {
			t.Fatalf("不应重复提交：%d", b.clicks)
		}
	}
}

// TestConfirmGreetingKeepsUncertainResult 验证没有成功证据时不误报成功，也不再次点击。
func TestConfirmGreetingKeepsUncertainResult(t *testing.T) {
	b := &confirmationBrowser{clickError: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_VERIFICATION_FAILED"}}}
	cfg := recoveryConfig()
	cfg.Selectors["candidate.greet_without_job"] = selectorForGreetingTest("candidate.greet_without_job")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := confirmGreeting(ctx, b, cfg, model.Candidate{Index: 0}, "candidate.greet_without_job", "不选择职位开聊")
	if err == nil || !strings.Contains(err.Error(), "开聊结果尚未确认") || b.clicks != 1 {
		t.Fatalf("%v clicks=%d", err, b.clicks)
	}
}

// TestConfirmGreetingPreservesClickFailure 验证真正的点击失败不进入结果恢复流程。
func TestConfirmGreetingPreservesClickFailure(t *testing.T) {
	original := errors.New("mouse failed")
	b := &confirmationBrowser{clickError: original}
	cfg := recoveryConfig()
	cfg.Selectors["candidate.greet_without_job"] = selectorForGreetingTest("candidate.greet_without_job")
	err := confirmGreeting(context.Background(), b, cfg, model.Candidate{}, "candidate.greet_without_job", "不选择职位开聊")
	if !errors.Is(err, original) || b.clicks != 1 || b.probes != 0 {
		t.Fatalf("%v %+v", err, b)
	}
}
