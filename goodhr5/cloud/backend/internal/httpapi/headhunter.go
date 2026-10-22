// 本文件定义猎头甲方客户、业务岗位和平台岗位绑定的数据合同及输入校验。
package httpapi

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

var (
	ErrHeadhunterInvalid   = errors.New("客户或岗位资料需要调整")
	ErrHeadhunterLinked    = errors.New("这条记录还有关联岗位或业务记录，请先处理关联关系")
	ErrHeadhunterDuplicate = errors.New("团队中已存在相同名称或编码的客户")
)

// HeadhunterContact 保存一位甲方联系人，不向无管理权限的成员公开。
type HeadhunterContact struct {
	Name     string `json:"name"`
	Position string `json:"position"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
}

// HeadhunterContract 保存用户提供的资料链接；后端不抓取链接内容。
type HeadhunterContract struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// HeadhunterCustomer 保存当前团队的甲方企业资料与负责人。
type HeadhunterCustomer struct {
	ID         string               `json:"id"`
	TenantID   string               `json:"tenant_id"`
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
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
	Jobs       int                  `json:"jobs"`
	CanManage  bool                 `json:"can_manage"`
}

// HeadhunterJob 保存独立甲方岗位JD；薪资上下限单位均为年薪万元，零表示尚未填写。
type HeadhunterJob struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	CustomerID   string    `json:"customer_id"`
	CustomerName string    `json:"customer_name"`
	Name         string    `json:"name"`
	City         string    `json:"city"`
	Department   string    `json:"department"`
	Headcount    int       `json:"headcount"`
	SalaryMin    float64   `json:"salary_min"`
	SalaryMax    float64   `json:"salary_max"`
	Education    string    `json:"education"`
	Experience   string    `json:"experience"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	OwnerEmail   string    `json:"owner_email"`
	Description  string    `json:"description"`
	Requirements string    `json:"requirements"`
	Remark       string    `json:"remark"`
	PositionIDs  []string  `json:"position_ids"`
	Revision     int64     `json:"revision"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	CanManage    bool      `json:"can_manage"`
}

// HeadhunterQuery 表示服务端允许使用的固定搜索和分页条件。
type HeadhunterQuery struct {
	Keyword, Status, OwnerEmail, CustomerID string
	Page, PageSize                          int
}

// HeadhunterCustomerList 返回客户分页结果。
type HeadhunterCustomerList struct {
	Items    []HeadhunterCustomer `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// HeadhunterJobList 返回甲方岗位分页结果。
type HeadhunterJobList struct {
	Items    []HeadhunterJob `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// HeadhunterStore 按可信团队范围保存客户和业务岗位；更新、删除均检查修订号及归属。
type HeadhunterStore interface {
	ListCustomers(CandidateLibraryScope, HeadhunterQuery) (HeadhunterCustomerList, error)
	CustomerByID(CandidateLibraryScope, string) (HeadhunterCustomer, error)
	SaveCustomer(CandidateLibraryScope, HeadhunterCustomer) (HeadhunterCustomer, error)
	DeleteCustomer(CandidateLibraryScope, string, int64) error
	ListJobs(CandidateLibraryScope, HeadhunterQuery) (HeadhunterJobList, error)
	JobByID(CandidateLibraryScope, string) (HeadhunterJob, error)
	SaveJob(CandidateLibraryScope, HeadhunterJob) (HeadhunterJob, error)
	DeleteJob(CandidateLibraryScope, string, int64) error
}

// headhunterInvalid 生成可供界面直接展示的业务输入提示。
func headhunterInvalid(message string) error {
	return fmt.Errorf("%w：%s", ErrHeadhunterInvalid, message)
}

// headhunterCanManage 判断当前成员是否为记录负责人或团队管理员。
func headhunterCanManage(scope CandidateLibraryScope, owner string) bool {
	return scope.IsAdmin || strings.EqualFold(scope.UserEmail, owner)
}

// headhunterScopeValid 拒绝缺失团队、用户或插件身份的业务访问。
func headhunterScopeValid(scope CandidateLibraryScope) bool {
	return scope.TenantID != "" && scope.UserEmail != "" && !scope.IsExtension
}

// headhunterCustomerPublic 复制客户并隐藏其他成员的联系人与合同资料。
func headhunterCustomerPublic(scope CandidateLibraryScope, item HeadhunterCustomer) HeadhunterCustomer {
	item = recruitmentClone(item)
	item.CanManage = headhunterCanManage(scope, item.OwnerEmail)
	if !item.CanManage {
		item.Contacts = []HeadhunterContact{}
		item.Contracts = []HeadhunterContract{}
	}
	return item
}

// headhunterJobPublic 标记业务岗位可管理状态，不附带平台自动化敏感配置。
func headhunterJobPublic(scope CandidateLibraryScope, item HeadhunterJob) HeadhunterJob {
	item = recruitmentClone(item)
	item.CanManage = headhunterCanManage(scope, item.OwnerEmail)
	return item
}

// headhunterNormalizeQuery 规范分页并限制查询文本长度。
func headhunterNormalizeQuery(query HeadhunterQuery) (HeadhunterQuery, error) {
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.OwnerEmail = strings.ToLower(strings.TrimSpace(query.OwnerEmail))
	query.Status = strings.TrimSpace(query.Status)
	query.CustomerID = strings.TrimSpace(query.CustomerID)
	query.Page, query.PageSize = normalizeCandidatePage(query.Page, query.PageSize)
	if query.Page > 100000 || len([]rune(query.Keyword)) > 200 || len(query.OwnerEmail) > 320 || len(query.CustomerID) > 100 {
		return query, headhunterInvalid("筛选内容太长，请缩小查询范围")
	}
	return query, nil
}

// headhunterTrimTexts 清理文本并按字符数检查存储上限。
func headhunterTrimTexts(limit int, values ...*string) error {
	for _, value := range values {
		*value = strings.TrimSpace(*value)
		if len([]rune(*value)) > limit {
			return headhunterInvalid("资料有些长，请适当缩短")
		}
	}
	return nil
}

// validateHeadhunterCustomer 规范客户字段、联系人和资料链接；空联系人列表允许先建档。
func validateHeadhunterCustomer(item *HeadhunterCustomer) error {
	if err := headhunterTrimTexts(160, &item.Name, &item.ShortName, &item.Code, &item.Industry, &item.Scale, &item.City, &item.Source); err != nil {
		return err
	}
	if item.Name == "" {
		return headhunterInvalid("请先填写甲方公司名称")
	}
	if err := headhunterTrimTexts(2000, &item.Remark); err != nil {
		return err
	}
	if item.Status == "" {
		item.Status = "potential"
	}
	if item.Status != "potential" && item.Status != "active" && item.Status != "paused" {
		return headhunterInvalid("客户状态请选择潜在、签约或暂停合作")
	}
	if len(item.Contacts) > 20 || len(item.Contracts) > 30 {
		return headhunterInvalid("联系人最多20位，资料链接最多30条")
	}
	for index := range item.Contacts {
		contact := &item.Contacts[index]
		if err := headhunterTrimTexts(120, &contact.Name, &contact.Position, &contact.Phone, &contact.Email); err != nil {
			return err
		}
		if contact.Name == "" && contact.Phone == "" && contact.Email == "" {
			return headhunterInvalid("联系人请至少填写姓名、电话或邮箱")
		}
		if contact.Phone != "" {
			contact.Phone = normalizeCandidatePhone(contact.Phone)
			if contact.Phone == "" {
				return headhunterInvalid("联系人电话没认准，请填写完整号码")
			}
		}
		if contact.Email != "" {
			email, ok := normalizeEmail(contact.Email)
			if !ok {
				return headhunterInvalid("联系人邮箱看起来没填完整")
			}
			contact.Email = email
		}
	}
	for index := range item.Contracts {
		contract := &item.Contracts[index]
		if err := headhunterTrimTexts(200, &contract.Name); err != nil {
			return err
		}
		contract.URL = strings.TrimSpace(contract.URL)
		parsed, err := url.Parse(contract.URL)
		if contract.Name == "" || len(contract.URL) > 2048 || err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || strings.ContainsAny(contract.URL, "\r\n") {
			return headhunterInvalid("资料请填写名称和完整的http或https链接")
		}
	}
	if item.Contacts == nil {
		item.Contacts = []HeadhunterContact{}
	}
	if item.Contracts == nil {
		item.Contracts = []HeadhunterContract{}
	}
	return nil
}

// validateHeadhunterJob 校验完整业务JD和有界的平台绑定，不把未知薪资当成负值。
func validateHeadhunterJob(item *HeadhunterJob) error {
	if err := headhunterTrimTexts(160, &item.Name, &item.City, &item.Department, &item.Education, &item.Experience); err != nil {
		return err
	}
	if item.Name == "" || item.CustomerID == "" || len(item.CustomerID) > 100 {
		return headhunterInvalid("请填写岗位名称并选择甲方客户")
	}
	if err := headhunterTrimTexts(100000, &item.Description, &item.Requirements); err != nil {
		return err
	}
	if err := headhunterTrimTexts(2000, &item.Remark); err != nil {
		return err
	}
	if item.Headcount == 0 {
		item.Headcount = 1
	}
	if item.Headcount < 1 || item.Headcount > 100000 {
		return headhunterInvalid("招聘人数请填写1到100000之间的整数")
	}
	for _, value := range []float64{item.SalaryMin, item.SalaryMax} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100000 {
			return headhunterInvalid("年薪请填写有效数字，单位万元")
		}
	}
	if item.SalaryMax > 0 && item.SalaryMin > item.SalaryMax {
		return headhunterInvalid("最高年薪不能低于最低年薪")
	}
	if item.Status == "" {
		item.Status = "open"
	}
	if item.Priority == "" {
		item.Priority = "normal"
	}
	if item.Status != "open" && item.Status != "paused" && item.Status != "closed" {
		return headhunterInvalid("岗位状态请选择招聘中、暂停或关闭")
	}
	if item.Priority != "low" && item.Priority != "normal" && item.Priority != "high" && item.Priority != "urgent" {
		return headhunterInvalid("岗位优先级没认出来")
	}
	if len(item.PositionIDs) > 100 {
		return headhunterInvalid("一个业务岗位最多绑定100个自动招聘岗位")
	}
	seen := map[string]bool{}
	positions := []string{}
	for _, value := range item.PositionIDs {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 100 {
			return headhunterInvalid("绑定岗位编号没认出来")
		}
		if !seen[value] {
			positions = append(positions, value)
			seen[value] = true
		}
	}
	item.PositionIDs = positions
	return nil
}
