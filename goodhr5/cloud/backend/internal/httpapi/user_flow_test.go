// 本文件验证用户首次跑通招聘任务的流程推导和卡点状态。
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
	state, err := applyUserFlowUpdate(defaultUserFlowState(), UserFlowUpdate{Step: userFlowTaskStarted, Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range userFlowStepOrder[:6] {
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
	state, err := applyUserFlowUpdate(defaultUserFlowState(), UserFlowUpdate{Step: userFlowTaskStarted, Status: "blocked", ReasonCode: "runtime_missing", Message: "缺少组件"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != userFlowAgentDetected || state.State != "pending" {
		t.Fatalf("blocked later step moved current stage: %+v", state)
	}
	if state.Steps[userFlowTaskStarted].Status != "blocked" {
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
