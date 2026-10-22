// 本文件提供猎头客户与甲方岗位HTTP入口，复用登录团队身份并对联系人、资料链接和岗位绑定实施权限校验。
package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// HeadhunterService 维护独立甲方业务资料，不操作任何招聘平台浏览器。
type HeadhunterService struct {
	auth        *AuthService
	store       HeadhunterStore
	tenants     TenantStore
	positions   PositionStore
	candidates  CandidateStore
	recruitment RecruitmentStore
}

// NewHeadhunterService 注入既有认证、团队、岗位配置和独立业务存储。
func NewHeadhunterService(auth *AuthService, store HeadhunterStore, tenants TenantStore, positions PositionStore) *HeadhunterService {
	return &HeadhunterService{auth: auth, store: store, tenants: tenants, positions: positions}
}

// SetBusinessStores 为内存环境补充已有推荐与企业招聘的删除关联检查；PostgreSQL存储独立执行真实SQL核对。
func (s *HeadhunterService) SetBusinessStores(candidates CandidateStore, recruitment RecruitmentStore) {
	s.candidates = candidates
	s.recruitment = recruitment
}

// headhunterCustomerInput 仅接受用户可维护字段，拒绝客户端伪造团队、权限或时间字段。
type headhunterCustomerInput struct {
	Name       string               `json:"name"`
	ShortName  string               `json:"short_name"`
	Code       string               `json:"code"`
	Industry   string               `json:"industry"`
	Scale      string               `json:"scale"`
	City       string               `json:"city"`
	Status     string               `json:"status"`
	Source     string               `json:"source"`
	OwnerEmail string               `json:"owner_email"`
	Contacts   []HeadhunterContact  `json:"contacts"`
	Contracts  []HeadhunterContract `json:"contracts"`
	Remark     string               `json:"remark"`
	Revision   int64                `json:"revision"`
}

// customer 将已解码的允许字段转成存储合同；编号与团队由服务端填写。
func (input headhunterCustomerInput) customer(id string) HeadhunterCustomer {
	return HeadhunterCustomer{ID: id, Name: input.Name, ShortName: input.ShortName, Code: input.Code, Industry: input.Industry, Scale: input.Scale, City: input.City, Status: input.Status, Source: input.Source, OwnerEmail: input.OwnerEmail, Contacts: input.Contacts, Contracts: input.Contracts, Remark: input.Remark, Revision: input.Revision}
}

// headhunterJobInput 只开放完整业务JD和绑定编号，薪资单位为年薪万元。
type headhunterJobInput struct {
	CustomerID   string   `json:"customer_id"`
	Name         string   `json:"name"`
	City         string   `json:"city"`
	Department   string   `json:"department"`
	Headcount    int      `json:"headcount"`
	SalaryMin    float64  `json:"salary_min"`
	SalaryMax    float64  `json:"salary_max"`
	Education    string   `json:"education"`
	Experience   string   `json:"experience"`
	Status       string   `json:"status"`
	Priority     string   `json:"priority"`
	OwnerEmail   string   `json:"owner_email"`
	Description  string   `json:"description"`
	Requirements string   `json:"requirements"`
	Remark       string   `json:"remark"`
	PositionIDs  []string `json:"position_ids"`
	Revision     int64    `json:"revision"`
}

// job 将可写JD字段转为业务岗位，不接受客户端提供客户名与管理权限。
func (input headhunterJobInput) job(id string) HeadhunterJob {
	return HeadhunterJob{ID: id, CustomerID: input.CustomerID, Name: input.Name, City: input.City, Department: input.Department, Headcount: input.Headcount, SalaryMin: input.SalaryMin, SalaryMax: input.SalaryMax, Education: input.Education, Experience: input.Experience, Status: input.Status, Priority: input.Priority, OwnerEmail: input.OwnerEmail, Description: input.Description, Requirements: input.Requirements, Remark: input.Remark, PositionIDs: input.PositionIDs, Revision: input.Revision}
}

