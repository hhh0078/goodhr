// 本文件验证用户首次跑通招聘岗位运行的流程推导和卡点状态。
package httpapi

import (
	"testing"
	"time"
)

// TestUserFlowTransitionTable 验证八个节点依次完成后的当前阶段。
func TestUserFlowTransitionTable(t *testing.T) {
	state := defaultUserFlowState()
	for index, step := range userFlowStepOrder {
		var err error
		state, err = applyUserFlowUpdate(state, UserFlowUpdate{Step: step, Status: "completed", OccurredAt: time.Unix(int64(index+1), 0).UTC()})
		if err != nil {
			t.Fatalf("complete %s: %v", step, err)
		}
		if index < len(userFlowStepOrder)-1 && state.Stage != userFlowStepOrder[index+1] {
			t.Fatalf("after %s stage=%s", step, state.Stage)
		}
	}
	if state.State != "completed" || state.Stage != "completed" || state.CompletedAt == nil {
		t.Fatalf("unexpected completed flow: %+v", state)
	}
}

// TestUserFlowLaterCompletionInfersRequiredSteps 验证有确凿后续成功时自动补齐此前必经节点。
func TestUserFlowLaterCompletionInfersRequiredSteps(t *testing.T) {
	state, err := applyUserFlowUpdate(defaultUserFlowState(), UserFlowUpdate{Step: userFlowPositionStarted, Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range userFlowStepOrder[:5] {
		if state.Steps[step].Status != "completed" {
			t.Fatalf("step %s was not inferred: %+v", step, state)
		}
	}
	if state.Stage != userFlowFirstResumeProcessed {
		t.Fatalf("stage=%s", state.Stage)
	}
}

// TestUserFlowBlockedDoesNotInferEarlierSteps 验证失败事件只标记卡点，不伪造此前步骤成功。
func TestUserFlowBlockedDoesNotInferEarlierSteps(t *testing.T) {
	state, err := applyUserFlowUpdate(defaultUserFlowState(), UserFlowUpdate{Step: userFlowPositionStarted, Status: "blocked", ReasonCode: "runtime_missing", Message: "缺少组件"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != userFlowAgentDetected || state.State != "pending" {
		t.Fatalf("blocked later step moved current stage: %+v", state)
	}
	if state.Steps[userFlowPositionStarted].Status != "blocked" {
		t.Fatalf("blocked evidence missing: %+v", state)
	}
}

// TestUserFlowBlankStatusDefaultsCompleted 验证省略状态时按成功事件处理。
func TestUserFlowBlankStatusDefaultsCompleted(t *testing.T) {
	state, err := applyUserFlowUpdate(defaultUserFlowState(), UserFlowUpdate{Step: userFlowAgentDetected})
	if err != nil {
		t.Fatal(err)
	}
	if state.Steps[userFlowAgentDetected].Status != "completed" {
		t.Fatalf("blank status was not normalized: %+v", state)
	}
}

// TestUserFlowReconcilesFirstResumeFromPositionStats 验证岗位扫描统计可以补齐遗漏的首份简历节点。
func TestUserFlowReconcilesFirstResumeFromPositionStats(t *testing.T) {
	positions := NewMemoryPositionStore()
	position, err := positions.SavePosition(Position{UserEmail: "flow@example.com", Name: "测试岗位"})
	if err != nil {
		t.Fatal(err)
	}
	if err := positions.IncrementPositionCounts(position.ID, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	flows := NewMemoryUserFlowStore()
	service := NewUserFlowService(nil, flows, positions)
	state, err := service.reconcilePositionProgress("flow@example.com", defaultUserFlowState())
	if err != nil {
		t.Fatal(err)
	}
	if state.Steps[userFlowFirstResumeProcessed].Status != "completed" || state.Stage != userFlowFirstGreetSuccess {
		t.Fatalf("首份简历节点没有按岗位统计补齐：%+v", state)
	}
}

// TestUserFlowReconcilesGreetFromTodayCount 验证当前账号今日打招呼数可以一次补齐最后两个节点。
func TestUserFlowReconcilesGreetFromTodayCount(t *testing.T) {
	positions := NewMemoryPositionStore()
	position, err := positions.SavePosition(Position{UserEmail: "flow@example.com", Name: "测试岗位"})
	if err != nil {
		t.Fatal(err)
	}
	if err := positions.FinishPositionRun(position.ID, "completed", 1); err != nil {
		t.Fatal(err)
	}
	flows := NewMemoryUserFlowStore()
	service := NewUserFlowService(nil, flows, positions)
	state, err := service.reconcilePositionProgress("flow@example.com", defaultUserFlowState())
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != "completed" || state.Steps[userFlowFirstResumeProcessed].Status != "completed" ||
		state.Steps[userFlowFirstGreetSuccess].Status != "completed" {
		t.Fatalf("今日打招呼数没有补齐最后两个节点：%+v", state)
	}
}
