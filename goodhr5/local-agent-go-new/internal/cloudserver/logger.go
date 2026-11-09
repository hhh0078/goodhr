// Package cloudserver 本文件记录有来源的设备流程/AI阶段，候选人事件仅按真实会话编号关联。
package cloudserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/failurediag"
)

type runLogger struct {
	mu                     sync.Mutex
	run                    *activeRun
	conversation, position string
	analysis               shared.AnalysisStatus
}

// BindAutoReplyConversation 只有当前流程已保存并确认会话归属时才设置候选人事件范围。
func (l *runLogger) BindAutoReplyConversation(conversation, position string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conversation, l.position = conversation, position
	l.run.mu.Lock()
	l.run.conversation = conversation
	l.run.mu.Unlock()
}

// Step 将带时间和状态的真实动作提交云端；技术错误正文可能带凭据，只写阶段与结果。
func (l *runLogger) Step(task, flow, step, status string, began time.Time, err error) {
	l.run.mu.Lock()
	l.run.currentStep = step
	l.run.mu.Unlock()
	details := failurediag.Details(err)
	description := ""
	if err != nil {
		description = safeRunFailure(step)
		if len(details) > 0 {
			description = failurediag.Summary(details)
		}
	}
	var scoped *auto_reply.ScopedFailure
	var position, conversation string
	if errors.As(err, &scoped) {
		position, conversation = scoped.Position, scoped.Conversation
	}
	l.logEvent(step, status, description, began, nil, position, conversation, details)
}

// ReportAnalysis 将现有工作流发出的简历整理、条件复核及送达阶段写入时间线。
func (l *runLogger) ReportAnalysis(task string, s shared.AnalysisStatus) {
	l.mu.Lock()
	l.run.mu.Lock()
	s.ProcessedCount = l.run.stats.Processed
	s.SucceededCount = l.run.stats.Succeeded
	s.SkippedCount = l.run.stats.Skipped
	sameEvent := s.UpdatedAt != "" && l.analysis.UpdatedAt == s.UpdatedAt && l.analysis.Stage == s.Stage
	l.run.analysis = s
	l.run.currentStep = s.Stage
	l.run.mu.Unlock()
	l.analysis = s
	l.mu.Unlock()
	if !sameEvent {
		l.logEvent(s.Stage, s.Phase, s.Reason, time.Time{}, s.DurationMS, s.PositionID, "", s.FailureDiagnostics)
	}
}

// AnalysisStatus 返回当前运行的阶段副本。
func (l *runLogger) AnalysisStatus(task string) *shared.AnalysisStatus {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.analysis
	return &s
}

// ResetAnalysis 清除当前可见状态，历史事件已单独保存在云端。
func (l *runLogger) ResetAnalysis(task string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.analysis = shared.AnalysisStatus{}
	l.run.mu.Lock()
	l.run.analysis = shared.AnalysisStatus{}
	l.run.mu.Unlock()
}

// logEvent 用独立稳定编号记录一次真实事件，持久层失败则终止继续执行网页操作。
func (l *runLogger) logEvent(title, status, description string, began time.Time, measured *int64, eventPosition, eventConversation string, details []failurediag.Detail) {
	l.mu.Lock()
	conversation, position := l.conversation, l.position
	l.mu.Unlock()
	if title == "server_run_stopped" {
		// 后台工作可以仍在执行，整个任务的最终失败只采用该错误自己携带的岗位范围。
		conversation, position = "", ""
	}
	if eventPosition != "" {
		if eventPosition != position {
			conversation = ""
		}
		position = eventPosition
		if eventConversation != "" {
			conversation = eventConversation
		}
	}
	now := time.Now().UTC()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		l.run.fail("云端没能保存操作记录，请检查连接后重新启动。")
		return
	}
	event := map[string]any{"run_id": l.run.id, "position_id": position, "conversation_id": conversation, "title": title, "status": status, "description": description, "occurred_at": now}
	if len(details) > 0 {
		event["failure_diagnostics"] = details
		event["description"] = failurediag.Summary(details)
	}
	if measured != nil {
		event["duration_ms"] = *measured
	} else if !began.IsZero() && status != "start" {
		event["duration_ms"] = now.Sub(began).Milliseconds()
	}
	eventID := hex.EncodeToString(id)
	var notice *cloud.AutoReplyFailureNotice
	if len(details) > 0 && position != "" {
		// 将编号、权威事件写入以及可见顺序串行化，后发的游标不能越过尚未提交的故障。
		l.run.failureMu.Lock()
		defer l.run.failureMu.Unlock()
		sequence := l.run.failureSequence + 1
		notice = &cloud.AutoReplyFailureNotice{Sequence: sequence, PositionID: position, Status: status, Step: title, At: now, Details: details}
		eventID = fmt.Sprintf("failure:%s:%020d", l.run.id, sequence)
		event["failure_sequence"] = sequence
		event["failure_machine"] = l.run.scope.machine
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := l.run.client.ServerAutoReplyTransaction(ctx, l.run.cred, func(tx *storage.AutoReplyTx) error {
		_, err := tx.ServerLedger("runtime_events", eventID, event)
		return err
	}); err != nil {
		l.run.fail("云端没能保存操作记录，请检查连接后重新启动。")
		return
	}
	if notice != nil {
		l.run.rememberFailure(*notice)
	}
}

// UpdateReplyStats 发布流程实际计数，不从事件标题推测成功或失败次数。
func (l *runLogger) UpdateReplyStats(stats shared.Stats) {
	l.run.mu.Lock()
	defer l.run.mu.Unlock()
	l.run.stats = stats
}

// rememberFailure 在权威事件持久化后给发起设备留一份有限的排障小票，不将其作为业务数据。
func (r *activeRun) rememberFailure(n cloud.AutoReplyFailureNotice) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failureSequence = n.Sequence
	if len(r.failureNotices) >= 1000 {
		copy(r.failureNotices, r.failureNotices[1:])
		r.failureNotices = r.failureNotices[:999]
	}
	r.failureNotices = append(r.failureNotices, n)
}

// nextPayload 根据本设备已落盘游标返回最近千条安全故障，兼容旧请求，并在已停止的任务上完成最后补领。
func (r *activeRun) nextPayload(command any, running bool, after *int64) map[string]any {
	result := map[string]any{"ok": true, "running": running, "command": command}
	if after == nil {
		return result
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.failureNotices {
		if n.Sequence > *after {
			result["failures"] = append([]cloud.AutoReplyFailureNotice(nil), r.failureNotices[i:]...)
			break
		}
	}
	return result
}
