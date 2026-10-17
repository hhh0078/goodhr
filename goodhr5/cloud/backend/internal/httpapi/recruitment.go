// 本文件定义团队招聘流程、岗位应聘实例、面试场次及受限邀请确认的数据合同。
package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrRecruitmentVersion   = errors.New("这条招聘进度刚被更新，请刷新后再操作")
	ErrRecruitmentAccess    = errors.New("这一步需要负责人或指定成员处理")
	ErrRecruitmentInvalid   = errors.New("招聘信息没填完整，请检查后再试")
	ErrRecruitmentDuplicate = errors.New("这个岗位已有进行中的招聘流程，请打开原记录")
)

// RecruitmentNode 保存团队模板中的节点配置，人员只允许有效团队成员。
type RecruitmentNode struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	OwnerEmail        string   `json:"owner_email"`
	InterviewerEmails []string `json:"interviewer_emails"`
	ApproverEmails    []string `json:"approver_emails"`
	CCEmails          []string `json:"cc_emails"`
	ApprovalMode      string   `json:"approval_mode"`
	RequiresFeedback  bool     `json:"requires_feedback"`
	MailTemplateID    string   `json:"mail_template_id,omitempty"`
}

// RecruitmentWorkflow 保存可复用的团队流程模板，实例复制其版本而不随模板修改。
type RecruitmentWorkflow struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id"`
	Name      string            `json:"name"`
	Version   int64             `json:"version"`
	Revision  int64             `json:"revision"`
	IsDefault bool              `json:"is_default"`
	Nodes     []RecruitmentNode `json:"nodes"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// RecruitmentDecision 保存一位指定审核人的独立审核结论。
type RecruitmentDecision struct {
	AuthorEmail string    `json:"author_email"`
	Decision    string    `json:"decision"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// RecruitmentNodeInstance 保存节点配置快照与当前处理轮次，退回时增加轮次。
type RecruitmentNodeInstance struct {
	RecruitmentNode
	Status    string                `json:"status"`
	Attempt   int                   `json:"attempt"`
	Approvals []RecruitmentDecision `json:"approvals"`
}

