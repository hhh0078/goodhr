// Package common 本文件实现带焦点和会话核对的回车优先发送；仅在可以证明回车没有发送时才使用按钮。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// SendAutoReplyEnterFirst 在输入完成后核对触发来信、焦点和草稿；传输中断或结果不明时交给原发送账本复核。
func SendAutoReplyEnterFirst(ctx context.Context, browser model.Browser, cfg model.Config, pageURL, message, anchor string, inspect func() (model.ConversationMessage, error)) error {
	if strings.TrimSpace(message) == "" || pageURL == "" || inspect == nil {
		return errors.New("消息、所属页面或会话核验不完整，暂不发送")
	}
	input, err := RequiredSelector(cfg, "message.input")
	if err != nil {
		return err
	}
	if anchor == "" {
		before, readErr := inspect()
		if readErr != nil {
			return readErr
		}
		anchor = firstNonEmpty(before.PlatformMessageID, before.Key)
		if anchor == "" {
			return errors.New("当前会话没有可核对的消息编号，暂不发送")
		}
	}
	// 输入本轮已准备好的完整草稿。Worker 保留真实聚焦、输入后回读与正常的按键发送守卫。
	writeDraft := func() error {
		_, err := browser.Input(ctx, contract.ElementInputRequest{Selector: input, Text: message, InsertText: true})
		return err
	}
	if err = writeDraft(); err != nil {
		return err
	}
	readDraft := func() (string, error) {
		draft, readErr := browser.Read(ctx, contract.ElementReadRequest{Selector: input, Property: "value"})
		if readErr == nil && draft.Property != "value" {
			return "", errors.New("浏览器组件暂不能核对输入框的实际草稿，请更新后重试")
		}
		return draft.Value, readErr
	}
	checkAnchor := func() error {
		latest, readErr := inspect()
		if readErr != nil {
			return readErr
		}
		if firstNonEmpty(latest.PlatformMessageID, latest.Key) != anchor {
			if latest.Direction == "candidate" && firstNonEmpty(latest.PlatformMessageID, latest.Key) != "" {
				return model.ErrAutoReplySupersededBeforeSend
			}
			return errors.New("聊天已有新消息，这次回复先放下")
		}
		return nil
	}
	if err = checkAnchor(); err != nil {
		return err
	}
	draft, err := readDraft()
	if err != nil {
		return err
	}
	if draft != message {
		return errors.New("输入框草稿已变化，暂不发送")
	}
	key, keyErr := browser.PressKey(ctx, contract.KeyboardPressRequest{Key: "Enter", DelayMS: 80, Description: "发送已核对的回复",
		ExpectedPageURL: pageURL, Target: &input, RequireFocus: true, ExpectedValue: &message})
	if ctx.Err() != nil || contract.IsSessionUnavailable(keyErr) {
		return errors.Join(ctx.Err(), keyErr)
	}
	// 只有明确属于输入前的错误才允许按钮直接接手；普通超时、响应丢失和未知阶段都不具备这个证据。
	notStarted := messageKeyNotStarted(keyErr)
	for attempt := 0; attempt < 3; attempt++ {
		draft, err = readDraft()
		if err != nil {
			return err
		}
		if draft == "" && keyErr == nil && key.Pressed {
			return nil // 外层必须再读到真实的新发气泡，才能将发送账本写为 sent。
		}
		latest, readErr := inspect()
		if readErr != nil {
			return readErr
		}
		latestKey := firstNonEmpty(latest.PlatformMessageID, latest.Key)
		if latestKey == "" {
			return errors.New("没能核对发送后的聊天编号，暂不补发")
		}
		if latestKey != anchor {
			if notStarted && latest.Direction == "candidate" {
				return model.ErrAutoReplySupersededBeforeSend
			}
			if latest.Direction == "self" && latest.TextContent == message {
				return nil
			}
			return errors.New("聊天内容已变化，暂不补发")
		}
		if draft == message && notStarted {
			break
		}
		if keyErr == nil && key.Pressed && (draft == message+"\n" || draft == message+"\r\n") {
			hint, hintErr := browser.Read(ctx, contract.ElementReadRequest{Selector: input, Attribute: "placeholder"})
			if hintErr != nil {
				return hintErr
			}
			shortcut := strings.ToLower(strings.Join(strings.Fields(hint.Value), ""))
			if strings.Contains(shortcut, "enter换行") && strings.Contains(shortcut, "ctrl+enter发送") {
				if err = writeDraft(); err != nil {
					return err
				}
				break
			}
		}
		if keyErr != nil || attempt == 2 || draft != message {
			return errors.Join(keyErr, errors.New("回车的发送结果尚未确认，为避免重复，这次没有再点按钮"))
		}
		if err = waitConversationPoll(ctx); err != nil {
			return err
		}
	}
	draft, err = readDraft()
	if err != nil || draft != message {
		return errors.Join(err, errors.New("备用发送前草稿已变化，暂不发送"))
	}
	if err = checkAnchor(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	button, err := RequiredSelector(cfg, "message.send")
	if err != nil {
		return err
	}
	clicked, err := browser.Click(ctx, contract.ElementClickRequest{Selector: button})
	if err != nil || !clicked.Clicked {
		return errors.Join(err, fmt.Errorf("发送按钮结果没有确认，暂不重发"))
	}
	return nil
}

// messageKeyNotStarted 只采信 Worker 明示尚未调用键盘的回执，取消或通讯超时保留未知。
func messageKeyNotStarted(err error) bool {
	var e *contract.WorkerError
	if !errors.As(err, &e) || contract.IsSessionUnavailable(err) || e.Body.Code == "ACTION_TIMEOUT" || e.Body.Code == "ACTION_CANCELLED" {
		return false
	}
	var details struct {
		Started *bool `json:"key_started"`
	}
	return json.Unmarshal(e.Body.Details, &details) == nil && details.Started != nil && !*details.Started
}
