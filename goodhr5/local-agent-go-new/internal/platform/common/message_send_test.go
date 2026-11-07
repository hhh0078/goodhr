// Package common 文件作用：回归按钮发送、回车兜底、发送确认及避免重复发送的边界。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// messageSendBrowser 模拟输入、按钮、聚焦和按键，保留发送次数供回归检查。
type messageSendBrowser struct {
	model.Browser
	buttonErr, focusErr, keyErr    error
	clicked, keepDraft             bool
	inputs, buttons, focuses, keys int
	keyRequest                     contract.KeyboardPressRequest
	cancel                         context.CancelFunc
}

// testSelector 构造测试逻辑目标，不包含任何招聘平台选择器。
func testSelector(description string) contract.SelectorSpec {
	return contract.SelectorSpec{Description: description, Target: contract.SelectorGroup{Selectors: []contract.SelectorCandidate{{Type: "text", Value: description}}}}
}

// Input 记录消息输入次数，确保兜底不会再次输入。
func (b *messageSendBrowser) Input(context.Context, contract.ElementInputRequest) (contract.InputResult, error) {
	b.inputs++
	return contract.InputResult{Typed: true}, nil
}

// Click 区分发送按钮和重新聚焦，模拟点击前及点击后的失败。
func (b *messageSendBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	if request.Selector.Description == "消息输入框" {
		b.focuses++
		return contract.ClickResult{Clicked: b.focusErr == nil}, b.focusErr
	}
	b.buttons++
	if b.cancel != nil {
		b.cancel()
	}
	return contract.ClickResult{Clicked: b.clicked || b.buttonErr == nil}, b.buttonErr
}

// PressKey 记录是否只按了一次回车以及是否开启用户可见诊断。
func (b *messageSendBrowser) PressKey(_ context.Context, request contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.keys++
	b.keyRequest = request
	return contract.KeyboardPressResult{Pressed: b.keyErr == nil}, b.keyErr
}

// Read 模拟消息发送后输入框清空，或消息仍留在草稿中。
func (b *messageSendBrowser) Read(context.Context, contract.ElementReadRequest) (contract.ReadResult, error) {
	if b.keepDraft {
		return contract.ReadResult{Value: "你好"}, nil
	}
	return contract.ReadResult{}, nil
}

// TestMessageSendFallback 验证按钮优先、发送失败兜底和不确定结果不重发。
func TestMessageSendFallback(t *testing.T) {
	missing := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND", Step: "find", Message: "发送按钮未找到"}}
	moveFailed := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "MOVE_FAILED", Step: "move", Details: json.RawMessage(`{"click_started":false}`)}}
	uncertain := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_FAILED", Step: "click", Details: json.RawMessage(`{"click_started":true}`)}}
	for _, tc := range []struct {
		name    string
		browser messageSendBrowser
		allow   bool
		wantErr bool
		keys    int
	}{
		{"按钮成功不回车", messageSendBrowser{}, true, false, 0},
		{"按钮缺失改回车", messageSendBrowser{buttonErr: missing}, true, false, 1},
		{"移动失败改回车", messageSendBrowser{buttonErr: moveFailed}, true, false, 1},
		{"其他平台不开兜底", messageSendBrowser{buttonErr: missing}, false, true, 0},
		{"点击结果不确定不回车", messageSendBrowser{buttonErr: uncertain}, true, true, 0},
		{"已经点击不回车", messageSendBrowser{buttonErr: moveFailed, clicked: true}, true, true, 0},
		{"聚焦失败保留原错误", messageSendBrowser{buttonErr: missing, focusErr: errors.New("聚焦失败")}, true, true, 0},
		{"回车失败保留原错误", messageSendBrowser{buttonErr: missing, keyErr: errors.New("键盘失败")}, true, true, 1},
		{"浏览器中断不回车", messageSendBrowser{buttonErr: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Details: json.RawMessage(`{"click_started":false}`)}}}, true, true, 0},
		{"草稿未清空报错不重发", messageSendBrowser{keepDraft: true}, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := model.Config{Behavior: model.Behavior{MessageSendEnterFallback: tc.allow, MessageSendVerifyEmpty: true}, Selectors: map[string]contract.SelectorSpec{
				"input": testSelector("消息输入框"), "send": testSelector("发送按钮"),
			}}
			b := &tc.browser
			err := sendMessage(context.Background(), b, cfg, "input", "send", "你好")
			if (err != nil) != tc.wantErr || b.keys != tc.keys || b.inputs != 1 {
				t.Fatalf("发送策略错误：err=%v browser=%+v", err, b)
			}
			if b.keys > 0 && (b.keyRequest.Key != "Enter" || !b.keyRequest.Diagnostic || b.focuses != 1) {
				t.Fatalf("回车兜底缺少聚焦或诊断：%+v", b)
			}
			if tc.wantErr && b.buttonErr == missing && !strings.Contains(err.Error(), "发送按钮未找到") {
				t.Fatalf("丢失原按钮错误：%v", err)
			}
		})
	}
}

