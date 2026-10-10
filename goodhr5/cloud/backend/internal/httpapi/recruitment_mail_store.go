// 本文件保存团队招聘公司信息、独立邮件模板和幂等邮件发送快照，并提供内存实现。
package httpapi

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrRecruitmentMailConflict = errors.New("这次发送编号已经用于其他内容，请重新生成编号")

const recruitmentMailSendingLease = 2 * time.Minute
const recruitmentMailInterruptedMessage = "发送进程没有留下完整结果，请先核实邮件记录，避免重复邀请"

// RecruitmentCompany 保存团队统一使用的公司署名和招聘联系方式。
type RecruitmentCompany struct {
	CompanyName    string `json:"company_name"`
	ContactName    string `json:"contact_name"`
	ContactPhone   string `json:"contact_phone"`
	ContactEmail   string `json:"contact_email"`
	CompanyAddress string `json:"company_address"`
}

// RecruitmentMailTemplate 表示团队可以独立维护的纯文本邮件模板。
type RecruitmentMailTemplate struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	IsBuiltin bool      `json:"is_builtin"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RecruitmentMailDelivery 保存一次发送的不可变正文和明确结果，不记录SMTP凭据。
type RecruitmentMailDelivery struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"-"`
	ActorEmail          string     `json:"actor_email"`
	ApplicationID       string     `json:"application_id"`
	InterviewID         string     `json:"interview_id,omitempty"`
	TemplateID          string     `json:"template_id,omitempty"`
	Recipient           string     `json:"recipient"`
	ReplyTo             string     `json:"reply_to"`
	Subject             string     `json:"subject"`
	Body                string     `json:"body"`
	HTML                string     `json:"html"`
	Status              string     `json:"status"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	IdempotencyKey      string     `json:"-"`
	Fingerprint         string     `json:"-"`
	ApplicationRevision int64      `json:"application_revision"`
	InterviewRevision   int64      `json:"interview_revision"`
	Prepared            bool       `json:"prepared"`
	CreatedAt           time.Time  `json:"created_at"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
}

// RecruitmentMailStore 为模板、公司配置和独占发送提供相同的持久化合同。
type RecruitmentMailStore interface {
	GetCompany(tenantID string) (RecruitmentCompany, error)
	SaveCompany(tenantID string, company RecruitmentCompany) error
	ListTemplates(tenantID string) ([]RecruitmentMailTemplate, error)
	GetTemplate(tenantID, id string) (RecruitmentMailTemplate, error)
	SaveTemplate(tenantID string, item RecruitmentMailTemplate, create bool) (RecruitmentMailTemplate, error)
	DeleteTemplate(tenantID, id string) error
	FindDelivery(tenantID, actor, key string) (RecruitmentMailDelivery, bool, error)
	ClaimDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, bool, error)
	PrepareDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, error)
	FinishDelivery(tenantID, id, status, message string) (RecruitmentMailDelivery, error)
	ListDeliveries(tenantID, applicationID string) ([]RecruitmentMailDelivery, error)
}

// MemoryRecruitmentMailStore 为本地开发和测试提供受锁保护的团队隔离存储。
type MemoryRecruitmentMailStore struct {
	mu         sync.Mutex
	companies  map[string]RecruitmentCompany
	templates  map[string]map[string]RecruitmentMailTemplate
	deliveries map[string]RecruitmentMailDelivery
	receipts   map[string]string
}

// NewMemoryRecruitmentMailStore 创建空招聘邮件存储，不发送邮件。
func NewMemoryRecruitmentMailStore() *MemoryRecruitmentMailStore {
	return &MemoryRecruitmentMailStore{companies: map[string]RecruitmentCompany{}, templates: map[string]map[string]RecruitmentMailTemplate{}, deliveries: map[string]RecruitmentMailDelivery{}, receipts: map[string]string{}}
}

// ensureLocked 为首次访问的团队一次性创建预置模板，已删除模板不会重新出现。
func (s *MemoryRecruitmentMailStore) ensureLocked(tenantID string) {
	if _, exists := s.companies[tenantID]; exists {
		return
	}
	s.companies[tenantID] = RecruitmentCompany{}
	s.templates[tenantID] = map[string]RecruitmentMailTemplate{}
	for _, item := range defaultRecruitmentMailTemplates() {
		item.ID = randomMailID()
		item.UpdatedAt = time.Now().UTC()
		s.templates[tenantID][item.ID] = item
	}
}

// GetCompany 返回指定团队的公司配置。
func (s *MemoryRecruitmentMailStore) GetCompany(tenantID string) (RecruitmentCompany, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	return s.companies[tenantID], nil
}

