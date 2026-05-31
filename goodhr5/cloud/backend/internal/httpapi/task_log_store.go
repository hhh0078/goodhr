// 本文件负责定义云端任务日志的数据模型和存储接口。
package httpapi

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const maxTaskLogsPerTask = 1000

// TaskLog 表示一条云端任务日志摘要。
type TaskLog struct {
	ID        string
	TaskID    string
	UserEmail string
	Level     string
	Message   string
	CreatedAt time.Time
}

type TaskCountSummary struct {
	ScannedCount int
	GreetedCount int
	SkippedCount int
	FailedCount  int
}

// TaskLogQuery 定义任务日志分页查询条件。
type TaskLogQuery struct {
	Since  *time.Time
	Before *time.Time
	Limit  int
}

// TaskLogStore 定义任务日志摘要的持久化能力。
type TaskLogStore interface {
	AddTaskLog(log TaskLog) (TaskLog, error)
	ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool, query TaskLogQuery) ([]TaskLog, bool, error)
	ClearTaskLogs(tenantID, userEmail, taskID string, isAdmin bool) error
	SummarizeTaskCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]TaskCountSummary, error)
}

// TaskLogFlushStore 定义任务日志缓存落库能力。
type TaskLogFlushStore interface {
	FlushTaskLogs(taskID, userEmail string) error
}

// MemoryTaskLogStore 提供开发期使用的内存任务日志存储。
type MemoryTaskLogStore struct {
	mu     sync.Mutex
	logs   []TaskLog
	now    func() time.Time
	nextID func() string
}

// NewMemoryTaskLogStore 创建开发期内存任务日志存储。
func NewMemoryTaskLogStore() *MemoryTaskLogStore {
	seq := 0
	return &MemoryTaskLogStore{
		logs: make([]TaskLog, 0),
		now:  time.Now,
		nextID: func() string {
			seq++
			return "task_log_" + intString(seq)
		},
	}
}

// AddTaskLog 新增一条任务日志摘要。
func (s *MemoryTaskLogStore) AddTaskLog(log TaskLog) (TaskLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.ID = s.nextID()
	if log.CreatedAt.IsZero() {
		log.CreatedAt = s.now()
	}
	if log.Level == "" {
		log.Level = "info"
	}
	s.trimTaskLogsLocked(log.TaskID, log.UserEmail, 1)
	s.logs = append(s.logs, log)
	return log, nil
}

// ListTaskLogs 列出当前用户某个任务的日志摘要。
func (s *MemoryTaskLogStore) ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool, query TaskLogQuery) ([]TaskLog, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]TaskLog, 0)
	for _, log := range s.logs {
		if (!isAdmin && log.UserEmail != userEmail) || log.TaskID != taskID {
			continue
		}
		if query.Since != nil && log.CreatedAt.Before(*query.Since) {
			continue
		}
		if query.Before != nil && !log.CreatedAt.Before(*query.Before) {
			continue
		}
		items = append(items, log)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	limit := normalizeTaskLogLimit(query.Limit)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// ClearTaskLogs 清空当前用户某个任务的日志摘要。
func (s *MemoryTaskLogStore) ClearTaskLogs(tenantID, userEmail, taskID string, isAdmin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]TaskLog, 0, len(s.logs))
	for _, log := range s.logs {
		isTargetTask := log.TaskID == taskID
		isTargetUser := isAdmin || log.UserEmail == userEmail
		if isTargetTask && isTargetUser {
			continue
		}
		filtered = append(filtered, log)
	}
	s.logs = filtered
	return nil
}

// SummarizeTaskCounts 汇总指定时间范围内各任务的扫描/打招呼/跳过/失败数量。
func (s *MemoryTaskLogStore) SummarizeTaskCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]TaskCountSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := map[string]TaskCountSummary{}
	for _, log := range s.logs {
		if !isAdmin && log.UserEmail != userEmail {
			continue
		}
		if since != nil && log.CreatedAt.Before(*since) {
			continue
		}
		scanned, greeted, skipped, failed := classifyTaskLogMessage(log.Message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		item := result[log.TaskID]
		item.ScannedCount += scanned
		item.GreetedCount += greeted
		item.SkippedCount += skipped
		item.FailedCount += failed
		result[log.TaskID] = item
	}
	return result, nil
}

// trimTaskLogsLocked 写入前检查内存日志数量，超过上限时删除最早日志。
func (s *MemoryTaskLogStore) trimTaskLogsLocked(taskID string, userEmail string, incoming int) {
	count := 0
	for _, item := range s.logs {
		if item.TaskID == taskID && item.UserEmail == userEmail {
			count++
		}
	}
	removeCount := count + incoming - maxTaskLogsPerTask
	if removeCount <= 0 {
		return
	}
	targets := make([]TaskLog, 0, count)
	for _, item := range s.logs {
		if item.TaskID == taskID && item.UserEmail == userEmail {
			targets = append(targets, item)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].CreatedAt.Before(targets[j].CreatedAt)
	})
	removeIDs := map[string]struct{}{}
	for i := 0; i < removeCount && i < len(targets); i++ {
		removeIDs[targets[i].ID] = struct{}{}
	}
	kept := make([]TaskLog, 0, len(s.logs))
	for _, item := range s.logs {
		if _, ok := removeIDs[item.ID]; ok {
			continue
		}
		kept = append(kept, item)
	}
	s.logs = kept
}

func classifyTaskLogMessage(message string) (int, int, int, int) {
	switch {
	case strings.HasPrefix(message, "处理候选人 "):
		return 1, 0, 0, 0
	case strings.Contains(message, "打招呼成功"):
		return 0, 1, 0, 0
	case strings.Contains(message, "筛选跳过"):
		return 0, 0, 1, 0
	case strings.Contains(message, "打招呼失败"), strings.Contains(message, "AI 筛选失败"):
		return 0, 0, 0, 1
	default:
		return 0, 0, 0, 0
	}
}
