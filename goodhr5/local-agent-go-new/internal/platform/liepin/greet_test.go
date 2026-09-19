// Package liepin 文件作用：验证企业端必须得到发送成功证据，权益不足等未发送场景不能当作成功。
package liepin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// greetingEvidenceBrowser 模拟点击成功但业务发送可能被平台拒绝的浏览器。
type greetingEvidenceBrowser struct {
	model.Browser
	confirmed bool
	clicks    int
	confirmAt time.Time
}

// Click 记录真实发送入口点击，拒绝测试意外触发其它动作。
func (b *greetingEvidenceBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if request.Selector.Description != "猎聘企业端打招呼按钮" {
		return contract.ClickResult{}, errors.New("意外点击其它按钮")
	}
	b.clicks++
	return contract.ClickResult{}, nil
}

// FindAll 仅在平台确认发送后显示当前候选人的继续沟通按钮。
func (b *greetingEvidenceBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if request.Selector.Target.Text == "继续沟通" && b.clicks > 0 && b.confirmed && !time.Now().Before(b.confirmAt) {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
}

// TestGreetingWaitsForDelayedEvidence 验证平台超过旧五秒窗口才更新按钮时仍只发送一次。
func TestGreetingWaitsForDelayedEvidence(t *testing.T) {
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	browser := &greetingEvidenceBrowser{confirmed: true, confirmAt: time.Now().Add(5200 * time.Millisecond)}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if err = NewRuntime().GreetCandidate(ctx, browser, cfg, model.Candidate{}, model.GreetRequest{}); err != nil {
		t.Fatalf("延迟出现的发送证据应被确认：%v", err)
	}
	if browser.clicks != 1 {
		t.Fatalf("等待状态不能重复发送，实际点击 %d 次", browser.clicks)
	}
}

// TestGreetingRequiresPlatformEvidence 验证业务拒绝不会因底层点击成功而误判，且只点击一次不会自动重发。
func TestGreetingRequiresPlatformEvidence(t *testing.T) {
	content, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg model.Config
	if err = json.Unmarshal(content, &cfg); err != nil {
		t.Fatal(err)
	}
	for _, confirmed := range []bool{true, false} {
		browser := &greetingEvidenceBrowser{confirmed: confirmed}
		err = NewRuntime().GreetCandidate(context.Background(), browser, cfg, model.Candidate{}, model.GreetRequest{})
		if confirmed && err != nil {
			t.Fatalf("有发送证据应成功：%v", err)
		}
		if !confirmed && !errors.Is(err, model.ErrGreetingUnconfirmed) {
			t.Fatalf("未发送应明确失败：%v", err)
		}
		if browser.clicks != 1 {
			t.Fatalf("不得盲目重复发送，实际点击 %d 次", browser.clicks)
		}
	}
}
