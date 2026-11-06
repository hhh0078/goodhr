// Package job51 文件作用：验证索要选项为空、聊天对象不符及脱敏同姓聊天的安全边界，全程使用合成协议回包。
package job51

import (
	"context"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type followupBrowserStub struct {
	model.Browser
	opened         bool
	name           string
	clicks         []string
	drawer         bool
	input          string
	sent           []string
	wechatConfirm  bool
	wechatSent     int
	resumeSent     bool
	resumePosition string
	resumeClicks   int
	resumeErr      error
	resumeVerified bool
	closeMoves     int
	closeErr       error
}

// Read 返回合成聊天框姓名，不读取真实候选人资料。
func (s *followupBrowserStub) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	if request.Property == "html" {
		return contract.ReadResult{Value: `<li id="RESUME_CARD_1000010">合成人选 28岁</li>`}, nil
	}
	if request.Selector.Description == "前程无忧聊天消息输入框" {
		return contract.ReadResult{Value: s.input}, nil
	}
	return contract.ReadResult{Value: s.name}, nil
}

// Scroll 模拟卡片位于固定页头下方，不发出实际滚轮。
func (s *followupBrowserStub) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	return contract.ScrollResult{After: &contract.ElementView{Box: contract.ElementBox{X: 20, Y: 300, Width: 700, Height: 200}, Viewport: contract.ViewportSize{Width: 900, Height: 1040}, Visible: true}}, nil
}