// writeHeadhunterError 返回清楚的业务提示，不把原始数据库信息或联系人写进响应。
func writeHeadhunterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrHeadhunterInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), ErrHeadhunterInvalid.Error()+"："))
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "这条客户或岗位记录暂时无法访问")
	case errors.Is(err, ErrRecruitmentAccess):
		writeError(w, http.StatusForbidden, "这一步需要负责人或团队管理员处理")
	case errors.Is(err, ErrRecruitmentVersion):
		writeError(w, http.StatusConflict, "这条记录刚被更新，请刷新后再保存")
	case errors.Is(err, ErrHeadhunterLinked), errors.Is(err, ErrHeadhunterDuplicate):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "客户资料暂时没处理好，请稍后再试")
	}
}

// activeMembers 同时确认调用人仍是团队有效成员，避免使用过期权限缓存。
func (s *HeadhunterService) activeMembers(scope CandidateLibraryScope) ([]TenantMember, error) {
	if !headhunterScopeValid(scope) || s.tenants == nil {
		return nil, ErrRecruitmentAccess
	}
	items, err := s.tenants.ListMembers(scope.TenantID)
	if err != nil {
		return nil, err
	}
	active := []TenantMember{}
	present := false
	for _, item := range items {
		if item.Status == "active" {
			active = append(active, item)
			if strings.EqualFold(item.Email, scope.UserEmail) {
				present = true
			}
		}
	}
	if !present {
		return nil, ErrRecruitmentAccess
	}
	return active, nil
}

// validateOwner 将新负责人限定在当前团队，并阻止普通成员把资料移交他人。
func (s *HeadhunterService) validateOwner(scope CandidateLibraryScope, owner *string) error {
	*owner = strings.ToLower(strings.TrimSpace(*owner))
	if *owner == "" {
		*owner = scope.UserEmail
	}
	if !scope.IsAdmin && !strings.EqualFold(*owner, scope.UserEmail) {
		return ErrRecruitmentAccess
	}
	members, err := s.activeMembers(scope)
	if err != nil {
		return err
	}
	for _, member := range members {
		if strings.EqualFold(member.Email, *owner) {
			return nil
		}
	}
	return headhunterInvalid("负责人需选择已加入团队的有效成员")
}

// HeadhunterPositionOption 仅开放绑定所需的平台岗位摘要，不含AI提示词、密钥或自动化配置。
type HeadhunterPositionOption struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platform_id"`
	UserEmail  string `json:"user_email"`
	IsOpen     bool   `json:"is_open"`
}

// positionOptions 按团队查询岗位并用当前有效成员二次收口，内存测试也不能混入其他团队。
func (s *HeadhunterService) positionOptions(scope CandidateLibraryScope) ([]HeadhunterPositionOption, error) {
	members, err := s.activeMembers(scope)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, member := range members {
		active[strings.ToLower(member.Email)] = true
	}
	if s.positions == nil {
		return []HeadhunterPositionOption{}, nil
	}
	items, err := s.positions.ListPositions(scope.TenantID, scope.UserEmail, true)
	if err != nil {
		return nil, err
	}
	result := []HeadhunterPositionOption{}
	for _, item := range items {
		if active[strings.ToLower(item.UserEmail)] {
			result = append(result, HeadhunterPositionOption{ID: item.ID, Name: item.Name, PlatformID: item.PlatformID, UserEmail: item.UserEmail, IsOpen: item.IsOpen})
		}
	}
	return result, nil
}

// SaveCustomer 供HTTP和受控业务调用复用负责人校验，不接受客户端决定的团队归属。
func (s *HeadhunterService) SaveCustomer(scope CandidateLibraryScope, item HeadhunterCustomer) (HeadhunterCustomer, error) {
	if err := s.validateOwner(scope, &item.OwnerEmail); err != nil {
		return HeadhunterCustomer{}, err
	}
	result, err := s.store.SaveCustomer(scope, item)
	return headhunterCustomerPublic(scope, result), err
}

// SaveJob 验证所有绑定编号确属当前团队，再保存完整甲方岗位JD。
func (s *HeadhunterService) SaveJob(scope CandidateLibraryScope, item HeadhunterJob) (HeadhunterJob, error) {
	if err := validateHeadhunterJob(&item); err != nil {
		return HeadhunterJob{}, err
	}
	if err := s.validateOwner(scope, &item.OwnerEmail); err != nil {
		return HeadhunterJob{}, err
	}
	options, err := s.positionOptions(scope)
	if err != nil {
		return HeadhunterJob{}, err
	}
	available := map[string]bool{}
	for _, option := range options {
		available[option.ID] = true
	}
	for _, id := range item.PositionIDs {
		if !available[id] {
			return HeadhunterJob{}, headhunterInvalid("有些绑定岗位不属于当前团队，或负责人已离开，请重新选择")
		}
	}
	result, err := s.store.SaveJob(scope, item)
	return headhunterJobPublic(scope, result), err
}

