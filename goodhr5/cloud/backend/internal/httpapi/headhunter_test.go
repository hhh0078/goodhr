// 本文件验证猎头甲方客户、完整岗位JD、资料脱敏、团队绑定和删除保护，不连接生产或模型服务。
package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// headhunterFixture 创建包含管理员、负责人、普通同事和外部团队的隔离开发环境。
func headhunterFixture(t *testing.T) (*HeadhunterService, CandidateLibraryScope, CandidateLibraryScope, *MemoryHeadhunterStore) {
	t.Helper()
	tenants := NewMemoryTenantStore()
	tenant, err := tenants.GetOrCreateTenant("owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{"consultant@example.com", "colleague@example.com"} {
		tenants.members[tenant.ID] = append(tenants.members[tenant.ID], TenantMember{Email: email, Status: "active", Role: "member"})
	}
	tenants.members[tenant.ID] = append(tenants.members[tenant.ID], TenantMember{Email: "former@example.com", Status: "inactive", Role: "member"})
	positions := NewMemoryPositionStore()
	for _, item := range []Position{{ID: "own", UserEmail: "consultant@example.com", Name: "自己的自动岗位", PlatformID: "boss", IsOpen: true, AIConfig: map[string]any{"secret": "must-not-leak"}}, {ID: "team", UserEmail: "colleague@example.com", Name: "同事的自动岗位", PlatformID: "liepin", IsOpen: true}, {ID: "external", UserEmail: "outsider@example.com", Name: "外部岗位"}, {ID: "former", UserEmail: "former@example.com", Name: "已离开成员岗位"}} {
		positions.positions[item.ID] = item
	}
	store := NewMemoryHeadhunterStore()
	authStore := NewMemoryAuthStore()
	for token, email := range map[string]string{"owner-token": "owner@example.com", "member-token": "consultant@example.com", "colleague-token": "colleague@example.com"} {
		if err = authStore.SaveSession(token, Session{Email: email}, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	service := NewHeadhunterService(&AuthService{store: authStore}, store, tenants, positions)
	admin := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "owner@example.com", IsAdmin: true}
	member := CandidateLibraryScope{TenantID: tenant.ID, UserEmail: "consultant@example.com"}
	return service, admin, member, store
}

// headhunterCreateCustomer 创建包含敏感联系人和合同资料链接的最小客户。
func headhunterCreateCustomer(t *testing.T, service *HeadhunterService, scope CandidateLibraryScope) HeadhunterCustomer {
	t.Helper()
	item, err := service.SaveCustomer(scope, HeadhunterCustomer{Name: "甲方示例公司", Code: "KH001", OwnerEmail: scope.UserEmail, Contacts: []HeadhunterContact{{Name: "李经理", Phone: "138 0013 8000", Email: "hr@example.com"}}, Contracts: []HeadhunterContract{{Name: "合作材料", URL: "https://files.example.com/contract.pdf"}}})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

// TestHeadhunterCustomerValidation 验证联系人规范、链接协议、未知字段和并发修订边界。
func TestHeadhunterCustomerValidation(t *testing.T) {
	s, admin, member, store := headhunterFixture(t)
	customer := headhunterCreateCustomer(t, s, member)
	if customer.Contacts[0].Phone != "13800138000" || customer.Revision != 1 {
		t.Fatalf("customer=%+v", customer)
	}
	if _, err := s.SaveCustomer(member, HeadhunterCustomer{Name: "甲方示例公司"}); !errors.Is(err, ErrHeadhunterDuplicate) {
		t.Fatalf("duplicate=%v", err)
	}
	for _, link := range []string{"javascript:alert(1)", "file:///tmp/contract", "https://user:password@example.com/c", "https:///missing-host", "https://example.com/\nsecret"} {
		item := HeadhunterCustomer{Name: "链接校验", Contracts: []HeadhunterContract{{Name: "材料", URL: link}}}
		if err := validateHeadhunterCustomer(&item); !errors.Is(err, ErrHeadhunterInvalid) {
			t.Errorf("accepted link %s", link)
		}
	}
	forged := customer
	forged.OwnerEmail = "colleague@example.com"
	if _, err := s.SaveCustomer(member, forged); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("owner reassignment=%v", err)
	}
	other := member
	other.UserEmail = "colleague@example.com"
	if _, err := s.SaveCustomer(other, customer); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("other editor=%v", err)
	}
	customer.Remark = "只修改备注"
	saved, err := s.SaveCustomer(member, customer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCustomer(member, customer); !errors.Is(err, ErrRecruitmentVersion) {
		t.Fatalf("stale update=%v", err)
	}
	if saved.Contacts[0].Phone != "13800138000" || saved.Contracts[0].URL == "" {
		t.Fatal("editing cleared private fields")
	}
	cross := admin
	cross.TenantID = "other-team"
	if _, err = store.CustomerByID(cross, customer.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read=%v", err)
	}
	if err = store.DeleteCustomer(cross, customer.ID, saved.Revision); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete=%v", err)
	}
}

