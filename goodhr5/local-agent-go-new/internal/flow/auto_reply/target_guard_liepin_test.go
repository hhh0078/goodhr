// Package auto_reply 本文件用真实猎聘运行时和生产配置验证切换未读标签后的单会话恢复，不连接招聘页面。
package auto_reply

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// targetLiepinBrowser 模拟未读标签收起已读聊天，所有请求仍经过真实平台运行时和选择器配置。
type targetLiepinBrowser struct {
	autoReplyBrowserStub
	cfg                  model.Config
	snapshot             model.AutoReplyConversationSnapshot
	opened, unread       bool
	name, candidateID    string
	readErr              error
	scanErr              error
	contactClicks, scans int
}

// key 把生产选择器映射回逻辑键，避免在测试内复制平台CSS。
func (b *targetLiepinBrowser) key(selector contract.SelectorSpec) string {
	for key, configured := range b.cfg.Selectors {
		if selector.Description == configured.Description {
			return key
		}
	}
	return ""
}

// Read 模拟当前聊天姓名、稳定编号和岗位，明确不存在与控制通道错误分别返回。
func (b *targetLiepinBrowser) Read(_ context.Context, request contract.ElementReadRequest) (contract.ReadResult, error) {
	switch b.key(request.Selector) {
	case "message.current_name":
		if b.readErr != nil {
			return contract.ReadResult{}, b.readErr
		}
		if !b.opened {
			return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
		}
		return contract.ReadResult{Value: b.name}, nil
	case "message.current_position":
		return contract.ReadResult{Value: b.snapshot.CommunicationPosition}, nil
	case "message.candidate_id_source":
		return contract.ReadResult{Value: "cid=" + b.candidateID}, nil
	case "message.entry_unread_count":
		return contract.ReadResult{}, &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "ELEMENT_NOT_FOUND"}}
	default:
		return contract.ReadResult{}, fmt.Errorf("测试未预期的读取：%s", b.key(request.Selector))
	}
}

// FindAll 提供真实运行时需要的联系人、标签和消息窗口，不对生产流程打桩。
func (b *targetLiepinBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	exists := false
	switch b.key(request.Selector) {
	case "message.drawer":
		exists = true
	case "message.all_tab_selected":
		exists = !b.unread
	case "message.unread_tab_selected":
		exists = b.unread
	case "message.unread_item", "message.gender_female", "message.gender_male":
		return nil, nil
	case "message.contact_item":
		return []contract.FindAllItem{{Index: 3, Fields: map[string]string{"name": b.snapshot.CandidateName, "thread_meta": `{"to_imid":"target-thread"}`}}}, nil
	case "message.item":
		if !b.opened {
			return nil, fmt.Errorf("禁止用已收起的聊天快照读取消息")
		}
		return []contract.FindAllItem{{Index: 0, TotalMatches: 1, Text: "已回复", Fields: map[string]string{"message_meta": `{"message_id":"last-self"}`, "message_text": "已回复", "body_class": "message-item-send"}}}, nil
	default:
		return nil, fmt.Errorf("测试未预期的查找：%s", b.key(request.Selector))
	}
	if exists {
		return []contract.FindAllItem{{Index: 0}}, nil
	}
	return nil, nil
}

// Click 仅记录标签和精确目标点击；切换未读标签复现现场已读聊天消失的状态。
func (b *targetLiepinBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	switch b.key(request.Selector) {
	case "message.unread_tab":
		b.unread, b.opened = true, false
		b.scans++
		b.readErr = b.scanErr
	case "message.all_tab":
		b.unread = false
	case "message.contact_click_target":
		matched := false
		for _, parent := range request.Selector.Parents {
			if parent.Attributes[b.cfg.ConversationFields["thread_meta"].ReadAttribute] == `{"to_imid":"target-thread"}` && parent.Index == nil {
				matched = true
			}
		}
		if !matched {
			return contract.ClickResult{}, fmt.Errorf("打开目标未携带精确会话属性")
		}
		b.contactClicks++
		b.opened = true
		b.name, b.candidateID = b.snapshot.CandidateName, b.snapshot.PlatformCandidateID
	default:
		return contract.ClickResult{}, fmt.Errorf("测试禁止点击：%s", b.key(request.Selector))
	}
	return contract.ClickResult{Clicked: true}, nil
}

// Scroll 仅返回合成可见位置，不执行真实滚轮。
func (b *targetLiepinBrowser) Scroll(context.Context, contract.ScrollRequest) (contract.ScrollResult, error) {
	visible, moved := true, false
	return contract.ScrollResult{ProgressTargetVisible: &visible, ProgressTargetMoved: &moved}, nil
}

// newTargetLiepinRuntime 使用正式注册入口和配置组装真实平台运行时及目标守卫。
func newTargetLiepinRuntime(t *testing.T) (model.Runtime, model.AutoReplyRuntime, *targetLiepinBrowser, shared.AutoReplyTarget) {
	t.Helper()
	target, snapshot := targetFixture()
	cfg, err := platform.LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.RuntimeFor("liepin")
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := restrictAutoReplyRuntime(runtime.(model.AutoReplyRuntime), &target)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, guarded, &targetLiepinBrowser{cfg: cfg, snapshot: snapshot, opened: true, name: target.CandidateName, candidateID: target.PlatformCandidateID}, target
}

