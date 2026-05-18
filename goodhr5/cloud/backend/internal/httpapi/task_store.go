// 本文件负责定义云端任务的数据模型和存储接口。
package httpapi

import (
	"sync"
	"time"
)

// TaskRun 表示一个云端任务运行记录。
type TaskRun struct {
	ID                string
	UserEmail         string
	PlatformID        string
	PlatformAccountID string
	Mode              string
	MatchLimit        int
	Status            string
	ScannedCount      int
	GreetedCount      int
	SkippedCount      int
	FailedCount       int
	LocalTaskID       string
	CreatedAt         time.Time
	StartedAt         *time.Time
	FinishedAt        *time.Time
}

// TaskStore 定义任务运行记录的持久化能力。
type TaskStore interface {
	CreateTask(task TaskRun) (TaskRun, error)
	ListTasks(userEmail string) ([]TaskRun, error)
	TaskByID(userEmail string, taskID string) (TaskRun, error)
}

// MemoryTaskStore 提供开发期使用的内存任务存储。
type MemoryTaskStore struct {
	mu     sync.Mutex
	tasks  map[string]TaskRun
	now    func() time.Time
	nextID func() string
}

// NewMemoryTaskStore 创建开发期内存任务存储。
func NewMemoryTaskStore() *MemoryTaskStore {
	seq := 0
	return &MemoryTaskStore{
		tasks: make(map[string]TaskRun),
		now:   time.Now,
		nextID: func() string {
			seq++
			return "task_" + intString(seq)
		},
	}
}

// CreateTask 创建任务运行记录。
func (s *MemoryTaskStore) CreateTask(task TaskRun) (TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task.ID = s.nextID()
	task.Status = "created"
	task.CreatedAt = s.now()
	s.tasks[task.ID] = task
	return task, nil
}

// ListTasks 列出当前用户的任务运行记录。
func (s *MemoryTaskStore) ListTasks(userEmail string) ([]TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]TaskRun, 0)
	for _, task := range s.tasks {
		if task.UserEmail != userEmail {
			continue
		}
		items = append(items, task)
	}
	return items, nil
}

// TaskByID 读取当前用户的单个任务运行记录。
func (s *MemoryTaskStore) TaskByID(userEmail string, taskID string) (TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok || task.UserEmail != userEmail {
		return TaskRun{}, ErrNotFound
	}
	return task, nil
}