// TestHeadhunterHTTPRedactionAndMetadata 验证团队可浏览基本资料，非负责人无法读取联系人、合同或平台密钥。
func TestHeadhunterHTTPRedactionAndMetadata(t *testing.T) {
	s, _, member, _ := headhunterFixture(t)
	customer := headhunterCreateCustomer(t, s, member)
	for _, path := range []string{"/api/headhunter/customers", "/api/headhunter/customers/" + customer.ID} {
		status, body := candidateTestRequest(t, http.HandlerFunc(s.Route), "colleague-token", http.MethodGet, path, nil)
		encoded, _ := json.Marshal(body)
		if status != 200 || strings.Contains(string(encoded), "13800138000") || strings.Contains(string(encoded), "contract.pdf") || strings.Contains(string(encoded), "hr@example.com") {
			t.Fatalf("redaction status=%d body=%s", status, encoded)
		}
	}
	status, body := candidateTestRequest(t, http.HandlerFunc(s.Route), "member-token", http.MethodGet, "/api/headhunter/customers/"+customer.ID, nil)
	encoded, _ := json.Marshal(body)
	if status != 200 || !strings.Contains(string(encoded), "13800138000") {
		t.Fatalf("owner detail=%s", encoded)
	}
	status, body = candidateTestRequest(t, http.HandlerFunc(s.Route), "member-token", http.MethodGet, "/api/headhunter/metadata", nil)
	encoded, _ = json.Marshal(body)
	text := string(encoded)
	if status != 200 || !strings.Contains(text, "同事的自动岗位") || strings.Contains(text, "外部岗位") || strings.Contains(text, "已离开成员岗位") || strings.Contains(text, "must-not-leak") || strings.Contains(text, "ai_config") {
		t.Fatalf("metadata=%s", text)
	}
	status, _ = candidateTestRequest(t, http.HandlerFunc(s.Route), "member-token", http.MethodPost, "/api/headhunter/customers", map[string]any{"name": "伪造团队", "tenant_id": "other"})
	if status != 400 {
		t.Fatalf("forged tenant status=%d", status)
	}
	for _, path := range []string{"/api/headhunter/customers", "/api/headhunter/jobs", "/api/headhunter/metadata"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		s.Route(response, request)
		if response.Code != 401 {
			t.Fatalf("unauthenticated %s=%d", path, response.Code)
		}
	}
	if err := s.auth.store.SaveSession("plugin-token", Session{Email: member.UserEmail, Kind: extensionSessionKind}, time.Hour); err != nil {
		t.Fatal(err)
	}
	status, _ = candidateTestRequest(t, http.HandlerFunc(s.Route), "plugin-token", http.MethodGet, "/api/headhunter/customers", nil)
	if status != 403 {
		t.Fatalf("plugin status=%d", status)
	}
}

