// Package cloud 本文件验证目标版本、追问原子记账、新证据恢复与人工接管保护，不接触用户数据库。
package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// goalTestFixture 建立当前候选人、真实来信及一个尚未询问的 AI 沟通目标。
func goalTestFixture(t *testing.T) (*Client, AgentCredentials, AutoReplyConversation, CandidateConfirmationItem) {
	t.Helper()
	c, cred, _ := entityTestClient(t)
	ctx := context.Background()
	conv := entityTestConversation(t, c, cred)
	if _, err := c.EnsureLocalAutoReplyCandidate(ctx, cred, conv); err != nil {
		t.Fatal(err)
	}
	syncEntityMessages(t, c, cred, conv, AutoReplyMessage{Direction: "candidate", PlatformMessageID: "m1", TextContent: "我有兴趣，简历已发"})
	conv, _, _ = c.AutoReplyAnswerConversation(ctx, cred, conv.ID)
	goal, err := c.SaveAutoReplyConfirmationItem(ctx, cred, CandidateConfirmationItem{ID: "goal", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: conv.PositionID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "方便通话的时间", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	return c, cred, conv, goal
}

// TestGoalReceiptAndAskCountAreAtomicAndIdempotent 验证没有真实回执不计次数，实际出站确认只累计一次且同问题不再重发。
func TestGoalReceiptAndAskCountAreAtomicAndIdempotent(t *testing.T) {
	c, cred, conv, goal := goalTestFixture(t)
	ctx := context.Background()
	binding := &AutoReplyGoalSend{ConfirmationItemID: goal.ID, ExpectedVersion: AutoReplyConfirmationRevision(goal), BasedOnMessageKey: "m1", ExpectedLatestKey: "m1"}
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "方便什么时间通话？", 2, binding); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := c.FinishAutoReplySend(ctx, cred, conv.ID, "m1", 2, "sent"); err == nil {
		t.Fatal("无实际出站键也计为已问")
	}
	items, _ := c.AutoReplyConfirmationItems(ctx, cred, conv.ID)
	if items[0].AskCount != 0 {
		t.Fatal("失败事务泄露了提问计数")
	}
	for i := 0; i < 2; i++ {
		if err := c.FinishAutoReplySend(ctx, cred, conv.ID, "m1", 2, "sent", "out-1"); err != nil {
			t.Fatal(err)
		}
	}
	items, _ = c.AutoReplyConfirmationItems(ctx, cred, conv.ID)
	asked := items[0]
	if asked.AskCount != 1 || asked.LastAskedAt == nil || asked.LastAskedContentKey != AutoReplyGoalContentKey(goal) || AutoReplyGoalCanAsk(asked) {
		t.Fatalf("回执计数错误 %+v", asked)
	}
	syncEntityMessages(t, c, cred, conv, AutoReplyMessage{Direction: "candidate", PlatformMessageID: "m2", TextContent: "公司在哪里？"})
	binding.ExpectedVersion, binding.BasedOnMessageKey, binding.ExpectedLatestKey = AutoReplyConfirmationRevision(asked), "m2", "m2"
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m2", "再问下方便什么时间通话？", 2, binding); !errors.Is(err, ErrAutoReplyGoalChanged) || claimed {
		t.Fatal("换一轮来信后又催同问题", claimed, err)
	}
}

