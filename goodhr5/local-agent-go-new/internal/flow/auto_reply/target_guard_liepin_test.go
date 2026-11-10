// Package auto_reply 本文件用真实猎聘运行时和生产配置验证全部联系人扫描、已打开聊天和手动关窗后的单会话恢复，不连接招聘页面。
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

// targetLiepinBrowser 模拟全部联系人及当前聊天，所有请求仍经过真实平台运行时和选择器配置。
type targetLiepinBrowser struct {
	autoReplyBrowserStub
	cfg                   model.Config
	snapshot              model.AutoReplyConversationSnapshot
	opened                bool
	name, candidateID     string
	readErr               error
	scanErr               error
	contactClicks, scans  int
	nameReads, nameProbes int
}

// ListPages 提供本夹具唯一且已观测的当前招聘页，保留真实运行时对换页和多当前页的生产守卫。
func (b *targetLiepinBrowser) ListPages(context.Context) (contract.PageListResult, error) {
	return contract.PageListResult{Pages: []contract.PageInfo{{PageID: "target-liepin", URL: b.cfg.MessagesURL, Current: true}}, Count: 1}, nil
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
		b.nameReads++
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
	default:
		return contract.ReadResult{}, fmt.Errorf("测试未预期的读取：%s", b.key(request.Selector))
	}
}

