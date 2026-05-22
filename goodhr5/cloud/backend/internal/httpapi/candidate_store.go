// 本文件负责候选人入库存储接口及内存实现。
package httpapi

import (
	"fmt"
	"sync"
	"time"
)

// TaskCandidate 表示任务候选人入库记录。
type TaskCandidate struct {
	ID                  string
	TaskID              string
	UserEmail           string
	PlatformID          string
	PlatformCandidateID string
	CandidateName       string
	BasicInfo           string
	EducationLevel      string
	PersonalDescription string
	RawText             string
	FilterText          string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// CandidateStore 定义任务候选人入库能力。
type CandidateStore interface {
	SaveTaskCandidate(item TaskCandidate) (TaskCandidate, error)
}

// MemoryCandidateStore 提供开发期候选人内存存储。
type MemoryCandidateStore struct {
	mu     sync.Mutex
	items  map[string]TaskCandidate
	now    func() time.Time
	nextID func() string
}

// NewMemoryCandidateStore 创建候选人内存存储。
func NewMemoryCandidateStore() *MemoryCandidateStore {
	seq := 0
	return &MemoryCandidateStore{
		items: map[string]TaskCandidate{},
		now:   time.Now,
		nextID: func() string {
			seq++
			return fmt.Sprintf("task_candidate_%d", seq)
		},
	}
}

// SaveTaskCandidate 新增候选人记录。
func (s *MemoryCandidateStore) SaveTaskCandidate(item TaskCandidate) (TaskCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	item.ID = s.nextID()
	item.CreatedAt = now
	item.UpdatedAt = now
	s.items[item.ID] = item
	return item, nil
}
