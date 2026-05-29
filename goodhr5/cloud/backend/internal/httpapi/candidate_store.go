// 本文件负责候选人入库存储接口及内存实现。
package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskCandidate 表示任务候选人入库记录。
type TaskCandidate struct {
	ID                  string
	TaskID              string
	PositionID          string
	PositionName        string
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
	ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error)
	GetTaskCandidate(tenantID string, candidateID string) (TaskCandidate, error)
}

// TaskCandidateQuery 表示候选人列表查询条件。
type TaskCandidateQuery struct {
	TaskID     string
	PositionID string
	Keyword    string
	Page       int
	PageSize   int
}

// TaskCandidateListResult 表示候选人分页查询结果。
type TaskCandidateListResult struct {
	Items    []TaskCandidate
	Total    int
	Page     int
	PageSize int
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

// ListTaskCandidates 按条件分页列出内存候选人记录。
// tenantID 为团队 ID，内存实现不区分团队；query 为任务筛选和数量限制。
func (s *MemoryCandidateStore) ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	items := make([]TaskCandidate, 0)
	for _, item := range s.items {
		if query.TaskID != "" && item.TaskID != query.TaskID {
			continue
		}
		if query.PositionID != "" && item.PositionID != query.PositionID {
			continue
		}
		if query.Keyword != "" && !candidateContainsKeyword(item, query.Keyword) {
			continue
		}
		items = append(items, item)
	}
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		items = []TaskCandidate{}
	} else {
		end := start + pageSize
		if end > total {
			end = total
		}
		items = items[start:end]
	}
	return TaskCandidateListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetTaskCandidate 读取单个内存候选人记录。
// tenantID 为团队 ID，内存实现不区分团队；candidateID 为候选人 ID。
func (s *MemoryCandidateStore) GetTaskCandidate(tenantID string, candidateID string) (TaskCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.items[candidateID]
	if !ok {
		return TaskCandidate{}, ErrNotFound
	}
	return item, nil
}

// normalizeCandidatePage 规范候选人分页参数。
// page 和 pageSize 为前端传入分页值，返回安全范围内的分页值。
func normalizeCandidatePage(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

// candidateContainsKeyword 判断候选人是否命中搜索关键词。
// item 为候选人记录，keyword 为前端搜索词。
func candidateContainsKeyword(item TaskCandidate, keyword string) bool {
	text := item.CandidateName + " " + item.Phone + " " + item.Email + " " + item.WorkRegion + " " + item.WorkYears + " " + item.BasicInfo + " " + item.EducationLevel + " " + item.ExpectedPosition + " " + item.PersonalDescription + " " + item.RawText + " " + item.FilterText + " " + item.ResumeText
	return containsFold(text, keyword)
}

// containsFold 判断文本是否包含关键词且忽略大小写。
// value 为被搜索文本，keyword 为搜索词。
func containsFold(value string, keyword string) bool {
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), strings.ToLower(keyword))
}

func toJSONB(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return raw
}
