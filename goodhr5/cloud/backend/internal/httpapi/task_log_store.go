// 本文件负责定义云端任务日志的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// TaskLog 表示一条云端任务日志摘要。
type TaskLog struct {
	ID        string
	TaskID    string
	UserEmail string
	Level     string
	Message   string
	CreatedAt time.Time
}

// TaskLogStore 定义任务日志摘要的持久化能力。
type TaskLogStore interface {
	AddTaskLog(log TaskLog) (TaskLog, error)
	ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool) ([]TaskLog, error)
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
	log.CreatedAt = s.now()
	if log.Level == "" {
		log.Level = "info"
	}
	s.logs = append(s.logs, log)
	return log, nil
}

// ListTaskLogs 列出当前用户某个任务的日志摘要。
func (s *MemoryTaskLogStore) ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool) ([]TaskLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]TaskLog, 0)
	for _, log := range s.logs {
		if (!isAdmin && log.UserEmail != userEmail) || log.TaskID != taskID {
			continue
		}
		items = append(items, log)
	}
	return items, nil
}