// RecruitmentFeedback 保存每位面试官对一场面试独立提交的面评。
type RecruitmentFeedback struct {
	AuthorEmail string    `json:"author_email"`
	Conclusion  string    `json:"conclusion"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

// RecruitmentInterview 表示一轮中的一场面试，排期变更修订号使旧邀请自动失效。
type RecruitmentInterview struct {
	ID                string                `json:"id"`
	NodeID            string                `json:"node_id"`
	Attempt           int                   `json:"attempt"`
	StartsAt          time.Time             `json:"starts_at"`
	DurationMinutes   int                   `json:"duration_minutes"`
	Mode              string                `json:"mode"`
	Location          string                `json:"location"`
	MeetingURL        string                `json:"meeting_url"`
	InterviewerEmails []string              `json:"interviewer_emails"`
	Status            string                `json:"status"`
	Confirmation      string                `json:"confirmation"`
	ConfirmationNote  string                `json:"confirmation_note,omitempty"`
	Revision          int64                 `json:"revision"`
	Feedback          []RecruitmentFeedback `json:"feedback"`
}

// RecruitmentEvent 保存追加的业务历史和内部知情人员，不向候选人公开。
type RecruitmentEvent struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	NodeID       string    `json:"node_id,omitempty"`
	InterviewID  string    `json:"interview_id,omitempty"`
	Note         string    `json:"note"`
	AuthorEmail  string    `json:"author_email"`
	NotifyEmails []string  `json:"notify_emails"`
	ReadBy       []string  `json:"read_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// RecruitmentPermissions 告诉当前用户可以执行哪些动作，服务端仍独立验证。
type RecruitmentPermissions struct {
	CanManage     bool `json:"can_manage"`
	CanAssign     bool `json:"can_assign"`
	CanViewResume bool `json:"can_view_resume"`
	CanAdvance    bool `json:"can_advance"`
	CanSchedule   bool `json:"can_schedule"`
	CanFeedback   bool `json:"can_feedback"`
	CanApprove    bool `json:"can_approve"`
}

// RecruitmentApplication 保存候选人在一个岗位的独立HR流程，原推荐历史不被覆盖。
type RecruitmentApplication struct {
	ID               string                    `json:"id"`
	TenantID         string                    `json:"tenant_id"`
	CandidateID      string                    `json:"candidate_id"`
	CandidateName    string                    `json:"candidate_name"`
	AvatarDataURL    string                    `json:"avatar_data_url,omitempty"`
	CandidateEmail   string                    `json:"candidate_email"`
	PositionID       string                    `json:"position_id"`
	PositionName     string                    `json:"position_name"`
	RecommendationID string                    `json:"recommendation_id,omitempty"`
	OwnerEmail       string                    `json:"owner_email"`
	WorkflowID       string                    `json:"workflow_id"`
	WorkflowName     string                    `json:"workflow_name"`
	WorkflowVersion  int64                     `json:"workflow_version"`
	Status           string                    `json:"status"`
	Stage            string                    `json:"stage"`
	CurrentNode      int                       `json:"current_node"`
	Revision         int64                     `json:"revision"`
	EntryDate        string                    `json:"entry_date,omitempty"`
	Nodes            []RecruitmentNodeInstance `json:"nodes"`
	Interviews       []RecruitmentInterview    `json:"interviews"`
	Events           []RecruitmentEvent        `json:"events"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	Permissions      RecruitmentPermissions    `json:"permissions"`
}

// RecruitmentAction 表示需要修订号的明确业务动作，防止多人操作互相覆盖。
type RecruitmentAction struct {
	Action            string    `json:"action"`
	Revision          int64     `json:"revision"`
	NodeID            string    `json:"node_id"`
	InterviewID       string    `json:"interview_id"`
	TargetNodeIndex   int       `json:"target_node_index"`
	Note              string    `json:"note"`
	Content           string    `json:"content"`
	Status            string    `json:"status"`
	OwnerEmail        string    `json:"owner_email"`
	InterviewerEmails []string  `json:"interviewer_emails"`
	ApproverEmails    []string  `json:"approver_emails"`
	CCEmails          []string  `json:"cc_emails"`
	ApprovalMode      string    `json:"approval_mode"`
	StartsAt          time.Time `json:"starts_at"`
	DurationMinutes   int       `json:"duration_minutes"`
	Mode              string    `json:"mode"`
	Location          string    `json:"location"`
	MeetingURL        string    `json:"meeting_url"`
	Conclusion        string    `json:"conclusion"`
	Decision          string    `json:"decision"`
	EntryDate         string    `json:"entry_date"`
}

// RecruitmentInvitation 只保存随机令牌哈希与场次版本，不保存可使用的明文链接。
type RecruitmentInvitation struct {
	TokenHash         string
	TenantID          string
	ApplicationID     string
	InterviewID       string
	InterviewRevision int64
	ExpiresAt         time.Time
}

// RecruitmentMailContext 提供经过授权的服务端邮件变量，不能由浏览器伪造收件人。
type RecruitmentMailContext struct {
	ApplicationID, InterviewID, CandidateEmail, CandidateName, PositionName, NodeName string
	InterviewTime                                                                     time.Time
	Duration                                                                          int
	Mode, Location, MeetingURL, EntryDate, ConfirmationURL                            string
	InterviewStatus, ApplicationStatus                                                string
	InterviewRevision, ApplicationRevision                                            int64
}

// RecruitmentStore 提供带团队隔离、修订校验和原子变更的存储接口。
type RecruitmentStore interface {
	EnsureDefault(tenantID string, workflow RecruitmentWorkflow) error
	ListWorkflows(tenantID string) ([]RecruitmentWorkflow, error)
	SaveWorkflow(tenantID string, workflow RecruitmentWorkflow, revision int64) (RecruitmentWorkflow, error)
	DeleteWorkflow(tenantID, id string, revision int64) error
	ListApplications(tenantID string) ([]RecruitmentApplication, error)
	CreateApplication(application RecruitmentApplication) (RecruitmentApplication, error)
	GetApplication(tenantID, id string) (RecruitmentApplication, error)
	MutateApplication(tenantID, id string, revision int64, change func(*RecruitmentApplication) error) (RecruitmentApplication, error)
	SaveInvitation(invitation RecruitmentInvitation) error
	FindInvitation(hash string) (RecruitmentInvitation, error)
}

// recruitmentID 生成不可预测的标识，避免依赖进程本地递增计数。
func recruitmentID() string {
	return rand.Text()
}

// recruitmentClone 复制嵌套集合，避免内存读取或失败的事务修改已保存数据。
func recruitmentClone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

// recruitmentEmails 规范化并去除重复成员邮箱。
func recruitmentEmails(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !recruitmentContains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

// recruitmentContains 不区分大小写检查指定成员是否属于人员列表。
func recruitmentContains(values []string, email string) bool {
	for _, value := range values {
		if strings.EqualFold(value, email) {
			return true
		}
	}
	return false
}
