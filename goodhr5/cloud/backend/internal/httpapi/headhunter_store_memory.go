// 本文件提供猎头客户与甲方岗位的内存存储，供本地开发和隔离权限测试使用。
package httpapi

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryHeadhunterStore 保存独立团队数据，并在单一锁内验证版本与关联关系。
type MemoryHeadhunterStore struct {
	mu                 sync.Mutex
	customers          map[string]HeadhunterCustomer
	jobs               map[string]HeadhunterJob
	jobHistory         map[string][]string
	businessReferences map[string]bool
}

// NewMemoryHeadhunterStore 创建不包含任何真实客户资料的开发存储。
func NewMemoryHeadhunterStore() *MemoryHeadhunterStore {
	return &MemoryHeadhunterStore{customers: map[string]HeadhunterCustomer{}, jobs: map[string]HeadhunterJob{}, jobHistory: map[string][]string{}, businessReferences: map[string]bool{}}
}

// headhunterMatches 仅使用明确开放的字段执行大小写不敏感的文本查找。
func headhunterMatches(keyword string, fields ...string) bool {
	return keyword == "" || strings.Contains(strings.ToLower(strings.Join(fields, " ")), strings.ToLower(keyword))
}

// headhunterPageBounds 计算分页边界，越界页返回空列表。
func headhunterPageBounds(total, page, size int) (int, int) {
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return start, end
}

