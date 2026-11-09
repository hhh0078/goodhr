// Package cloudserver 本文件记录有来源的设备流程/AI阶段，候选人事件仅按真实会话编号关联。
package cloudserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/storage"
	"sync"
	"time"
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
	description := ""
	if err != nil {
		description = safeRunFailure(step)
	}
	l.logEvent(step, status, description, began, nil)
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
		l.logEvent(s.Stage, s.Phase, s.Reason, time.Time{}, s.DurationMS)
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
func (l *runLogger) logEvent(title, status, description string, began time.Time, measured *int64) {
	l.mu.Lock()
	conversation, position := l.conversation, l.position
	l.mu.Unlock()
	now := time.Now().UTC()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		l.run.fail("云端没能保存操作记录，请检查连接后重新启动。")
		return
	}
	event := map[string]any{"run_id": l.run.id, "position_id": position, "conversation_id": conversation, "title": title, "status": status, "description": description, "occurred_at": now}
	if measured != nil {
		event["duration_ms"] = *measured
	} else if !began.IsZero() && status != "start" {
		event["duration_ms"] = now.Sub(began).Milliseconds()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := l.run.client.ServerAutoReplyTransaction(ctx, l.run.cred, func(tx *storage.AutoReplyTx) error {
		_, err := tx.ServerLedger("runtime_events", hex.EncodeToString(id), event)
		return err
	}); err != nil {
		l.run.fail("云端没能保存操作记录，请检查连接后重新启动。")
	}
}

// UpdateReplyStats 发布流程实际计数，不从事件标题推测成功或失败次数。
func (l *runLogger) UpdateReplyStats(stats shared.Stats) {
	l.run.mu.Lock()
	defer l.run.mu.Unlock()
	l.run.stats = stats
}