// FindAll 提供真实运行时需要的联系人、标签和消息窗口，不对生产流程打桩。
func (b *targetLiepinBrowser) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	exists := false
	switch b.key(request.Selector) {
	case "message.current_name":
		b.nameProbes++
		if !request.ProbeOnly || !request.ExpectedMissing || request.Selector.TimeoutMS > 600 {
			return nil, fmt.Errorf("当前聊天应使用短预算存在性探测")
		}
		exists = b.opened
	case "message.drawer":
		exists = true
	case "message.all_tab_selected":
		exists = true
	case "message.gender_female", "message.gender_male":
		return nil, nil
	case "message.contact_item":
		// 身份定位只读取编号/姓名，扫描全部联系人还读取最新摘要。
		if _, full := request.Fields["last_message"]; full || (len(request.Fields) == 1 && request.MaxItems == b.cfg.MaxItems) {
			b.scans++
			if b.scanErr != nil {
				return nil, b.scanErr
			}
		}
		return []contract.FindAllItem{
			{Index: 0, TotalMatches: 2, Text: "别的联系人 有新消息", Fields: map[string]string{"name": "别的联系人", "thread_meta": `{"to_imid":"unrelated"}`, "unread_count": "3", "last_message": "有新消息"}},
			{Index: 1, TotalMatches: 2, Text: "测试目标 已回复", Fields: map[string]string{"name": b.snapshot.CandidateName, "thread_meta": `{"to_imid":"target-thread"}`, "last_message": "已回复"}},
		}, nil
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

// Click 验证只允许按稳定编号打开目标；点击任何标签都会使测试失败。
func (b *targetLiepinBrowser) Click(_ context.Context, request contract.ElementClickRequest) (contract.ClickResult, error) {
	switch b.key(request.Selector) {
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

// TestTargetLiepinRuntimeAllScanKeepsChatAndRecoversManualClose 验证全部标签扫描保持已打开聊天，手动关窗后才按稳定身份重新打开。
func TestTargetLiepinRuntimeAllScanKeepsChatAndRecoversManualClose(t *testing.T) {
	runtime, guarded, browser, _ := newTargetLiepinRuntime(t)
	ctx := context.Background()
	if _, err := runtime.ScanUnreadConversations(ctx, browser, browser.cfg); err != nil {
		t.Fatal(err)
	}
	if latest, open, err := guarded.ReadOpenAutoReplyLatestMessage(ctx, browser, browser.cfg, browser.snapshot); err != nil || !open || latest.PlatformMessageID != "last-self" || browser.scans != 1 || browser.contactClicks != 0 {
		t.Fatalf("读全部联系人关窗或改变了消息：open=%t latest=%+v scans=%d clicks=%d err=%v", open, latest, browser.scans, browser.contactClicks, err)
	}
	browser.opened = false
	nameReadsBeforeClose := browser.nameReads
	if _, open, err := guarded.ReadOpenAutoReplyLatestMessage(ctx, browser, browser.cfg, browser.snapshot); err != nil || open || browser.opened || browser.contactClicks != 0 || browser.nameReads != nameReadsBeforeClose {
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

// TestTargetLiepinCheckpointRestoresOnlyAuthorizedTarget 验证全部联系人扫描、手动关窗和读取失败时仍只恢复指定候选人。
func TestTargetLiepinCheckpointRestoresOnlyAuthorizedTarget(t *testing.T) {
	for _, scenario := range []string{"保持打开", "手动关窗", "读取失败"} {
		t.Run(scenario, func(t *testing.T) {
			readFailure := scenario == "读取失败"
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
			if scenario == "手动关窗" {
				browser.opened = false
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
			wantOpens := 0
			if scenario == "手动关窗" {
				wantOpens = 1
			}
			if err != nil || processed != wantProcessed || browser.scans != 1 || browser.contactClicks != wantOpens || stats.Failed != 0 || current == nil {
				t.Fatalf("扫描后旧快照恢复不正确：processed=%d scans=%d opens=%d failed=%d err=%v", processed, browser.scans, browser.contactClicks, stats.Failed, err)
			}
			state, err := flow.Cloud.AutoReplyCandidateState(ctx, cred, cloud.AutoReplyCandidateLookup{PlatformID: prepared.Platform.ID, PlatformAccountID: prepared.Request.PlatformAccountID, PlatformThreadID: target.PlatformThreadID})
			if err != nil || state.Conversation == nil || (state.Conversation.CaptureIntent != nil) != readFailure {
				t.Fatalf("待办完成状态不正确：state=%+v err=%v", state.Conversation, err)
			}
		})
	}
}

// TestAllContactEmptyObservationDoesNotStartHistoryBoundary 验证空抽屉不提前建立监听边界；只有实际看到的一份列表才能供下次对比。
func TestAllContactEmptyObservationDoesNotStartHistoryBoundary(t *testing.T) {
	ctx := context.Background()
	flow, prepared := newReliabilityFlow(t, "")
	prepared.Platform.ID = "liepin"
	rows, _, err := flow.reviewAllContacts(ctx, prepared, nil)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	row := model.Conversation{Key: "thread", PlatformThreadID: "thread", Name: "已存在的联系人", Summary: "旧内容", Fields: map[string]string{"contact_list_signature": "old"}}
	rows, _, err = flow.reviewAllContacts(ctx, prepared, []model.Conversation{row})
	if err != nil || len(rows) != 0 {
		t.Fatal("渲染后的第一份历史列表被误判成新进线", rows, err)
	}
	row.Fields["contact_list_signature"] = "changed"
	row.Summary = "新来信"
	rows, _, err = flow.reviewAllContacts(ctx, prepared, []model.Conversation{row})
	if err != nil || len(rows) != 1 || rows[0].PlatformThreadID != "thread" {
		t.Fatal("确认基线后无红点的新来信没有被发现", rows, err)
	}
}

// TestDueListReviewKeepsItsCheckpointSlot 验证轮查与本地待办重合时保留详情，普通轮次维持待办顺序。
func TestDueListReviewKeepsItsCheckpointSlot(t *testing.T) {
	queue := []model.Conversation{{PlatformThreadID: "pending"}, {PlatformThreadID: "quiet", PlatformCandidateID: "已核对身份"}, {PlatformThreadID: "other"}}
	reviews := map[string]cloud.AutoReplyListReview{"quiet": {ThreadID: "quiet"}}
	prioritizeDueListReview(queue, reviews)
	if queue[0].PlatformThreadID != "pending" {
		t.Fatal("普通轮次占用了本地名额")
	}
	reviews["quiet"] = cloud.AutoReplyListReview{ThreadID: "quiet", ReserveTurn: true}
	prioritizeDueListReview(queue, reviews)
	if queue[0].PlatformCandidateID != "已核对身份" || queue[1].PlatformThreadID != "pending" || queue[2].PlatformThreadID != "other" {
		t.Fatal("久未核对的会话或其它本地待办被挤掉", queue)
	}
}
