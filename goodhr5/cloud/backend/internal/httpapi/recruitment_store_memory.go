// 本文件提供招聘流程的内存存储，供开发与业务状态机测试使用，所有变更在锁内执行。
package httpapi

import (
	"sort"
	"sync"
	"time"
)

// MemoryRecruitmentStore 在进程内隔离各团队流程及邀请令牌哈希。
type MemoryRecruitmentStore struct {
	mu           sync.Mutex
	workflows    map[string]RecruitmentWorkflow
	applications map[string]RecruitmentApplication
	invitations  map[string]RecruitmentInvitation
}

// NewMemoryRecruitmentStore 创建空的招聘存储，不会自动启动历史候选人。
func NewMemoryRecruitmentStore() *MemoryRecruitmentStore {
	return &MemoryRecruitmentStore{workflows: map[string]RecruitmentWorkflow{}, applications: map[string]RecruitmentApplication{}, invitations: map[string]RecruitmentInvitation{}}
}

// EnsureDefault 在团队尚无模板时幂等初始化简单流程。
func (s *MemoryRecruitmentStore) EnsureDefault(tenantID string, workflow RecruitmentWorkflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.workflows {
		if item.TenantID == tenantID {
			return nil
		}
	}
	workflow.TenantID = tenantID
	workflow.IsDefault = true
	s.workflows[workflow.ID] = recruitmentClone(workflow)
	return nil
}

// ListWorkflows 返回当前团队模板的独立副本。
func (s *MemoryRecruitmentStore) ListWorkflows(tenantID string) ([]RecruitmentWorkflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []RecruitmentWorkflow{}
	for _, item := range s.workflows {
		if item.TenantID == tenantID {
			items = append(items, recruitmentClone(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

// SaveWorkflow 原子保存模板及默认选择，更新必须提供旧修订号。
func (s *MemoryRecruitmentStore) SaveWorkflow(tenantID string, item RecruitmentWorkflow, revision int64) (RecruitmentWorkflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.workflows[item.ID]
	if exists && (old.TenantID != tenantID) {
		return RecruitmentWorkflow{}, ErrNotFound
	}
	if exists && old.Revision != revision {
		return RecruitmentWorkflow{}, ErrRecruitmentVersion
	}
	if !exists && revision != 0 {
		return RecruitmentWorkflow{}, ErrNotFound
	}
	now := time.Now().UTC()
	item.TenantID = tenantID
	item.UpdatedAt = now
	if exists {
		item.CreatedAt = old.CreatedAt
		item.Revision = old.Revision + 1
		item.Version = old.Version + 1
		if old.IsDefault {
			item.IsDefault = true
		}
	} else {
		item.CreatedAt = now
		item.Revision = 1
		item.Version = 1
	}
	if item.IsDefault {
		for id, w := range s.workflows {
			if w.TenantID == tenantID && w.IsDefault && id != item.ID {
				w.IsDefault = false
				w.Revision++
				w.UpdatedAt = now
				s.workflows[id] = w
			}
		}
	}
	s.workflows[item.ID] = recruitmentClone(item)
	return recruitmentClone(item), nil
}

// DeleteWorkflow 校验版本后删除非默认模板，实例快照仍保留。
func (s *MemoryRecruitmentStore) DeleteWorkflow(tenantID, id string, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.workflows[id]
	if !ok || item.TenantID != tenantID {
		return ErrNotFound
	}
	if item.Revision != revision {
		return ErrRecruitmentVersion
	}
	if item.IsDefault {
		return ErrRecruitmentInvalid
	}
	delete(s.workflows, id)
	return nil
}

// ListApplications 返回团队内的实例副本，具体参与者权限由服务层验证。
func (s *MemoryRecruitmentStore) ListApplications(tenantID string) ([]RecruitmentApplication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []RecruitmentApplication{}
	for _, item := range s.applications {
		if item.TenantID == tenantID {
			items = append(items, recruitmentClone(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

// CreateApplication 只允许同人同岗有一个进行中流程，已结束流程可再次明确启动。
func (s *MemoryRecruitmentStore) CreateApplication(item RecruitmentApplication) (RecruitmentApplication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, old := range s.applications {
		if old.TenantID == item.TenantID && old.CandidateID == item.CandidateID && old.PositionID == item.PositionID && old.Status == "active" {
			return RecruitmentApplication{}, ErrRecruitmentDuplicate
		}
	}
	if _, ok := s.applications[item.ID]; ok {
		return RecruitmentApplication{}, ErrRecruitmentDuplicate
	}
	s.applications[item.ID] = recruitmentClone(item)
	return recruitmentClone(item), nil
}

// GetApplication 根据团队和实例编号读取，跨团队编号不可访问。
func (s *MemoryRecruitmentStore) GetApplication(tenantID, id string) (RecruitmentApplication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.applications[id]
	if !ok || item.TenantID != tenantID {
		return RecruitmentApplication{}, ErrNotFound
	}
	return recruitmentClone(item), nil
}

// MutateApplication 在锁内比较修订并执行完整动作，失败不会留下部分写入。
func (s *MemoryRecruitmentStore) MutateApplication(tenantID, id string, revision int64, change func(*RecruitmentApplication) error) (RecruitmentApplication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.applications[id]
	if !ok || old.TenantID != tenantID {
		return RecruitmentApplication{}, ErrNotFound
	}
	if old.Revision != revision {
		return RecruitmentApplication{}, ErrRecruitmentVersion
	}
	item := recruitmentClone(old)
	if err := change(&item); err != nil {
		return RecruitmentApplication{}, err
	}
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	item.Permissions = RecruitmentPermissions{}
	s.applications[id] = recruitmentClone(item)
	return recruitmentClone(item), nil
}

// SaveInvitation 只在场次版本仍有效时保存哈希，变更排期后无法继续发旧邀请。
func (s *MemoryRecruitmentStore) SaveInvitation(inv RecruitmentInvitation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, ok := s.applications[inv.ApplicationID]
	if !ok || app.TenantID != inv.TenantID || app.Status != "active" {
		return ErrNotFound
	}
	for _, item := range app.Interviews {
		if item.ID == inv.InterviewID && item.Status == "scheduled" && item.Revision == inv.InterviewRevision {
			s.invitations[inv.TokenHash] = inv
			return nil
		}
	}
	return ErrRecruitmentVersion
}

// FindInvitation 只按令牌哈希查找邀请，不提供团队或候选人枚举入口。
func (s *MemoryRecruitmentStore) FindInvitation(hash string) (RecruitmentInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.invitations[hash]
	if !ok {
		return RecruitmentInvitation{}, ErrNotFound
	}
	return item, nil
}