// FindAll 模拟聊天框、关闭按钮和当前卡片继续沟通入口的存在状态。
func (s *followupBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	switch request.Selector.Description {
	case "candidate.chat_modal", "candidate.chat_close":
		if s.opened {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
	case "candidate.continue":
		return []contract.FindAllItem{{Index: 0}}, nil
	case "前程无忧展开的联系人侧栏":
		if s.drawer {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
	case "前程无忧主候选人卡片":
		return []contract.FindAllItem{{Index: 0}}, nil
	case "前程无忧已发送投递邀请":
		if s.resumeSent && (len(request.Selector.Target.Texts) == 0 || request.Selector.Target.Texts[0] == s.resumePosition) {
			return []contract.FindAllItem{{Index: 0}}, nil
		}
	}
	return nil, nil
}

// Click 模拟先关闭旧聊天，再从当前卡片打开聊天，不发送实际消息。
func (s *followupBrowserStub) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	s.clicks = append(s.clicks, request.Selector.Description)
	if request.MoveToCenterAfterClick {
		s.closeMoves++
	}
	if request.Selector.Description == "candidate.chat_close" {
		s.opened = false
		if s.closeErr != nil {
			return contract.ClickResult{}, s.closeErr
		}
	}
	if request.Selector.Description == "candidate.continue" {
		s.opened = true
	}
	switch request.Selector.Description {
	case "前程无忧收起联系人侧栏":
		s.drawer = false
	case "前程无忧聊天发送按钮":
		s.sent = append(s.sent, s.input)
		s.input = ""
	case "前程无忧交换微信入口":
		s.wechatConfirm = true
	case "前程无忧发送微信请求":
		s.wechatConfirm = false
		s.wechatSent++
	case "前程无忧邀投递入口":
		s.resumeClicks++
		if s.resumeErr != nil {
			return contract.ClickResult{}, s.resumeErr
		}
		s.resumeVerified = request.Verify != nil && request.Verify.TargetVisible != nil && request.Verify.TargetVisible.Description == "前程无忧已发送投递邀请" && request.Verify.TimeoutMS > 0
		if !s.resumeVerified {
			return contract.ClickResult{}, errors.New("邀投递缺少发送结果验证")
		}
		s.resumeSent = true
		if names := request.Verify.TargetVisible.Target.Texts; len(names) > 0 {
			s.resumePosition = names[0]
		}
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Input 只记录合成输入内容，由测试发送回包清空，不连接真实聊天服务。
func (s *followupBrowserStub) Input(_ context.Context, request contract.ElementInputRequest) (contract.InputResult, error) {
	s.input = request.Text
	return contract.InputResult{}, nil
}

// followupConfigFixture 复用正式选择器结构，只把协议分派名称改成逻辑键；不以合成节点声明真实页面适配完成。
func followupConfigFixture(t *testing.T) model.Config {
	t.Helper()
	cfg := loadPositionConfig(t)
	for _, key := range []string{"candidate.chat_modal", "candidate.chat_close", "candidate.chat_name", "candidate.continue"} {
		spec := cfg.Selectors["candidate.greet"]
		spec.Description = key
		cfg.Selectors[key] = spec
	}
	return cfg
}

// TestFollowupRequestsDoNotBuyPhone 验证电话用礼貌消息索要、微信走原生确认、简历点击邀投递，绝不点付费获取电话。
func TestFollowupRequestsDoNotBuyPhone(t *testing.T) {
	cfg := followupConfigFixture(t)
	paid := cfg.Selectors["candidate.greet"]
	paid.Description = "付费获取电话"
	cfg.Selectors["candidate.request_phone"] = paid
	browser := &followupBrowserStub{name: "合成人选", opened: true}
	err := NewRuntime().RequestCandidateInfo(context.Background(), browser, cfg, model.Candidate{Name: "合成人选"}, model.CandidateInfoRequest{RequestPhone: true, RequestWechat: true, RequestResume: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(browser.sent) != 1 || browser.sent[0] != cfg.Behavior.RequestPhoneMessage || browser.wechatSent != 1 || browser.wechatConfirm || browser.resumeClicks != 1 || !browser.resumeVerified || browser.closeMoves != 0 {
		t.Fatal("电话消息、微信确认与邀投递没有按配置完成")
	}
	for _, click := range browser.clicks {
		if click == "付费获取电话" {
			t.Fatal("索要手机不能误买获取电话权益")
		}
	}
}

// TestRequestResumeInvitation 验证同岗去重、旧岗位不误判、失败不重发及身份不符时不触碰邀请按钮。
func TestRequestResumeInvitation(t *testing.T) {
	for _, item := range []struct {
		name        string
		chatName    string
		oldPosition string
		clickErr    error
		wantClicks  int
		wantErr     bool
	}{
		{name: "首次邀请", chatName: "合成人选", wantClicks: 1},
		{name: "同岗已邀请", chatName: "合成人选", oldPosition: "目标岗位"},
		{name: "其他岗位邀请不能代替本次", chatName: "合成人选", oldPosition: "旧岗位", wantClicks: 1},
		{name: "未确认发送不重试", chatName: "合成人选", clickErr: errors.New("结果验证超时"), wantClicks: 1, wantErr: true},
		{name: "候选人不符", chatName: "其他人选", wantErr: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			browser := &followupBrowserStub{
				name: item.chatName, resumeSent: item.oldPosition != "", resumePosition: item.oldPosition, resumeErr: item.clickErr,
			}
			runtime := &Runtime{positionName: "目标岗位"}
			err := runtime.RequestCandidateInfo(context.Background(), browser, followupConfigFixture(t), model.Candidate{Name: "合成人选"}, model.CandidateInfoRequest{RequestResume: true})
			if (err != nil) != item.wantErr || browser.resumeClicks != item.wantClicks || len(browser.sent) != 0 || browser.input != "" {
				t.Fatalf("邀投递结果不符：err=%v, clicks=%d, messages=%v", err, browser.resumeClicks, browser.sent)
			}
			if item.clickErr != nil && !errors.Is(err, item.clickErr) {
				t.Fatalf("邀请失败必须保留原因：%v", err)
			}
		})
	}
}

// TestRequestResumeRequiresConfiguredResult 验证缺失结果选择器时停止，不能点击后猜测成功或退回文字消息。
func TestRequestResumeRequiresConfiguredResult(t *testing.T) {
	cfg := followupConfigFixture(t)
	delete(cfg.Selectors, "candidate.request_resume_sent")
	browser := &followupBrowserStub{name: "合成人选"}
	err := NewRuntime().RequestCandidateInfo(context.Background(), browser, cfg, model.Candidate{Name: "合成人选"}, model.CandidateInfoRequest{RequestResume: true})
	if err == nil || len(browser.clicks) != 0 || len(browser.sent) != 0 {
		t.Fatal("缺少发送结果配置时必须在点击前停止")
	}
}

// TestCloseConversationAlsoFoldsDrawer 验证关闭聊天后继续收起联系人侧栏，避免它挡住下一位候选人。
func TestCloseConversationAlsoFoldsDrawer(t *testing.T) {
	browser := &followupBrowserStub{opened: true, drawer: true}
	if err := NewRuntime().CloseCandidateConversation(context.Background(), browser, followupConfigFixture(t), model.Candidate{}); err != nil {
		t.Fatal(err)
	}
	if browser.opened || browser.drawer || len(browser.clicks) != 2 || browser.closeMoves != 2 {
		t.Fatal("聊天框和联系人侧栏都必须确认关闭")
	}
}

// TestCloseConversationPreservesMouseMoveFailure 验证聊天框关闭后的鼠标移动失败会返回错误，且不重放关闭点击。
func TestCloseConversationPreservesMouseMoveFailure(t *testing.T) {
	failure := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "MOVE_FAILED", Details: []byte(`{"post_click_move_failed":true}`)}}
	browser := &followupBrowserStub{opened: true, closeErr: failure}
	err := NewRuntime().CloseCandidateConversation(context.Background(), browser, followupConfigFixture(t), model.Candidate{})
	if !errors.Is(err, failure) || browser.opened || len(browser.clicks) != 1 || browser.closeMoves != 1 {
		t.Fatalf("聊天关闭不能掩盖移走失败或重复点击：clicks=%v err=%v", browser.clicks, err)
	}
}

// TestRequestInfoEmptyAndMismatch 验证无索要选项不触碰浏览器，姓名不一致时也不点击任何索要动作。
func TestRequestInfoEmptyAndMismatch(t *testing.T) {
	if err := NewRuntime().RequestCandidateInfo(context.Background(), nil, model.Config{}, model.Candidate{}, model.CandidateInfoRequest{}); err != nil {
		t.Fatal(err)
	}
	browser := &followupBrowserStub{name: "合成人选乙"}
	err := NewRuntime().RequestCandidateInfo(context.Background(), browser, followupConfigFixture(t), model.Candidate{Name: "合成人选甲"}, model.CandidateInfoRequest{RequestPhone: true})
	if err == nil || len(browser.clicks) != 0 {
		t.Fatal("聊天对象不符时不能索要任何资料")
	}
}

// TestMaskedNameReopensFromCard 验证页面没有公开会话编号时，不靠同姓、脱敏名或重名复用旧聊天。
func TestMaskedNameReopensFromCard(t *testing.T) {
	for _, item := range []struct {
		name, actual string
		reopen       bool
	}{{"李**", "李先生", true}, {"李先生", "李先生", true}, {"合成人选", "合成人选", true}} {
		browser := &followupBrowserStub{opened: true, name: item.actual}
		if err := NewRuntime().EnsureCandidateConversation(context.Background(), browser, followupConfigFixture(t), model.Candidate{Name: item.name, Fields: map[string]string{"platform_candidate_id": "100001"}}); err != nil {
			t.Fatal(err)
		}
		if item.reopen {
			if len(browser.clicks) != 2 || browser.clicks[0] != "candidate.chat_close" || browser.clicks[1] != "candidate.continue" {
				t.Fatalf("脱敏同姓必须由当前卡片重开：%v", browser.clicks)
			}
		} else if len(browser.clicks) != 0 {
			t.Fatal("已确认的完整姓名聊天不应重复打开")
		}
	}
}
