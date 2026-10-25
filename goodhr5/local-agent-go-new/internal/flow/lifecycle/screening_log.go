// Package lifecycle 文件作用：把两轮判定的真实耗时和完整原因写入普通日志，不使用并发任务当前候选人上下文推断归属。
package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"goodhr5/local-agent-go-new/internal/screening"
	"goodhr5/local-agent-go-new/internal/storage"
	"goodhr5/local-agent-go-new/internal/system/diagnostics"
)

// ScreeningRound 使用判定现场传入的候选人和毫秒数落日志，避免SQLite写入及后台尾部响应污染耗时。
func (l *TaskLogger) ScreeningRound(taskID, candidateName string, number int, round screening.Round) {
	if l == nil {
		return
	}
	step, label := "screening_first", "第一轮筛选"
	if number == 2 {
		step, label = "screening_second", "第二轮筛选"
	}
	result := "失败"
	switch round.Status {
	case "passed":
		result = "通过"
	case "rejected":
		result = "不通过"
	case "cancelled":
		result = "已取消"
	}
	duration := int64(0)
	if round.DurationMS != nil {
		duration = *round.DurationMS
	}
	message := strings.TrimSpace(string(diagnostics.Redact([]byte(fmt.Sprintf("%s，候选人“%s”%s，原因：%s", label, candidateName, result, round.Reason)))))
	log.Printf("task_id=%s flow=greeting step=%s status=%s duration_ms=%d message=%q", taskID, step, round.Status, duration, message)
	if l.store == nil {
		return
	}
	output, _ := json.Marshal(round)
	output = diagnostics.Redact(output)
	category := "运行"
	isError := round.Status == "failed"
	if isError {
		category = "错误"
	}
	_, err := l.store.SaveTaskLog(context.Background(), storage.TaskLog{TaskID: taskID, Flow: "greeting", Step: step, Status: round.Status, Message: message, Category: category, IsError: isError, Input: json.RawMessage(`{}`), Output: output, DurationMS: duration})
	if err != nil {
		log.Printf("task_id=%s flow=greeting step=save_screening_log status=warning message=筛选普通日志未写入，独立筛选快照仍已保存", taskID)
	}
}