// SaveCompany 保存指定团队的招聘署名。
func (s *MemoryRecruitmentMailStore) SaveCompany(tenantID string, item RecruitmentCompany) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	s.companies[tenantID] = item
	return nil
}

// ListTemplates 返回团队独立模板，按名称和编号稳定排序。
func (s *MemoryRecruitmentMailStore) ListTemplates(tenantID string) ([]RecruitmentMailTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	result := []RecruitmentMailTemplate{}
	for _, item := range s.templates[tenantID] {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

// GetTemplate 只读取当前团队拥有的模板。
func (s *MemoryRecruitmentMailStore) GetTemplate(tenantID, id string) (RecruitmentMailTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	item, ok := s.templates[tenantID][id]
	if !ok {
		return RecruitmentMailTemplate{}, ErrNotFound
	}
	return item, nil
}

// SaveTemplate 创建或编辑团队模板，复制模板时生成新的编号。
func (s *MemoryRecruitmentMailStore) SaveTemplate(tenantID string, item RecruitmentMailTemplate, create bool) (RecruitmentMailTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	if create {
		item.ID = randomMailID()
		item.IsBuiltin = false
	} else {
		old, ok := s.templates[tenantID][item.ID]
		if !ok {
			return RecruitmentMailTemplate{}, ErrNotFound
		}
		item.IsBuiltin = old.IsBuiltin
	}
	item.UpdatedAt = time.Now().UTC()
	s.templates[tenantID][item.ID] = item
	return item, nil
}

// DeleteTemplate 删除团队模板，不改动已保存发送快照。
func (s *MemoryRecruitmentMailStore) DeleteTemplate(tenantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(tenantID)
	if _, ok := s.templates[tenantID][id]; !ok {
		return ErrNotFound
	}
	delete(s.templates[tenantID], id)
	return nil
}

// recruitmentMailReceiptKey 将幂等编号限制到团队和操作者，防止跨团队碰撞。
func recruitmentMailReceiptKey(tenantID, actor, key string) string {
	return tenantID + "\x00" + strings.ToLower(actor) + "\x00" + key
}

// FindDelivery 读取原发送快照，使重复请求无需重新渲染或生成确认链接。
func (s *MemoryRecruitmentMailStore) FindDelivery(tenantID, actor, key string) (RecruitmentMailDelivery, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverInterruptedLocked(tenantID)
	id, ok := s.receipts[recruitmentMailReceiptKey(tenantID, actor, key)]
	if !ok {
		return RecruitmentMailDelivery{}, false, nil
	}
	return s.deliveries[id], true, nil
}

// recoverInterruptedLocked 将超过发送截止时间的中断任务记为不确定，禁止重新领取发送。
func (s *MemoryRecruitmentMailStore) recoverInterruptedLocked(tenantID string) {
	now := time.Now().UTC()
	for id, item := range s.deliveries {
		if item.TenantID == tenantID && item.Status == "sending" && !item.CreatedAt.After(now.Add(-recruitmentMailSendingLease)) {
			item.Status = "uncertain"
			item.ErrorMessage = recruitmentMailInterruptedMessage
			item.FinishedAt = &now
			s.deliveries[id] = item
		}
	}
}

// ClaimDelivery 原子持久化sending快照；相同编号只能首次请求拥有发送权。
func (s *MemoryRecruitmentMailStore) ClaimDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := recruitmentMailReceiptKey(item.TenantID, item.ActorEmail, item.IdempotencyKey)
	if id, ok := s.receipts[key]; ok {
		old := s.deliveries[id]
		if old.Fingerprint != item.Fingerprint {
			return RecruitmentMailDelivery{}, false, ErrRecruitmentMailConflict
		}
		return old, false, nil
	}
	item.ID = randomMailID()
	item.Status = "sending"
	item.CreatedAt = time.Now().UTC()
	s.deliveries[item.ID] = item
	s.receipts[key] = item.ID
	return item, true, nil
}

// PrepareDelivery 仅由首次抢占者补全确认链接和最终正文，SMTP开始前持久保存快照。
func (s *MemoryRecruitmentMailStore) PrepareDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.deliveries[item.ID]
	if !ok || old.TenantID != item.TenantID || old.Fingerprint != item.Fingerprint {
		return RecruitmentMailDelivery{}, ErrNotFound
	}
	if old.Status != "sending" || old.Prepared {
		return old, ErrRecruitmentMailConflict
	}
	item.Status = old.Status
	item.CreatedAt = old.CreatedAt
	item.Prepared = true
	s.deliveries[item.ID] = item
	return item, nil
}

