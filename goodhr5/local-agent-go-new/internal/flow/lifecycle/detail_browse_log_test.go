// Package lifecycle 文件作用：验证详情滚动首尾和耗时出现在岗位日志，原子鼠标细节继续按既有规则精简。
package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// TestDetailBrowseTimelineKeepsSummary 验证开始、成功和失败有候选人上下文及耗时，且没有虚构已经到底。
func TestDetailBrowseTimelineKeepsSummary(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTask(ctx, storage.TaskRun{TaskID: "detail-log", PositionID: "detail-position", TaskType: "greeting", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	logger := NewTaskLogger(store, nil)
	logger.CandidateActivity("detail-log", "合成候选人", "正在查看详情页", "running", nil)
	started := time.Now().Add(-25 * time.Millisecond)
	logger.Step("detail-log", "greeting", "browse_candidate_detail", "start", started, nil)
	logger.WorkerLine([]byte(`{"trace_id":"detail-log","action":"page.scroll","step":"mouse_wheel","status":"success","level":"info","diagnostic":true,"output":{}}`))
	logger.Step("detail-log", "platform", "browse_candidate_detail", "success", started, nil)
	logger.Step("detail-log", "greeting", "browse_candidate_detail", "success", started, nil)
	logger.Step("detail-log", "greeting", "browse_candidate_detail", "failed", started, errors.New("合成滚轮失败"))
	items, err := store.ListPositionLogs(ctx, "detail-position", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("应该只有查看、滚动开始、完成与失败，实际为 %d 条", len(items))
	}
	for index, status := range []string{"start", "success", "failed"} {
		item := items[index+1]
		if item.Status != status || item.DurationMS < 25 || !strings.Contains(item.Message, "合成候选人") || !strings.Contains(item.Message, "滚动浏览详情") || strings.Contains(item.Message, "到底") {
			t.Fatalf("详情滚动摘要不正确：%+v", item)
		}
	}
	if !items[3].IsError || !strings.Contains(items[3].ErrorMessage, "合成滚轮失败") || items[2].IsError {
		t.Fatalf("滚动成功与失败分类不正确：%+v", items)
	}
}
