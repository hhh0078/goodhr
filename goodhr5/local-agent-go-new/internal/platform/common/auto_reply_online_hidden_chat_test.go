// Package common 本文件验证在线DOM弹层隐藏聊天时的本轮打开凭据、精确姓名和关闭后原生身份保护，不操作真实平台。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// hiddenChatProfileBrowser 模拟Worker错误响应不带ClickResult，只通过明确的clicked字段证明已经执行点击。
type hiddenChatProfileBrowser struct {
	*onlineLifecycleBrowser
	zeroClickResult bool
}

// Click 保留页面实际变化，但按真实Worker错误协议清空成功结果。
func (b *hiddenChatProfileBrowser) Click(ctx context.Context, req contract.ElementClickRequest) (contract.ClickResult, error) {
	result, err := b.onlineLifecycleBrowser.Click(ctx, req)
	if b.zeroClickResult && req.Selector.Description == "entry" {
		result = contract.ClickResult{}
	}
	return result, err
}

// TestOnlineProfileHiddenConversationLifecycle 验证仅本轮已核实入口实际打开的DOM层可临时缺少聊天，旧层、canvas和未知点击均不放行。
func TestOnlineProfileHiddenConversationLifecycle(t *testing.T) {
	for _, scenario := range []string{
		"opened-dom", "opened-dom-fragment", "escape-fallback", "preexisting-same", "preexisting-other",
		"not-configured", "canvas-not-relaxed", "other-name", "partial-name", "changed-page", "changed-query",
		"worker-verify-failure", "unknown-click", "cancel-after-click", "restore-failure", "restored-wrong-native-id",
	} {
		t.Run(scenario, func(t *testing.T) {
			platform := "liepin"
			if scenario == "canvas-not-relaxed" {
				platform = "boss"
			}
			base, cfg, snapshot := onlineProfileFixture(platform)
			cfg.Behavior.AutoReplyOnlineProfileHidesConversation = scenario != "not-configured"
			cfg.Behavior.AutoReplyOnlineProfileFragment = "preview"
			b := &hiddenChatProfileBrowser{onlineLifecycleBrowser: &onlineLifecycleBrowser{onlineProfileTestBrowser: base, page: cfg.MessagesURL, chatRight: true}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("本轮入口或恢复失败")
			switch scenario {
			case "opened-dom-fragment", "not-configured":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.page += "#preview" }
			case "escape-fallback":
				b.ignoreEscape = true
			case "preexisting-same", "preexisting-other":
				b.open, b.panelName = true, snapshot.CandidateName
				if scenario == "preexisting-other" {
					b.panelName = "另一位候选人"
				}
			case "other-name":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.panelName = "另一位候选人" }
			case "partial-name":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.panelName = "目标" }
			case "changed-page":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.page = "https://other.test/chat#preview" }
			case "changed-query":
				b.afterEntry = func(b *onlineLifecycleBrowser) { b.page += "?candidate=other#preview" }
			case "worker-verify-failure":
				b.zeroClickResult = true
				b.entryError = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_VERIFICATION_FAILED", Step: "verify_click", Details: json.RawMessage(`{"clicked":true,"click_started":true}`)}}
				cause = b.entryError
			case "unknown-click":
				b.zeroClickResult, b.entryError = true, cause
			case "cancel-after-click":
				b.afterEntry = func(*onlineLifecycleBrowser) { cancel() }
				b.entryError, cause = context.Canceled, context.Canceled
			}
			verify := func(check context.Context) error {
				if err := check.Err(); err != nil {
					return err
				}
				if b.open || b.keys > 0 && b.restores == 0 {
					return model.ErrAutoReplyConversationNotOpen
				}
				if scenario == "restored-wrong-native-id" && b.restores > 0 {
					return cause
				}
				return nil
			}
			restore := func(context.Context) error {
				b.restores++
				if scenario == "restore-failure" {
					return cause
				}
				return nil
			}
			profile, err := ReadConfiguredAutoReplyOnlineProfile(ctx, b, cfg, snapshot, verify, restore, nil)
			switch scenario {
			case "opened-dom", "opened-dom-fragment", "escape-fallback":
				if err != nil || profile.Mode != "dom" || profile.RawText == "" || b.open || b.entryClicks != 1 || b.bodyReads != 1 || b.keys != 1 || b.restores != 1 {
					t.Fatalf("本轮DOM弹层没有安全读取并恢复：err=%v entry=%d body=%d keys=%d restore=%d", err, b.entryClicks, b.bodyReads, b.keys, b.restores)
				}
				if scenario == "escape-fallback" && b.closeClicks != 1 {
					t.Fatal("Esc未生效时没有按已核实备用按钮关闭")
				}
			case "worker-verify-failure", "cancel-after-click":
				if !errors.Is(err, cause) || b.open || b.bodyReads != 0 || b.entryClicks != 1 || b.keys != 1 || b.restores != 1 {
					t.Fatalf("实际点击后失败没有保留原因并收尾：err=%v open=%v keys=%d restore=%d", err, b.open, b.keys, b.restores)
				}
			case "restore-failure", "restored-wrong-native-id":
				if !errors.Is(err, cause) || b.open || b.bodyReads != 1 || b.restores != 1 || b.keys != 1 {
					t.Fatalf("关闭后原生身份恢复错误被放过：err=%v open=%v restore=%d", err, b.open, b.restores)
				}
			default:
				if err == nil || !b.open || b.bodyReads != 0 || b.screenshots != 0 || b.keys != 0 || b.closeClicks != 0 || b.restores != 0 {
					t.Fatalf("未知归属或禁用例外仍操作资料：err=%v body=%d shots=%d keys=%d close=%d restore=%d", err, b.bodyReads, b.screenshots, b.keys, b.closeClicks, b.restores)
				}
				if (scenario == "preexisting-same" || scenario == "preexisting-other") && b.entryClicks != 0 {
					t.Fatal("预存且原聊天不可核实的弹层被重新点击")
				}
			}
		})
	}
}

// TestOnlineProfileClickProofRequiresCompletedClick 验证开始按鼠标、未知超时和错误格式不能冒充已点击回执。
func TestOnlineProfileClickProofRequiresCompletedClick(t *testing.T) {
	for _, details := range []string{`{"click_started":true}`, `{"clicked":false}`, `{"clicked":"true"}`, `invalid`} {
		err := &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "CLICK_FAILED", Details: json.RawMessage(details)}}
		if onlineProfileEntryClickCompleted(err) {
			t.Fatalf("不完整点击证据被接受：%s", details)
		}
	}
	if onlineProfileEntryClickCompleted(&contract.WorkerError{Body: contract.WorkerErrorBody{Code: "WORKER_UNRESPONSIVE", Details: json.RawMessage(`{"clicked":true}`)}}) {
		t.Fatal("浏览器已不可操作仍接受点击清理凭据")
	}
}