// FinishDelivery 仅结束sending任务，已明确结果不能被晚到请求覆盖。
func (s *MemoryRecruitmentMailStore) FinishDelivery(tenantID, id, status, message string) (RecruitmentMailDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.deliveries[id]
	if !ok || item.TenantID != tenantID {
		return RecruitmentMailDelivery{}, ErrNotFound
	}
	if status != "sent" && status != "failed" && status != "uncertain" {
		return RecruitmentMailDelivery{}, errors.New("邮件结果不正确")
	}
	if item.Status == "sending" {
		now := time.Now().UTC()
		item.Status = status
		item.ErrorMessage = message
		item.FinishedAt = &now
		s.deliveries[id] = item
	}
	return item, nil
}

// ListDeliveries 返回团队指定应聘记录的最近发送快照。
func (s *MemoryRecruitmentMailStore) ListDeliveries(tenantID, applicationID string) ([]RecruitmentMailDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recoverInterruptedLocked(tenantID)
	items := []RecruitmentMailDelivery{}
	for _, item := range s.deliveries {
		if item.TenantID == tenantID && item.ApplicationID == applicationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > 100 {
		items = items[:100]
	}
	return items, nil
}

// defaultRecruitmentMailTemplates 提供可由各团队编辑或复制的招聘纯文本模板。
func defaultRecruitmentMailTemplates() []RecruitmentMailTemplate {
	return []RecruitmentMailTemplate{
		{Name: "初试邀请", Category: "interview_first", Subject: "{岗位名称} 初试邀请", Body: "{简历姓名}，你好：\n我们是{公司名称}，想邀请你参加{岗位名称}的{面试节点}。\n时间：{面试时间}\n预计时长：{面试时长}\n方式：{面试方式}\n地点：{面试地点}\n会议链接：{会议链接}\n请在这里确认或提出改期：{确认链接}\n如需联系：{招聘联系人} / {联系电话} / {联系邮箱}\n期待与你聊聊。", IsBuiltin: true},
		{Name: "复试邀请", Category: "interview_followup", Subject: "{岗位名称} 复试邀请", Body: "{简历姓名}，你好：\n感谢参加上一轮沟通，{公司名称}邀请你继续参加{面试节点}。\n岗位：{岗位名称}\n时间：{面试时间}\n预计时长：{面试时长}\n方式：{面试方式}\n地点：{面试地点}\n会议链接：{会议链接}\n确认或改期：{确认链接}\n联系人：{招聘联系人} / {联系电话} / {联系邮箱}", IsBuiltin: true},
		{Name: "面试改期", Category: "reschedule", Subject: "{岗位名称} 面试时间调整", Body: "{简历姓名}，你好：\n{公司名称}的{面试节点}时间已有调整。\n新时间：{面试时间}\n预计时长：{面试时长}\n方式：{面试方式}\n地点：{面试地点}\n会议链接：{会议链接}\n请重新确认：{确认链接}\n联系人：{招聘联系人} / {联系电话} / {联系邮箱}", IsBuiltin: true},
		{Name: "面试取消", Category: "cancel", Subject: "{岗位名称} 面试安排取消", Body: "{简历姓名}，你好：\n很抱歉，{公司名称}此前安排的{岗位名称} / {面试节点}面试已取消。\n如有新的安排，我们会再次联系。\n联系人：{招聘联系人} / {联系电话} / {联系邮箱}", IsBuiltin: true},
		{Name: "面试结果通知", Category: "result", Subject: "{岗位名称} 面试结果", Body: "{简历姓名}，你好：\n感谢你参加{公司名称}的{岗位名称}面试。\n结果及后续安排：【请填写已确认的面试结果和后续安排】\n联系人：{招聘联系人} / {联系电话} / {联系邮箱}", IsBuiltin: true},
		{Name: "Offer通知", Category: "offer", Subject: "{岗位名称} 录用通知", Body: "{简历姓名}，你好：\n{公司名称}很高兴向你发出{岗位名称}的录用邀请。\n预计入职日期：{入职日期}\n录用说明：【请填写已确认的录用条件】\n请联系{招聘联系人}确认安排：{联系电话} / {联系邮箱}", IsBuiltin: true},
		{Name: "入职通知", Category: "onboarding", Subject: "{岗位名称} 入职安排", Body: "{简历姓名}，你好：\n欢迎加入{公司名称}。\n岗位：{岗位名称}\n入职日期：{入职日期}\n报到地点：{公司地址}\n报到材料：【请填写已确认的报到材料】\n联系人：{招聘联系人} / {联系电话} / {联系邮箱}", IsBuiltin: true},
	}
}
