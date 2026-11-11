// Package common 文件作用：验证跨系统详情入口数组按顺序各点一次，失败切换且成功、取消或会话失效立即停止。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// detailVariantBrowser 记录独立点击请求，模拟新旧页面入口以及明确未点击、结果不确定和断线。
type detailVariantBrowser struct {
	model.Browser
	clicks        []string
	errors        map[string]error
	success       string
	opened        bool
	cancel        context.CancelFunc
	openWithError bool
}

// Click 仅接受一条已保留候选人作用域的入口，真实页面和消息均不参与本测试。
func (b *detailVariantBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if len(request.Selector.Target.Selectors) != 1 || len(request.Selector.Alternatives) != 0 || len(request.Selector.Parents) == 0 {
		return contract.ClickResult{}, errors.New("未展开独立入口或候选人作用域丢失")
	}
	value := request.Selector.Target.Selectors[0].Value
	b.clicks = append(b.clicks, value)
	if b.cancel != nil {
		b.cancel()
	}
	if value == b.success {
		b.opened = true
	}
	if err := b.errors[value]; err != nil {
		if b.openWithError {
			b.opened = true
		}
		return contract.ClickResult{}, err
	}
	return contract.ClickResult{Clicked: true}, nil
}

// FindAll 返回详情真实出现与否，不能把成功执行探测等同于详情已打开。
func (b *detailVariantBrowser) FindAll(context.Context, contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if b.opened {
		return []contract.FindAllItem{{}}, nil
	}
	return nil, nil
}

// TestDetailSelectorVariantsOnce 验证完整对象数组、目标数组、旧对象和重复配置均逐条只执行一次。
func TestDetailSelectorVariantsOnce(t *testing.T) {
	notClicked := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_FAILED", Details: json.RawMessage(`{"click_started":false}`)}}
	for _, test := range []struct {
		name, config, success string
		failures              map[string]error
		want                  []string
		wantError             bool
	}{
		{"新入口成功", `[{"target":{"selectors":[{"type":"css","value":".new"}]}},{"target":{"selectors":[{"type":"css","value":".old"}]}}]`, ".new", nil, []string{".new"}, false},
		{"新入口失败旧入口成功", `[{"target":{"selectors":[{"type":"css","value":".new"}]}},{"target":{"selectors":[{"type":"css","value":".old"}]}}]`, ".old", map[string]error{".new": notClicked}, []string{".new", ".old"}, false},
		{"目标数组且重复入口", `{"target":{"selectors":[{"type":"css","value":".new"},{"type":"css","value":".new"},{"type":"css","value":".old"}]}}`, ".old", map[string]error{".new": notClicked}, []string{".new", ".old"}, false},
		{"旧对象成功", `{"target":{"selectors":[{"type":"css","value":".old"}]}}`, ".old", nil, []string{".old"}, false},
		{"单入口失败不重试", `{"target":{"selectors":[{"type":"css","value":".old"}]}}`, "", map[string]error{".old": notClicked}, []string{".old"}, true},
		{"全部失败各一次", `{"target":{"selectors":[{"type":"css","value":".new"},{"type":"css","value":".old"}]}}`, "", map[string]error{".new": notClicked, ".old": notClicked}, []string{".new", ".old"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var spec contract.SelectorSpec
			if err := json.Unmarshal([]byte(test.config), &spec); err != nil {
				t.Fatal(err)
			}
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"candidate.item": selector("候选人卡片"), "candidate.open_target": spec, "candidate.detail": selector("详情正文")}}
			browser := &detailVariantBrowser{success: test.success, errors: test.failures}
			err := OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{Index: 2, IdentityTexts: []string{"唯一候选人"}})
			if (err != nil) != test.wantError || !reflect.DeepEqual(browser.clicks, test.want) {
				t.Fatalf("入口次数或结果错误：%v，点击%v，期望%v", err, browser.clicks, test.want)
			}
		})
	}
}

// TestDetailSelectorStopsOnUncertainResult 验证点击后即使回执报错，只要详情已打开就不能继续点击备用入口。
func TestDetailSelectorStopsOnUncertainResult(t *testing.T) {
	cfg := cleanupConfig()
	delete(cfg.Selectors, "candidate.detail_dialog")
	entry := selector("详情入口")
	entry.Target.Selectors = append(entry.Target.Selectors, contract.SelectorCandidate{Type: "css", Value: ".备用"})
	cfg.Selectors["candidate.open_target"] = entry
	uncertain := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_FAILED", Details: json.RawMessage(`{"click_started":true}`)}}
	browser := &detailVariantBrowser{errors: map[string]error{".详情入口": uncertain}, openWithError: true}
	if err := OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); err != nil || len(browser.clicks) != 1 {
		t.Fatalf("已打开后仍点击备用入口：%v，%v", err, browser.clicks)
	}
	ctx, cancel := context.WithCancel(context.Background())
	browser = &detailVariantBrowser{cancel: cancel}
	if err := OpenCandidateDetail(ctx, browser, cfg, model.Candidate{}); !errors.Is(err, context.Canceled) || len(browser.clicks) != 1 {
		t.Fatalf("取消后仍继续点击：%v，%v", err, browser.clicks)
	}
}

// TestDetailSelectorStopsOnLostSession 验证浏览器已退出时不再执行备用入口，也不把断线当成入口不存在。
func TestDetailSelectorStopsOnLostSession(t *testing.T) {
	cfg := cleanupConfig()
	entry := selector("详情入口")
	entry.Target.Selectors = append(entry.Target.Selectors, contract.SelectorCandidate{Type: "css", Value: ".备用"})
	cfg.Selectors["candidate.open_target"] = entry
	unavailable := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "PAGE_CLOSED"}}
	browser := &detailVariantBrowser{errors: map[string]error{".详情入口": unavailable}}
	if err := OpenCandidateDetail(context.Background(), browser, cfg, model.Candidate{}); !errors.Is(err, unavailable) || len(browser.clicks) != 1 {
		t.Fatalf("会话失效后仍执行备用入口：%v，%v", err, browser.clicks)
	}
}