// ListCustomers 按可信团队和固定条件查询客户，返回原始记录供服务层按权限脱敏。
func (s *MemoryHeadhunterStore) ListCustomers(scope CandidateLibraryScope, query HeadhunterQuery) (HeadhunterCustomerList, error) {
	if !headhunterScopeValid(scope) {
		return HeadhunterCustomerList{}, ErrRecruitmentAccess
	}
	query, err := headhunterNormalizeQuery(query)
	if err != nil {
		return HeadhunterCustomerList{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []HeadhunterCustomer{}
	for _, item := range s.customers {
		if item.TenantID != scope.TenantID || query.Status != "" && item.Status != query.Status || query.OwnerEmail != "" && item.OwnerEmail != query.OwnerEmail || !headhunterMatches(query.Keyword, item.Name, item.ShortName, item.Code, item.Industry, item.City, item.Source) {
			continue
		}
		item.Jobs = 0
		for _, job := range s.jobs {
			if job.TenantID == scope.TenantID && job.CustomerID == item.ID {
				item.Jobs++
			}
		}
		items = append(items, recruitmentClone(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	start, end := headhunterPageBounds(len(items), query.Page, query.PageSize)
	return HeadhunterCustomerList{Items: items[start:end], Total: len(items), Page: query.Page, PageSize: query.PageSize}, nil
}

// CustomerByID 拒绝跨团队访问并复制嵌套联系人，避免外部修改内存记录。
func (s *MemoryHeadhunterStore) CustomerByID(scope CandidateLibraryScope, id string) (HeadhunterCustomer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.customers[id]
	if !headhunterScopeValid(scope) || !ok || item.TenantID != scope.TenantID {
		return HeadhunterCustomer{}, ErrNotFound
	}
	return recruitmentClone(item), nil
}

// SaveCustomer 原子检查负责人权限、修订号及团队内客户名称编码重复。
func (s *MemoryHeadhunterStore) SaveCustomer(scope CandidateLibraryScope, item HeadhunterCustomer) (HeadhunterCustomer, error) {
	item = recruitmentClone(item)
	if !headhunterScopeValid(scope) {
		return HeadhunterCustomer{}, ErrRecruitmentAccess
	}
	if err := validateHeadhunterCustomer(&item); err != nil {
		return HeadhunterCustomer{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item.OwnerEmail = strings.ToLower(strings.TrimSpace(item.OwnerEmail))
	if item.OwnerEmail == "" {
		item.OwnerEmail = scope.UserEmail
	}
	if !scope.IsAdmin && !strings.EqualFold(item.OwnerEmail, scope.UserEmail) {
		return HeadhunterCustomer{}, ErrRecruitmentAccess
	}
	if item.ID != "" {
		old, ok := s.customers[item.ID]
		if !ok || old.TenantID != scope.TenantID {
			return HeadhunterCustomer{}, ErrNotFound
		}
		if !headhunterCanManage(scope, old.OwnerEmail) {
			return HeadhunterCustomer{}, ErrRecruitmentAccess
		}
		if item.Revision != old.Revision {
			return HeadhunterCustomer{}, ErrRecruitmentVersion
		}
		item.CreatedAt = old.CreatedAt
	} else {
		item.ID = "hhc_" + recruitmentID()
		item.CreatedAt = time.Now().UTC()
		item.Revision = 0
	}
	for _, old := range s.customers {
		if old.TenantID == scope.TenantID && old.ID != item.ID && (strings.EqualFold(old.Name, item.Name) || item.Code != "" && strings.EqualFold(old.Code, item.Code)) {
			return HeadhunterCustomer{}, ErrHeadhunterDuplicate
		}
	}
	item.TenantID = scope.TenantID
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	item.CanManage = false
	item.Jobs = 0
	s.customers[item.ID] = recruitmentClone(item)
	return recruitmentClone(item), nil
}

// DeleteCustomer 仅允许负责人或管理员删除没有业务岗位的客户。
func (s *MemoryHeadhunterStore) DeleteCustomer(scope CandidateLibraryScope, id string, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.customers[id]
	if !headhunterScopeValid(scope) || !ok || item.TenantID != scope.TenantID {
		return ErrNotFound
	}
	if !headhunterCanManage(scope, item.OwnerEmail) {
		return ErrRecruitmentAccess
	}
	if item.Revision != revision {
		return ErrRecruitmentVersion
	}
	for _, job := range s.jobs {
		if job.TenantID == scope.TenantID && job.CustomerID == id {
			return ErrHeadhunterLinked
		}
	}
	delete(s.customers, id)
	return nil
}

// ListJobs 根据当前团队和甲方客户返回完整JD分页，不附带平台配置。
func (s *MemoryHeadhunterStore) ListJobs(scope CandidateLibraryScope, query HeadhunterQuery) (HeadhunterJobList, error) {
	if !headhunterScopeValid(scope) {
		return HeadhunterJobList{}, ErrRecruitmentAccess
	}
	query, err := headhunterNormalizeQuery(query)
	if err != nil {
		return HeadhunterJobList{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []HeadhunterJob{}
	for _, item := range s.jobs {
		if item.TenantID != scope.TenantID || query.Status != "" && item.Status != query.Status || query.OwnerEmail != "" && item.OwnerEmail != query.OwnerEmail || query.CustomerID != "" && item.CustomerID != query.CustomerID {
			continue
		}
		item.CustomerName = s.customers[item.CustomerID].Name
		if !headhunterMatches(query.Keyword, item.Name, item.City, item.Department, item.Description, item.Requirements, item.CustomerName) {
			continue
		}
		items = append(items, recruitmentClone(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	start, end := headhunterPageBounds(len(items), query.Page, query.PageSize)
	return HeadhunterJobList{Items: items[start:end], Total: len(items), Page: query.Page, PageSize: query.PageSize}, nil
}

// JobByID 按团队读取一份完整甲方岗位，供对话工具和页面复用。
func (s *MemoryHeadhunterStore) JobByID(scope CandidateLibraryScope, id string) (HeadhunterJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.jobs[id]
	if !headhunterScopeValid(scope) || !ok || item.TenantID != scope.TenantID {
		return HeadhunterJob{}, ErrNotFound
	}
	item.CustomerName = s.customers[item.CustomerID].Name
	return recruitmentClone(item), nil
}

// SaveJob 在一个锁内验证客户归属、岗位负责人和修订号，再保存独立业务岗位。
func (s *MemoryHeadhunterStore) SaveJob(scope CandidateLibraryScope, item HeadhunterJob) (HeadhunterJob, error) {
	item = recruitmentClone(item)
	if !headhunterScopeValid(scope) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	if err := validateHeadhunterJob(&item); err != nil {
		return HeadhunterJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	customer, ok := s.customers[item.CustomerID]
	if !ok || customer.TenantID != scope.TenantID {
		return HeadhunterJob{}, ErrNotFound
	}
	item.OwnerEmail = strings.ToLower(strings.TrimSpace(item.OwnerEmail))
	if item.OwnerEmail == "" {
		item.OwnerEmail = scope.UserEmail
	}
	if !scope.IsAdmin && !strings.EqualFold(item.OwnerEmail, scope.UserEmail) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	old, exists := s.jobs[item.ID]
	if item.ID != "" {
		if !exists || old.TenantID != scope.TenantID {
			return HeadhunterJob{}, ErrNotFound
		}
		if !headhunterCanManage(scope, old.OwnerEmail) {
			return HeadhunterJob{}, ErrRecruitmentAccess
		}
		if item.Revision != old.Revision {
			return HeadhunterJob{}, ErrRecruitmentVersion
		}
		item.CreatedAt = old.CreatedAt
	} else {
		item.ID = "hhj_" + recruitmentID()
		item.Revision = 0
		item.CreatedAt = time.Now().UTC()
	}
	if (!exists || old.CustomerID != item.CustomerID) && !headhunterCanManage(scope, customer.OwnerEmail) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	item.TenantID = scope.TenantID
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	item.CanManage = false
	item.CustomerName = customer.Name
	s.jobHistory[item.ID] = headhunterMergeIDs(s.jobHistory[item.ID], item.PositionIDs)
	s.jobs[item.ID] = recruitmentClone(item)
	return recruitmentClone(item), nil
}

// headhunterMergeIDs 保留历史绑定编号，供解除绑定后删除时继续核对业务历史。
func headhunterMergeIDs(previous, current []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, list := range [][]string{previous, current} {
		for _, id := range list {
			if !seen[id] {
				result = append(result, id)
				seen[id] = true
			}
		}
	}
	return result
}

// DeleteJob 拒绝删除仍有平台绑定或业务记录的岗位，始终保留平台岗位主体。
func (s *MemoryHeadhunterStore) DeleteJob(scope CandidateLibraryScope, id string, revision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.jobs[id]
	if !headhunterScopeValid(scope) || !ok || item.TenantID != scope.TenantID {
		return ErrNotFound
	}
	if !headhunterCanManage(scope, item.OwnerEmail) {
		return ErrRecruitmentAccess
	}
	if item.Revision != revision {
		return ErrRecruitmentVersion
	}
	if len(item.PositionIDs) > 0 || s.businessReferences[scope.TenantID+":"+id] {
		return ErrHeadhunterLinked
	}
	for _, positionID := range s.jobHistory[id] {
		if s.businessReferences[scope.TenantID+":"+positionID] {
			return ErrHeadhunterLinked
		}
	}
	delete(s.jobs, id)
	delete(s.jobHistory, id)
	return nil
}