// SearchJobs 为岗位查找工具提供服务端固定查询，权限来自调用者的真实团队范围。
func (s *HeadhunterService) SearchJobs(scope CandidateLibraryScope, query HeadhunterQuery) (HeadhunterJobList, error) {
	if _, err := s.activeMembers(scope); err != nil {
		return HeadhunterJobList{}, err
	}
	result, err := s.store.ListJobs(scope, query)
	for index := range result.Items {
		result.Items[index] = headhunterJobPublic(scope, result.Items[index])
	}
	return result, err
}

// JobByID 为对话工具提供完整业务JD，并重新检查用户仍属于当前团队。
func (s *HeadhunterService) JobByID(scope CandidateLibraryScope, id string) (HeadhunterJob, error) {
	if _, err := s.activeMembers(scope); err != nil {
		return HeadhunterJob{}, err
	}
	result, err := s.store.JobByID(scope, id)
	return headhunterJobPublic(scope, result), err
}

// checkMemoryBusinessReferences 仅为开发内存环境读取既有推荐和企业招聘，删除校验不向调用者泄露具体记录。
func (s *HeadhunterService) checkMemoryBusinessReferences(scope CandidateLibraryScope, id string) error {
	memory, ok := s.store.(*MemoryHeadhunterStore)
	if !ok {
		return nil
	}
	memory.mu.Lock()
	keys := headhunterMergeIDs(memory.jobHistory[id], []string{id})
	memory.mu.Unlock()
	if len(keys) > 1 && s.candidates == nil && s.recruitment == nil {
		return ErrHeadhunterLinked
	}
	if source, ok := s.candidates.(CandidateRecommendationListStore); ok {
		integrityScope := scope
		integrityScope.IsAdmin = true
		for _, key := range keys {
			records, err := source.ListCandidateRecommendationRecords(integrityScope, CandidateRecommendationQuery{PositionID: key, PageSize: 1})
			if err != nil {
				return err
			}
			if records.Total > 0 {
				return ErrHeadhunterLinked
			}
		}
	}
	if s.recruitment != nil {
		items, err := s.recruitment.ListApplications(scope.TenantID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if recruitmentContains(keys, item.PositionID) {
				return ErrHeadhunterLinked
			}
		}
	}
	return nil
}

// Route 分发客户、岗位与绑定元数据接口，所有私有入口先校验Web登录和团队身份。
func (s *HeadhunterService) Route(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "客户管理还没准备好")
		return
	}
	if s.auth != nil {
		if session, err := s.auth.SessionFromRequest(r); err == nil && session.Kind == extensionSessionKind {
			writeError(w, http.StatusForbidden, "客户和岗位管理请到控制台操作")
			return
		}
	}
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	if _, err := s.activeMembers(scope); err != nil {
		writeHeadhunterError(w, err)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/headhunter/"), "/")
	parts := strings.Split(path, "/")
	if path == "metadata" {
		s.metadata(w, r, scope)
		return
	}
	if len(parts) > 2 || (parts[0] != "customers" && parts[0] != "jobs") {
		writeError(w, http.StatusNotFound, "这个客户管理入口暂时没找到")
		return
	}
	id := ""
	if len(parts) == 2 {
		id = parts[1]
		if id == "" || len(id) > 100 {
			writeError(w, http.StatusBadRequest, "记录编号没认出来")
			return
		}
	}
	if parts[0] == "customers" {
		s.customerRoute(w, r, scope, id)
	} else {
		s.jobRoute(w, r, scope, id)
	}
}

// metadata 返回有效成员和脱敏的平台岗位选项，不重复返回敏感配置。
func (s *HeadhunterService) metadata(w http.ResponseWriter, r *http.Request, scope CandidateLibraryScope) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	members, err := s.activeMembers(scope)
	if err != nil {
		writeHeadhunterError(w, err)
		return
	}
	positions, err := s.positionOptions(scope)
	if err != nil {
		writeHeadhunterError(w, err)
		return
	}
	publicMembers := []map[string]string{}
	for _, item := range members {
		publicMembers = append(publicMembers, map[string]string{"email": item.Email, "role": item.Role, "status": item.Status})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "members": publicMembers, "positions": positions})
}