// TestTargetLiepinRuntimeRecoversAfterUnreadTab 验证真实扫描收起聊天后能按目标重新打开，之后仍按稳定编号读取。
func TestTargetLiepinRuntimeRecoversAfterUnreadTab(t *testing.T) {
	runtime, guarded, browser, _ := newTargetLiepinRuntime(t)
	ctx := context.Background()
	if _, err := runtime.ScanUnreadConversations(ctx, browser, browser.cfg); err != nil {
		t.Fatal(err)
	}
	if _, open, err := guarded.ReadOpenAutoReplyLatestMessage(ctx, browser, browser.cfg, browser.snapshot); err != nil || open || browser.opened || browser.contactClicks != 0 {
		t.Fatalf("已收起聊天没有按关闭处理：open=%v err=%v clicks=%d", open, err, browser.contactClicks)
	}
	snapshot, err := guarded.OpenAutoReplyConversation(ctx, browser, browser.cfg, browser.snapshot.Conversation, []string{"last-self"}, 5000)
	if err != nil || browser.contactClicks != 1 || snapshot.PlatformCandidateID != browser.snapshot.PlatformCandidateID {
		t.Fatalf("未能安全恢复精确目标：snapshot=%+v err=%v clicks=%d", snapshot, err, browser.contactClicks)
	}
	if latest, open, err := guarded.ReadOpenAutoReplyLatestMessage(ctx, browser, browser.cfg, snapshot); err != nil || !open || latest.PlatformMessageID != "last-self" {
		t.Fatalf("恢复后未能继续读取：latest=%+v open=%v err=%v", latest, open, err)
	}
}

// TestTargetLiepinRuntimeOnlyMissingChatAllowsRecovery 验证错名、同名错编号、空姓名、缺配置和浏览器错误均不能伪装成关闭。
func TestTargetLiepinRuntimeOnlyMissingChatAllowsRecovery(t *testing.T) {
	for _, scenario := range []string{"wrong-name", "wrong-id", "blank-name", "missing-selector", "read-failure", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			_, guarded, browser, _ := newTargetLiepinRuntime(t)
			switch scenario {
			case "wrong-name":
				browser.name = "其他候选人"
			case "wrong-id":
				browser.candidateID = "other-candidate"
			case "blank-name":
				browser.name = ""
			case "missing-selector":
				delete(browser.cfg.Selectors, "message.current_name")
			case "read-failure":
				browser.readErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "BROWSER_OPERATION_TIMEOUT"}}
			case "canceled":
				browser.readErr = context.Canceled
			}
			if _, open, err := guarded.ReadOpenAutoReplyLatestMessage(context.Background(), browser, browser.cfg, browser.snapshot); err == nil || open || errors.Is(err, model.ErrAutoReplyConversationNotOpen) || browser.contactClicks != 0 {
				t.Fatalf("不能确认身份的状态被当作关闭：open=%v err=%v clicks=%d", open, err, browser.contactClicks)
			}
		})
	}
}

// TestTargetLiepinCheckpointRechecksSnapshotAfterUnreadScan 验证同一轮扫描收起聊天后，持久采集待办不会复用失效快照。
func TestTargetLiepinCheckpointRechecksSnapshotAfterUnreadScan(t *testing.T) {
	for _, readFailure := range []bool{false, true} {
		t.Run(fmt.Sprintf("扫描后读取故障=%v", readFailure), func(t *testing.T) {
			flow, prepared := newReliabilityFlow(t, "")
			runtime, guarded, browser, target := newTargetLiepinRuntime(t)
			flow.Browser = browser
			prepared.Platform = browser.cfg
			prepared.Request.AutoReplyTarget = &target
			prepared.Position.ID = "position"
			ctx := context.Background()
			cred := credentials(prepared)
			conversation, err := flow.Cloud.SaveAutoReplyConversation(ctx, cred, cloud.AutoReplyConversation{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: target.PlatformThreadID, CandidateName: target.CandidateName, PositionID: "position", PagePositionText: "工程师"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = flow.Cloud.SyncAutoReplyMessages(ctx, cred, cloud.AutoReplyMessageSyncRequest{ConversationID: conversation.ID, Messages: []cloud.AutoReplyMessage{{PlatformMessageID: "last-self", Direction: "self", MessageType: "text", TextContent: "已回复"}}}); err != nil {
				t.Fatal(err)
			}
			if err = flow.Cloud.BeginAutoReplyCapture(ctx, cred, prepared.Platform.ID, prepared.Request.PlatformAccountID, target.PlatformThreadID, cloud.AutoReplyCaptureIntent{PositionID: "position", PlatformThreadID: target.PlatformThreadID, CandidateName: target.CandidateName, PagePositionText: "工程师"}); err != nil {
				t.Fatal(err)
			}
			if readFailure {
				browser.scanErr = &contract.WorkerError{Body: contract.WorkerErrorBody{Code: "BROWSER_OPERATION_TIMEOUT"}}
			}
			current := &openAutoReplyConversation{conversation: browser.snapshot.Conversation, snapshot: browser.snapshot}
			stats := &shared.Stats{}
			positions := []cloud.AutoReplyPositionSnapshot{{Position: cloud.AutoReplyPosition{ID: "position", Name: "工程师"}}}
			processed, current, err := flow.processCheckpoint(ctx, prepared, runtime, guarded, positions, 1, stats, &shared.ConsecutiveErrorPolicy{}, current)
			wantProcessed := 1
			if readFailure {
				wantProcessed = 0
			}
			if err != nil || processed != wantProcessed || browser.scans != 1 || browser.contactClicks != wantProcessed || stats.Failed != 0 || current == nil {
				t.Fatalf("扫描后旧快照恢复不正确：processed=%d scans=%d opens=%d failed=%d err=%v", processed, browser.scans, browser.contactClicks, stats.Failed, err)
			}
			state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: target.PlatformThreadID})
			if err != nil || state.Conversation == nil || (state.Conversation.CaptureIntent != nil) != readFailure {
				t.Fatalf("待办完成状态不正确：state=%+v err=%v", state.Conversation, err)
			}
		})
	}
}