// TestMessageSendCancellation 验证岗位停止后不继续按回车发送。
func TestMessageSendCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &messageSendBrowser{cancel: cancel, buttonErr: &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND", Step: "find"}}}
	cfg := model.Config{Behavior: model.Behavior{MessageSendEnterFallback: true}, Selectors: map[string]contract.SelectorSpec{"input": testSelector("消息输入框"), "send": testSelector("发送按钮")}}
	err := sendMessage(ctx, b, cfg, "input", "send", "你好")
	if !errors.Is(err, context.Canceled) || b.keys != 0 || b.focuses != 0 {
		t.Fatalf("取消后仍发送：%v %+v", err, b)
	}
}

// enterFirstBrowser 保留草稿值、按键与按钮次数，模拟按键落地、回执丢失、输入模式和新消息。
type enterFirstBrowser struct {
	messageSendBrowser
	mode, draft string
}

// Input 模拟已验证输入或还原被快捷键换行的草稿。
func (b *enterFirstBrowser) Input(_ context.Context, r contract.ElementInputRequest) (contract.InputResult, error) {
	b.inputs++
	b.draft = r.Text
	return contract.InputResult{Typed: true, Verified: true}, nil
}

// Read 返回输入控件真实值或页面明确展示的发送快捷键。
func (b *enterFirstBrowser) Read(_ context.Context, r contract.ElementReadRequest) (contract.ReadResult, error) {
	if r.Attribute == "placeholder" && b.mode == "newline-mode" {
		return contract.ReadResult{Value: "Enter 换行，Ctrl+Enter 发送"}, nil
	}
	return contract.ReadResult{Value: b.draft, Property: r.Property}, nil
}

// PressKey 核验页面、焦点与草稿要求，按场景模拟真实按键结果。
func (b *enterFirstBrowser) PressKey(_ context.Context, r contract.KeyboardPressRequest) (contract.KeyboardPressResult, error) {
	b.keys++
	if r.Key != "Enter" || r.Target == nil || !r.RequireFocus || r.ExpectedPageURL != "page" || r.ExpectedValue == nil || *r.ExpectedValue != b.draft {
		return contract.KeyboardPressResult{}, errors.New("按键缺少现场约束")
	}
	switch b.mode {
	case "sent":
		b.draft = ""
	case "not-started", "changed-fallback":
		return contract.KeyboardPressResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_VISIBLE", Details: json.RawMessage(`{"key_started":false}`)}}
	case "lost-ack", "already-sent":
		return contract.KeyboardPressResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "INTERNAL_ERROR", Details: json.RawMessage(`{"key_started":true}`)}}
	case "newline-mode", "ambiguous-newline":
		b.draft += "\n"
	}
	return contract.KeyboardPressResult{Pressed: true, Key: "Enter"}, nil
}

// TestEnterFirstRequiresPositiveUnsentEvidence 验证成功、静默失败、传输中断、换人及新来信不会导致重复点击。
func TestEnterFirstRequiresPositiveUnsentEvidence(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		keys, buttons, inputs int
		ok, superseded        bool
	}{
		{"sent", 1, 0, 1, true, false}, {"not-started", 1, 1, 1, true, false}, {"newline-mode", 1, 1, 2, true, false},
		{"ambiguous-newline", 1, 0, 1, false, false}, {"silent", 1, 0, 1, false, false}, {"lost-ack", 1, 0, 1, false, false}, {"already-sent", 1, 0, 1, true, false},
		{"changed-before", 0, 0, 1, false, true}, {"changed-fallback", 1, 0, 1, false, true}, {"wrong-person", 0, 0, 1, false, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			b := &enterFirstBrowser{mode: tc.mode}
			cfg := model.Config{Selectors: map[string]contract.SelectorSpec{"message.input": testSelector("消息输入框"), "message.send": testSelector("发送按钮")}}
			inspect := func() (model.ConversationMessage, error) {
				switch {
				case tc.mode == "wrong-person":
					return model.ConversationMessage{}, errors.New("候选人已切换")
				case tc.mode == "changed-before" || (tc.mode == "changed-fallback" && b.keys > 0):
					return model.ConversationMessage{Direction: "candidate", Key: "new-inbound"}, nil
				case tc.mode == "already-sent" && b.keys > 0:
					return model.ConversationMessage{Direction: "self", Key: "new-outbound", TextContent: "你好"}, nil
				default:
					return model.ConversationMessage{Direction: "candidate", Key: "anchor"}, nil
				}
			}
			err := SendAutoReplyEnterFirst(context.Background(), b, cfg, "page", "你好", "anchor", inspect)
			if (err == nil) != tc.ok || errors.Is(err, model.ErrAutoReplySupersededBeforeSend) != tc.superseded || b.keys != tc.keys || b.buttons != tc.buttons || b.inputs != tc.inputs {
				t.Fatalf("err=%v keys=%d buttons=%d inputs=%d", err, b.keys, b.buttons, b.inputs)
			}
		})
	}
}