// headhunterRequestQuery 将URL参数映射为固定业务查询合同。
func headhunterRequestQuery(r *http.Request) HeadhunterQuery {
	return HeadhunterQuery{Keyword: firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("keyword")), Status: r.URL.Query().Get("status"), OwnerEmail: r.URL.Query().Get("owner_email"), CustomerID: r.URL.Query().Get("customer_id"), Page: parsePositiveInt(r.URL.Query().Get("page")), PageSize: parsePositiveInt(r.URL.Query().Get("page_size"))}
}

// customerRoute 处理客户列表、受控详情、新建、完整编辑与安全删除。
func (s *HeadhunterService) customerRoute(w http.ResponseWriter, r *http.Request, scope CandidateLibraryScope, id string) {
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			item, err := s.store.CustomerByID(scope, id)
			if err != nil {
				writeHeadhunterError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "customer": headhunterCustomerPublic(scope, item)})
			return
		}
		result, err := s.store.ListCustomers(scope, headhunterRequestQuery(r))
		if err != nil {
			writeHeadhunterError(w, err)
			return
		}
		for index := range result.Items {
			result.Items[index] = headhunterCustomerPublic(scope, result.Items[index])
		}
		writeJSON(w, 200, map[string]any{"ok": true, "items": result.Items, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
	case http.MethodPost, http.MethodPut:
		if r.Method == http.MethodPost && id != "" || r.Method == http.MethodPut && id == "" {
			w.WriteHeader(405)
			return
		}
		var input headhunterCustomerInput
		if err := recruitmentMailReadJSON(w, r, &input); err != nil {
			writeHeadhunterError(w, headhunterInvalid(err.Error()))
			return
		}
		item, err := s.SaveCustomer(scope, input.customer(id))
		if err != nil {
			writeHeadhunterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "customer": item})
	case http.MethodDelete:
		revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if id == "" || err != nil || revision < 1 {
			writeHeadhunterError(w, headhunterInvalid("删除前请刷新记录后再试"))
			return
		}
		if err = s.store.DeleteCustomer(scope, id, revision); err != nil {
			writeHeadhunterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// jobRoute 处理完整甲方岗位JD和平台绑定，删除时保留所有既有平台岗位。
func (s *HeadhunterService) jobRoute(w http.ResponseWriter, r *http.Request, scope CandidateLibraryScope, id string) {
	switch r.Method {
	case http.MethodGet:
		if id != "" {
			item, err := s.JobByID(scope, id)
			if err != nil {
				writeHeadhunterError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "job": item})
			return
		}
		result, err := s.SearchJobs(scope, headhunterRequestQuery(r))
		if err != nil {
			writeHeadhunterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "items": result.Items, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
	case http.MethodPost, http.MethodPut:
		if r.Method == http.MethodPost && id != "" || r.Method == http.MethodPut && id == "" {
			w.WriteHeader(405)
			return
		}
		var input headhunterJobInput
		if err := recruitmentMailReadJSON(w, r, &input); err != nil {
			writeHeadhunterError(w, headhunterInvalid(err.Error()))
			return
		}
		item, err := s.SaveJob(scope, input.job(id))
		if err != nil {
			writeHeadhunterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "job": item})
	case http.MethodDelete:
		revision, err := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if id == "" || err != nil || revision < 1 {
			writeHeadhunterError(w, headhunterInvalid("删除前请刷新记录后再试"))
			return
		}
		item, err := s.store.JobByID(scope, id)
		if err != nil {
			writeHeadhunterError(w, err)
			return
		}
		if !headhunterCanManage(scope, item.OwnerEmail) {
			writeHeadhunterError(w, ErrRecruitmentAccess)
			return
		}
		if err = s.checkMemoryBusinessReferences(scope, id); err != nil {
			writeHeadhunterError(w, err)
			return
		}
		if err = s.store.DeleteJob(scope, id, revision); err != nil {
			writeHeadhunterError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
