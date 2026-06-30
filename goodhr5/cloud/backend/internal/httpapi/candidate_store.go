// 本文件负责候选人主体、触达上下文和事件流水的存储接口及内存实现。
package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskCandidate 表示简历库候选人展示记录。
type TaskCandidate struct {
	ID                  string
	EngagementID        string
	EngagementStatus    string
	TaskID              string
	PositionID          string
	PositionName        string
	PlatformAccountID   string
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
	WorkStatus          string
	RawText             string
	FilterText          string
	WorkExperiences     []CandidateWorkExperience
	Educations          []CandidateEducation
	Certificates        []CandidateCertificate
	Honors              []CandidateHonor
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
	Events              []CandidateEvent
}

// CandidateProfileInput 表示候选人主体保存参数。
type CandidateProfileInput struct {
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
	WorkStatus          string
	RawText             string
	FilterText          string
	WorkExperiences     []CandidateWorkExperience
	Educations          []CandidateEducation
	Certificates        []CandidateCertificate
	Honors              []CandidateHonor
	ProjectExperiences  []CandidateProjectExperience
	Communications      []CandidateCommunication
	ResumeURL           string
	ResumeText          string
	AIDetailReason      string
	AIDetailScore       *float64
	AIGreetReason       string
	AIGreetScore        *float64
	Ext                 map[string]any
	FirstSeenAt         *time.Time
}