// TestGoalProposalUsesReadVersionAndKeepsHRConstraints 验证异步证据更新后的旧提议不能覆盖新目标，整批修改不会半份落库。
func TestGoalProposalUsesReadVersionAndKeepsHRConstraints(t *testing.T) {
	c, cred, conv, goal := goalTestFixture(t)
	ctx := context.Background()
	oldTime := goal.UpdatedAt
	stale := goal
	stale.AIProposal = true
	stale.ExpectedUpdatedAt = &oldTime
	stale.Content = "旧模型想覆盖的内容"
	goal.Content = "已更新为候选人平日方便通话时间"
	if _, err := c.SaveAutoReplyConfirmationItem(ctx, cred, goal); err != nil {
		t.Fatal(err)
	}
	newGoal := CandidateConfirmationItem{ID: "new", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: conv.PositionID, SourceType: "ai", CreatedByKind: "ai", ItemType: "confirm", Content: "另一个新目标", Status: "pending", AIProposal: true, RequireAbsent: true}
	if err := c.CommitAutoReplyProposal(ctx, cred, conv.ID, "m1", 1, []CandidateConfirmationItem{newGoal, stale}, nil); err == nil {
		t.Fatal("允许覆盖新版本目标")
	}
	items, _ := c.AutoReplyConfirmationItems(ctx, cred, conv.ID)
	if len(items) != 1 || items[0].Content != goal.Content {
		t.Fatal("旧提议留下了半份新目标", items)
	}
	hr, err := c.SaveAutoReplyConfirmationItem(ctx, cred, CandidateConfirmationItem{ID: "hr", ConversationID: conv.ID, CandidateID: conv.CandidateID, PositionID: conv.PositionID, SourceType: "position", PositionConditionID: "hr-location", ItemType: "required", Content: "可以在成都工作", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	hr.AIProposal = true
	hr.Content = "任意城市都行"
	if err = c.CommitAutoReplyProposal(ctx, cred, conv.ID, "m1", 1, []CandidateConfirmationItem{hr}, nil); err == nil {
		t.Fatal("服务器允许AI放宽HR条件")
	}
}

// queuedResumeGoalFixture 模拟新简历证据完成，并把一次追问与补审在同一事务提交。
func queuedResumeGoalFixture(t *testing.T, outcome ...string) (*Client, AgentCredentials, AutoReplyConversation, CandidateConfirmationItem) {
	t.Helper()
	c, cred, conv, goal := goalTestFixture(t)
	ctx := context.Background()
	processed := "sent"
	if len(outcome) > 0 {
		processed = outcome[0]
	}
	if err := c.CompleteAutoReplyMessage(ctx, cred, conv.ID, "m1", processed); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, "m1", "简历收到了", 1); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := c.FinishAutoReplySend(ctx, cred, conv.ID, "m1", 1, "sent", "ack"); err != nil {
		t.Fatal(err)
	}
	syncEntityMessages(t, c, cred, conv, AutoReplyMessage{Direction: "self", PlatformMessageID: "ack", TextContent: "简历收到了"})
	if _, _, err := c.ClaimAutoReplyResumeRetry(ctx, cred, conv.ID, "resume-card"); err != nil {
		t.Fatal(err)
	}
	var candidate AutoReplyStoredCandidate
	if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		candidate = AutoReplyStoredCandidate{ID: conv.CandidateID, CandidateName: "合成候选人", ResumeStructured: true, ResumeSourceMessageID: "resume-card"}
		return tx.Put("candidates", candidate.ID, candidate)
	}); err != nil {
		t.Fatal(err)
	}
	draft := AutoReplyGoalFollowup{ConfirmationItemID: goal.ID, Content: "方便什么时间通话？", ExpectedContentKey: AutoReplyGoalContentKey(goal)}
	if err := c.CommitAutoReplyResumeProposal(ctx, cred, conv.ID, "m1", "resume-card", AutoReplyResumeRevision(candidate), 1, nil, draft); err != nil {
		t.Fatal(err)
	}
	conv, goal, valid, err := c.AutoReplyGoalFollowupState(ctx, cred, conv.ID)
	wantValid := processed != "manual"
	if err != nil || valid != wantValid || wantValid && conv.GoalFollowup == nil || conv.ResumeRetry != nil {
		t.Fatalf("新证据未产生可恢复的一次追问：%+v %v", conv, err)
	}
	return c, cred, conv, goal
}

// TestWaitingForHRDoesNotFreezeResumeGoal 区分等待HR补资料与HR已接管，前者收到新简历后仍可继续一个独立目标。
func TestWaitingForHRDoesNotFreezeResumeGoal(t *testing.T) {
	for _, outcome := range []string{"awaiting_hr", "manual"} {
		c, cred, conv, _ := queuedResumeGoalFixture(t, outcome)
		_, _, valid, err := c.AutoReplyGoalFollowupState(context.Background(), cred, conv.ID)
		if err != nil || valid != (outcome == "awaiting_hr") {
			t.Fatal("未区分等待HR补资料和已人工接管", outcome, valid, err)
		}
	}
}

