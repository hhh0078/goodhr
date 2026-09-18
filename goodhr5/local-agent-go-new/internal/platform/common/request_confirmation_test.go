// Package common 文件作用：验证索要信息的确认按钮不能跳过，未确认时不能继续索要下一项。
package common

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// confirmationBrowser 记录索要和确认的完整调用顺序及隐藏验证参数。
type confirmationBrowser struct {
	model.Browser
	clicks []contract.ElementClickRequest
	fail   bool
}

// Click 模拟配置中的确认按钮成功或未出现，不允许通过可选探测跳过。
func (b *confirmationBrowser) Click(_ context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	b.clicks = append(b.clicks, req)
	if b.fail && req.Selector.Description == "确认电话" {
		return contract.ClickResult{}, errors.New("确认框未完成")
	}
	return contract.ClickResult{Clicked: true}, nil
}

// TestRequestInfoRequiresConfirmation 验证确认成功才继续下一项，失败时立即停止后续索要。
func TestRequestInfoRequiresConfirmation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		b := &confirmationBrowser{fail: fail}
		cfg := model.Config{Selectors: map[string]contract.SelectorSpec{
			"candidate.request_phone": selector("电话"), "candidate.request_phone_confirm": selector("确认电话"),
			"candidate.request_wechat": selector("微信"), "candidate.request_wechat_confirm": selector("确认微信"),
			"candidate.request_confirm_modal": selector("确认弹框"),
		}}
		err := RequestCandidateInfo(context.Background(), b, cfg, model.CandidateInfoRequest{RequestPhone: true, RequestWechat: true})
		if (err != nil) != fail {
			t.Fatalf("错误状态不正确：%v", err)
		}
		want := 4
		if fail {
			want = 2
		}
		if len(b.clicks) != want {
			t.Fatalf("确认未完成仍继续操作：%d", len(b.clicks))
		}
		if b.clicks[1].Verify == nil || b.clicks[1].Verify.TargetHidden == nil {
			t.Fatal("确认后缺少弹框消失验证")
		}
	}
}
