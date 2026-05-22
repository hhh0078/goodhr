// 本文件负责候选人入库存储接口及内存实现。
package httpapi

import (
	"encoding/json"
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
	BirthYM             string
	Phone               string
	Email               string
	WorkRegion          string
	WorkYears           string
	ExpectedSalaryMin   *int
	ExpectedSalaryMax   *int
	BasicInfo           string
	EducationLevel      string
	ExpectedPosition    string
	OnlineStatus        string
	PersonalDescription string
	RawText             string
	FilterText          string
	WorkExperiences     []CandidateWorkExperience
	Educations          []CandidateEducation
	Certificates        []string
	Honors              []string
	ProjectExperiences  []CandidateProjectExperience
	Communications      []CandidateCommunication
	ResumeURL           string
	ResumeText          string
	AIDetailReason      string
	AIDetailScore       *float64
	AIGreetReason       string
	AIGreetScore        *float64
	AIReviewReason      string
	AIReviewScore       *float64
	Ext                 map[string]any
	FirstSeenAt         *time.Time
	DetailFetchedAt     *time.Time
	GreetedAt           *time.Time
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

func toJSONB(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return raw
}