// CandidateEngagement 表示一次岗位、账号和任务下的触达上下文。
type CandidateEngagement struct {
	ID                string
	CandidateID       string
	UserEmail         string
	TaskID            string
	PositionID        string
	PlatformAccountID string
	PlatformID        string
	Status            string
	FirstSeenAt       *time.Time
	DetailFetchedAt   *time.Time
	GreetedAt         *time.Time
	LastEventAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CandidateEvent 表示候选人触达过程中的一条事件流水。
type CandidateEvent struct {
	ID                string         `json:"id"`
	EngagementID      string         `json:"engagement_id"`
	CandidateID       string         `json:"candidate_id"`
	TaskID            string         `json:"task_id"`
	PositionID        string         `json:"position_id"`
	PlatformAccountID string         `json:"platform_account_id"`
	PlatformID        string         `json:"platform_id"`
	EventType         string         `json:"event_type"`
	Score             *float64       `json:"score"`
	Reason            string         `json:"reason"`
	InputText         string         `json:"input_text"`
	OutputText        string         `json:"output_text"`
	MessageText       string         `json:"message_text"`
	Model             string         `json:"model"`
	TokenUsage        int            `json:"token_usage"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
}

// CandidateStore 定义候选人主体、触达上下文和事件流水能力。
type CandidateStore interface {
	SaveCandidateProfile(item CandidateProfileInput) (TaskCandidate, error)
	UpsertCandidateEngagement(item CandidateEngagement) (CandidateEngagement, error)
	SaveCandidateEvent(item CandidateEvent) (CandidateEvent, error)
	UpdateCandidateEngagementStatus(engagementID string, status string, detailFetchedAt *time.Time, greetedAt *time.Time) error
	ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error)
	GetTaskCandidate(tenantID string, candidateID string, engagementID string) (TaskCandidate, error)
	DeleteTeamCandidates(tenantID string) (int, error)
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
	mu          sync.Mutex
	profiles    map[string]TaskCandidate
	engagements map[string]CandidateEngagement
	events      map[string][]CandidateEvent
	now         func() time.Time
	nextID      func(prefix string) string
}

// NewMemoryCandidateStore 创建候选人内存存储。
func NewMemoryCandidateStore() *MemoryCandidateStore {
	seq := 0
	return &MemoryCandidateStore{
		profiles:    map[string]TaskCandidate{},
		engagements: map[string]CandidateEngagement{},
		events:      map[string][]CandidateEvent{},
		now:         time.Now,
		nextID: func(prefix string) string {
			seq++
			return fmt.Sprintf("%s_%d", prefix, seq)
		},
	}
}

// SaveCandidateProfile 新增或更新候选人主体。
// item 为候选人简历字段，返回保存后的简历库记录。
func (s *MemoryCandidateStore) SaveCandidateProfile(item CandidateProfileInput) (TaskCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	id := s.nextID("candidate")
	profile := TaskCandidate{
		ID:                  id,
		UserEmail:           item.UserEmail,
		PlatformID:          item.PlatformID,
		PlatformCandidateID: item.PlatformCandidateID,
		CandidateName:       item.CandidateName,
		BirthYM:             item.BirthYM,
		Phone:               item.Phone,
		Email:               item.Email,
		WorkRegion:          item.WorkRegion,
		WorkYears:           item.WorkYears,
		ExpectedSalaryMin:   item.ExpectedSalaryMin,
		ExpectedSalaryMax:   item.ExpectedSalaryMax,
		BasicInfo:           item.BasicInfo,
		EducationLevel:      item.EducationLevel,
		ExpectedPosition:    item.ExpectedPosition,
		OnlineStatus:        item.OnlineStatus,
		PersonalDescription: item.PersonalDescription,
		WorkStatus:          item.WorkStatus,
		RawText:             item.RawText,
		FilterText:          item.FilterText,
		WorkExperiences:     item.WorkExperiences,
		Educations:          item.Educations,
		Certificates:        item.Certificates,
		Honors:              item.Honors,
		ProjectExperiences:  item.ProjectExperiences,
		Communications:      item.Communications,
		ResumeURL:           item.ResumeURL,
		ResumeText:          item.ResumeText,
		Ext:                 item.Ext,
		AIDetailReason:      item.AIDetailReason,
		AIDetailScore:       item.AIDetailScore,
		AIGreetReason:       item.AIGreetReason,
		AIGreetScore:        item.AIGreetScore,
		FirstSeenAt:         item.FirstSeenAt,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	s.profiles[id] = profile
	return profile, nil
}

// UpsertCandidateEngagement 新增或更新触达上下文。
// item 为任务、岗位和账号上下文，返回保存后的触达记录。
func (s *MemoryCandidateStore) UpsertCandidateEngagement(item CandidateEngagement) (CandidateEngagement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	item.ID = s.nextID("engagement")
	item.CreatedAt = now
	item.UpdatedAt = now
	s.engagements[item.ID] = item
	return item, nil
}

// SaveCandidateEvent 保存候选人事件流水。
// item 为事件内容，返回保存后的事件。
func (s *MemoryCandidateStore) SaveCandidateEvent(item CandidateEvent) (CandidateEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item.ID = s.nextID("event")
	item.CreatedAt = s.now()
	s.events[item.CandidateID] = append(s.events[item.CandidateID], item)
	return item, nil
}

// UpdateCandidateEngagementStatus 更新触达上下文状态。
// engagementID 为触达ID，status 为目标状态，时间字段为空时不覆盖。
func (s *MemoryCandidateStore) UpdateCandidateEngagementStatus(engagementID string, status string, detailFetchedAt *time.Time, greetedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.engagements[engagementID]
	if !ok {
		return ErrNotFound
	}
	if status != "" {
		item.Status = status
	}
	if detailFetchedAt != nil {
		item.DetailFetchedAt = detailFetchedAt
	}
	if greetedAt != nil {
		item.GreetedAt = greetedAt
	}
	now := s.now()
	item.LastEventAt = &now
	item.UpdatedAt = now
	s.engagements[engagementID] = item
	return nil
}

// ListTaskCandidates 按条件分页列出内存候选人记录。
// tenantID 为团队 ID，内存实现不区分团队；query 为任务、岗位和关键词筛选。
func (s *MemoryCandidateStore) ListTaskCandidates(tenantID string, query TaskCandidateQuery) (TaskCandidateListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	items := make([]TaskCandidate, 0)
	for _, item := range s.profiles {
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
// tenantID 为团队 ID，candidateID 为候选人 ID，engagementID 为空时返回全部事件。
func (s *MemoryCandidateStore) GetTaskCandidate(tenantID string, candidateID string, engagementID string) (TaskCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.profiles[candidateID]
	if !ok {
		return TaskCandidate{}, ErrNotFound
	}
	events := s.events[candidateID]
	if strings.TrimSpace(engagementID) != "" {
		events = make([]CandidateEvent, 0, len(s.events[candidateID]))
		for _, event := range s.events[candidateID] {
			if event.EngagementID == engagementID {
				events = append(events, event)
			}
		}
	}
	item.Events = append([]CandidateEvent{}, events...)
	return item, nil
}

// DeleteTeamCandidates 清空团队候选人数据。
// tenantID 为团队 ID，内存实现会清空全部候选人、触达和事件记录。
func (s *MemoryCandidateStore) DeleteTeamCandidates(tenantID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := len(s.profiles)
	s.profiles = map[string]TaskCandidate{}
	s.engagements = map[string]CandidateEngagement{}
	s.events = map[string][]CandidateEvent{}
	return deleted, nil
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
	return strings.Contains(strings.ToLower(text), strings.ToLower(keyword))
}

func toJSONB(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return raw
}
