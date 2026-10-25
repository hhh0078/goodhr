// Package lifecycle 文件作用：验证两轮普通日志保存真实零毫秒和完整原因，不混入并发主循环当前候选人的名字。
package lifecycle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/screening"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestScreeningOrdinaryLogKeepsReasonAndDuration 验证人工日志保留超过30字的原因和现场耗时，不从落盘时间重新计算。
func TestScreeningOrdinaryLogKeepsReasonAndDuration(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveTask(context.Background(), storage.TaskRun{TaskID: "task", PositionID: "position", StartedAt: time.Now(), Status: "running"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.candidateTaskID = "task"
	logger.candidateName = "另一个正在处理的人"
	duration, zero, accepted := int64(0), float64(0), false
	reason := strings.Repeat("这是完整判定原因。", 50)
	logger.ScreeningRound("task", "真实被预筛的人", 1, screening.Round{Kind: "ai", Status: "rejected", Reason: reason, DurationMS: &duration, Score: &zero, Accepted: &accepted})
	items, err := store.ListPositionLogs(context.Background(), "position", 20)
	if err != nil || len(items) != 1 || items[0].DurationMS != 0 || !strings.Contains(items[0].Message, reason) || !strings.Contains(items[0].Message, "真实被预筛的人") || strings.Contains(items[0].Message, "另一个正在处理的人") || items[0].IsError {
		t.Fatalf("普通日志原因、归属或耗时丢失: %+v err=%v", items, err)
	}
}