// TestHeadhunterJobsBindingsAndDelete 验证甲方岗位保存完整JD、绑定同事岗位且拒绝外部或离职成员岗位。
func TestHeadhunterJobsBindingsAndDelete(t *testing.T) {
	s, admin, member, store := headhunterFixture(t)
	customer := headhunterCreateCustomer(t, s, member)
	job := HeadhunterJob{CustomerID: customer.ID, Name: "后端工程师", Description: "建设Go业务服务", Requirements: "熟悉Go，有检索项目经验优先", SalaryMin: 30, SalaryMax: 60, PositionIDs: []string{"own", "team", "team"}}
	saved, err := s.SaveJob(member, job)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Description != job.Description || saved.Requirements != job.Requirements || len(saved.PositionIDs) != 2 || saved.Headcount != 1 {
		t.Fatalf("job=%+v", saved)
	}
	for _, id := range []string{"external", "former", "unknown"} {
		job.PositionIDs = []string{id}
		if _, err = s.SaveJob(member, job); !errors.Is(err, ErrHeadhunterInvalid) {
			t.Fatalf("invalid binding %s=%v", id, err)
		}
	}
	if err = store.DeleteCustomer(member, customer.ID, customer.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("linked customer delete=%v", err)
	}
	if err = store.DeleteJob(member, saved.ID, saved.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("bound job delete=%v", err)
	}
	query, err := s.SearchJobs(member, HeadhunterQuery{Keyword: "检索项目", CustomerID: customer.ID})
	if err != nil || query.Total != 1 {
		t.Fatalf("search=%+v err=%v", query, err)
	}
	cross := admin
	cross.TenantID = "other-team"
	if _, err = store.JobByID(cross, saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross job read=%v", err)
	}
	read, err := s.JobByID(member, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	read.PositionIDs = []string{}
	read, err = s.SaveJob(member, read)
	if err != nil {
		t.Fatal(err)
	}
	store.businessReferences[member.TenantID+":team"] = true
	if err = store.DeleteJob(member, read.ID, read.Revision); !errors.Is(err, ErrHeadhunterLinked) {
		t.Fatalf("historical record delete=%v", err)
	}
	delete(store.businessReferences, member.TenantID+":team")
	if err = store.DeleteJob(member, read.ID, read.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = s.positions.PositionByID(member.TenantID, member.UserEmail, "own", false); err != nil {
		t.Fatalf("platform position deleted=%v", err)
	}
	if err = store.DeleteCustomer(member, customer.ID, customer.Revision); err != nil {
		t.Fatal(err)
	}
}

// TestHeadhunterConcurrentRevision 验证同一修订的并发编辑只允许一次成功。
func TestHeadhunterConcurrentRevision(t *testing.T) {
	s, _, member, store := headhunterFixture(t)
	customer := headhunterCreateCustomer(t, s, member)
	var wait sync.WaitGroup
	var mutex sync.Mutex
	success, conflict := 0, 0
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := store.SaveCustomer(member, customer)
			mutex.Lock()
			defer mutex.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, ErrRecruitmentVersion) {
				conflict++
			} else {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if success != 1 || conflict != 7 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

// TestHeadhunterJobValidationAndMemberRevocation 验证薪资数值边界，以及离开团队后无法继续查询或绑定。
func TestHeadhunterJobValidationAndMemberRevocation(t *testing.T) {
	s, _, member, _ := headhunterFixture(t)
	customer := headhunterCreateCustomer(t, s, member)
	for _, salary := range []float64{-1, math.NaN(), math.Inf(1)} {
		item := HeadhunterJob{Name: "测试岗位", CustomerID: customer.ID, SalaryMin: salary}
		if err := validateHeadhunterJob(&item); !errors.Is(err, ErrHeadhunterInvalid) {
			t.Fatalf("salary %v accepted", salary)
		}
	}
	tenants := s.tenants.(*MemoryTenantStore)
	for index := range tenants.members[member.TenantID] {
		if tenants.members[member.TenantID][index].Email == member.UserEmail {
			tenants.members[member.TenantID][index].Status = "inactive"
		}
	}
	if _, err := s.SearchJobs(member, HeadhunterQuery{}); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("revoked search=%v", err)
	}
	if _, err := s.SaveJob(member, HeadhunterJob{Name: "新岗位", CustomerID: customer.ID}); !errors.Is(err, ErrRecruitmentAccess) {
		t.Fatalf("revoked save=%v", err)
	}
}