// TestResumeGoalFollowupRecoversOnceWithoutNewInbound 验证没有新来信也能恢复新证据追问，确认送达后不再排队或重新调用模型。
func TestResumeGoalFollowupRecoversOnceWithoutNewInbound(t *testing.T) {
	c, cred, conv, goal := queuedResumeGoalFixture(t)
	ctx := context.Background()
	task := conv.GoalFollowup
	queue, err := c.ListPendingAutoReplyConversations(ctx, cred, conv.PlatformID, conv.PlatformAccountID)
	if err != nil || len(queue) != 1 {
		t.Fatal("无新消息的追问没有进入恢复队列", queue, err)
	}
	binding := &AutoReplyGoalSend{ConfirmationItemID: goal.ID, ExpectedVersion: task.ExpectedGoalVersion, BasedOnMessageKey: task.BasedOnMessageKey, ExpectedLatestKey: task.ExpectedLatestKey, AllowNoPending: true}
	key := "goal-event:" + task.EventKey
	if _, claimed, err := c.ClaimAutoReplySend(ctx, cred, conv.ID, key, task.Content, 1, binding); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err = c.FinishAutoReplySend(ctx, cred, conv.ID, key, 1, "sent", "goal-out"); err != nil {
		t.Fatal(err)
	}
	if err = c.FinishAutoReplyGoalFollowup(ctx, cred, conv.ID, task.EventKey); err != nil {
		t.Fatal(err)
	}
	queue, err = c.ListPendingAutoReplyConversations(ctx, cred, conv.PlatformID, conv.PlatformAccountID)
	if err != nil || len(queue) != 0 {
		t.Fatal("完成的一次事件反复排队", queue, err)
	}
	items, _ := c.AutoReplyConfirmationItems(ctx, cred, conv.ID)
	if items[0].AskCount != 1 {
		t.Fatal("追问次数错误")
	}
}

// TestQueuedGoalInvalidatedByNewMessageHRAndGoalChanges 验证所有过期追问都失效，而不是沿用旧目标或在人工发言后抢答。
func TestQueuedGoalInvalidatedByNewMessageHRAndGoalChanges(t *testing.T) {
	for _, mode := range []string{"candidate", "human", "edited", "archived", "manual", "resume_changed"} {
		t.Run(mode, func(t *testing.T) {
			c, cred, conv, goal := queuedResumeGoalFixture(t)
			ctx := context.Background()
			task := conv.GoalFollowup
			switch mode {
			case "candidate":
				syncEntityMessages(t, c, cred, conv, AutoReplyMessage{Direction: "candidate", PlatformMessageID: "new", TextContent: "我又有一个问题"})
			case "human":
				syncEntityMessages(t, c, cred, conv, AutoReplyMessage{Direction: "self", PlatformMessageID: "hr-reply", TextContent: "我来处理这位候选人"})
			case "edited":
				goal.Content = "已改为只确认周末沟通时间"
				_, _ = c.SaveAutoReplyConfirmationItem(ctx, cred, goal)
			case "archived":
				now := time.Now().UTC()
				goal.ArchivedAt = &now
				_, _ = c.SaveAutoReplyConfirmationItem(ctx, cred, goal)
			case "manual":
				if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
					v, _, e := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conv.ID)
					if e != nil {
						return e
					}
					v.Status = "manual"
					return tx.Put("conversations", v.ID, v)
				}); err != nil {
					t.Fatal(err)
				}
			case "resume_changed":
				if err := c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
					v, _, e := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", conv.CandidateID)
					if e != nil {
						return e
					}
					v.CandidateName = "新版本资料"
					return tx.Put("candidates", v.ID, v)
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, valid, err := c.AutoReplyGoalFollowupState(ctx, cred, conv.ID); err != nil || valid {
				t.Fatal("过期追问仍可发送", mode, valid, err)
			}
			if err := c.FinishAutoReplyGoalFollowup(ctx, cred, conv.ID, task.EventKey); err != nil {
				t.Fatal(err)
			}
		})
	}
}
