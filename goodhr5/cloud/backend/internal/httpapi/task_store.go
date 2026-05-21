package httpapi

import (
	"fmt"
	"sync"
	"time"
)

type TaskRun struct {
	ID, UserEmail, PlatformID, PlatformAccountID, PositionID, Mode, Status, LocalTaskID string
	MatchLimit, ScannedCount, GreetedCount, SkippedCount, FailedCount                   int
	EnableSound                                                                         bool
	CreatedAt                                                                           time.Time
	StartedAt, FinishedAt                                                               *time.Time
}
type TaskStore interface {
	CreateTask(task TaskRun) (TaskRun, error)
	ListTasks(tenantID, userEmail string, isAdmin bool) ([]TaskRun, error)
	TaskByID(tenantID, userEmail, taskID string, isAdmin bool) (TaskRun, error)
	DeleteTask(tenantID, userEmail, taskID string, isAdmin bool) error
	UpdateTask(taskID string, task TaskRun) (TaskRun, error)
	UpdateTaskStatus(taskID, status string) error
	IncrementTaskCounts(taskID string, scanned, greeted, skipped, failed int) error
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
	existing.PlatformAccountID = task.PlatformAccountID
	existing.PositionID = task.PositionID
	existing.Mode = task.Mode
	existing.MatchLimit = task.MatchLimit
	existing.EnableSound = task.EnableSound
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
	s.tasks[taskID] = task
	return nil
}
