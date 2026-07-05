package httpapi

import (
	"fmt"
	"sync"
	"time"
)

type TaskRun struct {
	ID, UserEmail, Name, PlatformID, PlatformAccountID, PositionID, Mode, Status, LocalTaskID string
	DailyGreetedDate                                                                          string
	MatchLimit, ScannedCount, GreetedCount, SkippedCount, FailedCount, DailyGreetedCount      int
	EnableSound, EnableThinking                                                               bool
	CreatedAt                                                                                 time.Time
	StartedAt, FinishedAt                                                                     *time.Time
}
type TaskStore interface {
	CreateTask(task TaskRun) (TaskRun, error)
	ListTasks(tenantID, userEmail string, isAdmin bool) ([]TaskRun, error)
	TaskByID(tenantID, userEmail, taskID string, isAdmin bool) (TaskRun, error)
	DeleteTask(tenantID, userEmail, taskID string, isAdmin bool) error
	UpdateTask(taskID string, task TaskRun) (TaskRun, error)
	UpdateTaskStatus(taskID, status string) error
	IncrementTaskCounts(taskID string, scanned, greeted, skipped, failed int) error
	SyncTaskCounts(taskID string, scanned, greeted, skipped, failed int) error
	TodayGreetedTotal() (int, error)
}
type MemoryTaskStore struct {
	mu     sync.Mutex
	tasks  map[string]TaskRun
	now    func() time.Time
	nextID func() string
}

func NewMemoryTaskStore() *MemoryTaskStore {
	seq := 0
	return &MemoryTaskStore{tasks: make(map[string]TaskRun), now: time.Now, nextID: func() string { seq++; return fmt.Sprintf("task_%d", seq) }}
}
func (s *MemoryTaskStore) CreateTask(task TaskRun) (TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.ID = s.nextID()
	task.Status = "created"
	task.LocalTaskID = task.ID
	task.CreatedAt = s.now()
	s.tasks[task.ID] = task
	return task, nil
}
func (s *MemoryTaskStore) ListTasks(tenantID, userEmail string, isAdmin bool) ([]TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]TaskRun, 0)
	for _, task := range s.tasks {
		if isAdmin {
			items = append(items, task)
		} else if task.UserEmail == userEmail {
			items = append(items, task)
		}
	}
	return items, nil
}
func (s *MemoryTaskStore) TaskByID(tenantID, userEmail, taskID string, isAdmin bool) (TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok || (!isAdmin && task.UserEmail != userEmail) {
		return TaskRun{}, ErrNotFound
	}
	return task, nil
}
func (s *MemoryTaskStore) DeleteTask(tenantID, userEmail, taskID string, isAdmin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok || (!isAdmin && task.UserEmail != userEmail) {
		return ErrNotFound
	}
	delete(s.tasks, taskID)
	return nil
}
func (s *MemoryTaskStore) UpdateTaskStatus(taskID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	now := s.now()
	task.Status = status
	if status == "running" {
		task.StartedAt = &now
		task.FinishedAt = nil
	}
	if status == "failed" || status == "stopped" {
		task.FinishedAt = &now
	}
	s.tasks[taskID] = task
	return nil
}

func (s *MemoryTaskStore) UpdateTask(taskID string, task TaskRun) (TaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tasks[taskID]
	if !ok {
		return TaskRun{}, ErrNotFound
	}
	existing.PlatformID = task.PlatformID
	existing.Name = task.Name
	existing.PlatformAccountID = task.PlatformAccountID
	existing.PositionID = task.PositionID
	existing.Mode = task.Mode
	existing.MatchLimit = task.MatchLimit
	existing.EnableSound = task.EnableSound
	existing.EnableThinking = task.EnableThinking
	s.tasks[taskID] = existing
	return existing, nil
}
func (s *MemoryTaskStore) IncrementTaskCounts(taskID string, scanned, greeted, skipped, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	task.ScannedCount += scanned
	task.GreetedCount += greeted
	task.SkippedCount += skipped
	task.FailedCount += failed
	if greeted > 0 {
		today := time.Now().In(time.Local).Format(time.DateOnly)
		if task.DailyGreetedDate != today {
			task.DailyGreetedDate = today
			task.DailyGreetedCount = 0
		}
		task.DailyGreetedCount += greeted
	}
	s.tasks[taskID] = task
	return nil
}

// SyncTaskCounts 按本地程序累计值同步内存任务统计，避免重复累加。
func (s *MemoryTaskStore) SyncTaskCounts(taskID string, scanned, greeted, skipped, failed int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	deltaGreeted := taskCountMax(0, greeted-task.GreetedCount)
	task.ScannedCount = taskCountMax(task.ScannedCount, scanned)
	task.GreetedCount = taskCountMax(task.GreetedCount, greeted)
	task.SkippedCount = taskCountMax(task.SkippedCount, skipped)
	task.FailedCount = taskCountMax(task.FailedCount, failed)
	if deltaGreeted > 0 {
		today := time.Now().In(time.Local).Format(time.DateOnly)
		if task.DailyGreetedDate != today {
			task.DailyGreetedDate = today
			task.DailyGreetedCount = deltaGreeted
		} else {
			task.DailyGreetedCount += deltaGreeted
		}
	}
	s.tasks[taskID] = task
	return nil
}

// taskCountMax 返回两个任务统计数中的较大值。
func taskCountMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TodayGreetedTotal 统计内存任务中今日打招呼总数。
// 返回 daily_greeted_date 等于今天的 daily_greeted_count 汇总。
func (s *MemoryTaskStore) TodayGreetedTotal() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	today := time.Now().In(time.Local).Format(time.DateOnly)
	total := 0
	for _, task := range s.tasks {
		if task.DailyGreetedDate == today {
			total += task.DailyGreetedCount
		}
	}
	return total, nil
}
