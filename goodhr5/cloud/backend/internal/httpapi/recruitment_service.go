// 本文件提供团队招聘流程接口、人员授权、节点推进和候选人受限面试确认，所有状态保存在云端。
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RecruitmentService 编排人工招聘业务，不操作招聘平台浏览器或原推荐历史。
type RecruitmentService struct {
	auth       *AuthService
	store      RecruitmentStore
	tenants    TenantStore
	candidates CandidateStore
	positions  PositionStore
}

// NewRecruitmentService 复用登录、团队、候选人和岗位能力创建招聘服务。
func NewRecruitmentService(auth *AuthService, store RecruitmentStore, tenants TenantStore, candidates CandidateStore, positions PositionStore) *RecruitmentService {
	return &RecruitmentService{auth: auth, store: store, tenants: tenants, candidates: candidates, positions: positions}
}

// recruitmentRequestScope 从服务端登录会话获取团队身份，供招聘邮件服务复用。
func recruitmentRequestScope(auth *AuthService, tenants TenantStore, w http.ResponseWriter, r *http.Request) (CandidateLibraryScope, bool) {
	if auth == nil || tenants == nil {
		writeError(w, http.StatusServiceUnavailable, "招聘入口还没准备好，请稍后再试")
		return CandidateLibraryScope{}, false
	}
	session, err := auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录，再继续处理招聘")
		return CandidateLibraryScope{}, false
	}
	tenant, err := tenants.GetOrCreateTenant(session.Email)
	if err != nil {
		writeRecruitmentError(w, err)
		return CandidateLibraryScope{}, false
	}
	admin, err := tenants.IsTenantAdmin(tenant.ID, session.Email)
	if err != nil {
		writeRecruitmentError(w, err)
		return CandidateLibraryScope{}, false
	}
	return CandidateLibraryScope{TenantID: tenant.ID, UserEmail: strings.ToLower(session.Email), AuthorName: session.Email, IsAdmin: admin}, true
}

// writeRecruitmentError 统一区分并发冲突、权限和可修正的业务输入。
func writeRecruitmentError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "这次招聘记录没处理成功，请稍后再试"
	switch {
	case errors.Is(err, ErrNotFound):
		status = http.StatusNotFound
		message = "这条招聘记录暂时无法访问"
	case errors.Is(err, ErrRecruitmentVersion), errors.Is(err, ErrRecruitmentDuplicate):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, ErrRecruitmentAccess):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, ErrRecruitmentInvalid):
		status = http.StatusBadRequest
		message = err.Error()
	default:
		if strings.HasPrefix(err.Error(), "招聘：") {
			status = http.StatusBadRequest
			message = strings.TrimPrefix(err.Error(), "招聘：")
		}
	}
	writeError(w, status, message)
}

// recruitmentInputError 创建简洁的可修正输入提示。
func recruitmentInputError(message string) error { return fmt.Errorf("招聘：%s", message) }

// recruitmentDecode 限制请求体大小且拒绝多段JSON，防止无限输入或拼接请求。
func recruitmentDecode(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return ErrRecruitmentInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrRecruitmentInvalid
	}
	return nil
}

// defaultRecruitmentWorkflow 提供无需通用流程引擎的简单招聘模板。
func defaultRecruitmentWorkflow(tenantID string) RecruitmentWorkflow {
	now := time.Now().UTC()
	nodes := []RecruitmentNode{}
	for _, item := range []struct{ name, kind string }{{"简历初筛", "review"}, {"面试", "interview"}, {"录用审核", "approval"}, {"Offer", "offer"}, {"入职确认", "onboarding"}} {
		nodes = append(nodes, RecruitmentNode{ID: recruitmentID(), Name: item.name, Type: item.kind, InterviewerEmails: []string{}, ApproverEmails: []string{}, CCEmails: []string{}, ApprovalMode: "all", RequiresFeedback: item.kind == "interview"})
	}
	return RecruitmentWorkflow{ID: recruitmentID(), TenantID: tenantID, Name: "简单招聘流程", Version: 1, Revision: 1, IsDefault: true, Nodes: nodes, CreatedAt: now, UpdatedAt: now}
}

// activeMembers 返回已接受邀请的当前团队成员，待加入或离开者不能参与新任务。
func (s *RecruitmentService) activeMembers(tenantID string) ([]TenantMember, error) {
	members, err := s.tenants.ListMembers(tenantID)
	if err != nil {
		return nil, err
	}
	result := []TenantMember{}
	for _, member := range members {
		if member.Status == "active" {
			result = append(result, member)
		}
	}
	return result, nil
}

// validatePeople 校验负责人、面试官、审核人和抄送人均在有效团队成员名单内。
func (s *RecruitmentService) validatePeople(tenantID, owner string, lists ...[]string) error {
	members, err := s.activeMembers(tenantID)
	if err != nil {
		return err
	}
	emails := []string{}
	for _, member := range members {
		emails = append(emails, member.Email)
	}
	if owner != "" && !recruitmentContains(emails, owner) {
		return recruitmentInputError("负责人需选择已加入团队的成员")
	}
	for _, list := range lists {
		for _, email := range list {
			if !recruitmentContains(emails, email) {
				return recruitmentInputError("面试官、审核人和抄送人需选择已加入团队的成员")
			}
		}
	}
	return nil
}

// validateWorkflow 校验节点顺序、唯一编号和人员，允许团队配置多套简单模板。
func (s *RecruitmentService) validateWorkflow(scope CandidateLibraryScope, item *RecruitmentWorkflow) error {
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" || len([]rune(item.Name)) > 80 || len(item.Nodes) == 0 || len(item.Nodes) > 30 {
		return recruitmentInputError("流程需填写名称，并设置1到30个节点")
	}
	ids := map[string]bool{}
	for i := range item.Nodes {
		node := &item.Nodes[i]
		if node.ID == "" {
			node.ID = recruitmentID()
		}
		if ids[node.ID] || len(node.ID) > 80 {
			return ErrRecruitmentInvalid
		}
		ids[node.ID] = true
		node.Name = strings.TrimSpace(node.Name)
		if node.Name == "" || len([]rune(node.Name)) > 80 {
			return recruitmentInputError("每个节点都需要一个简短名称")
		}
		switch node.Type {
		case "review", "interview", "approval", "offer", "onboarding":
		default:
			return ErrRecruitmentInvalid
		}
		if node.ApprovalMode == "" {
			node.ApprovalMode = "all"
		}
		if node.ApprovalMode != "all" && node.ApprovalMode != "any" {
			return ErrRecruitmentInvalid
		}
		node.OwnerEmail = strings.ToLower(strings.TrimSpace(node.OwnerEmail))
		node.InterviewerEmails = recruitmentEmails(node.InterviewerEmails)
		node.ApproverEmails = recruitmentEmails(node.ApproverEmails)
		node.CCEmails = recruitmentEmails(node.CCEmails)
		if err := s.validatePeople(scope.TenantID, node.OwnerEmail, node.InterviewerEmails, node.ApproverEmails, node.CCEmails); err != nil {
			return err
		}
	}
	return nil
}

// Workflows 列出团队模板和有效成员，管理员可以新建流程模板。
func (s *RecruitmentService) Workflows(w http.ResponseWriter, r *http.Request) {
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "招聘入口还没准备好")
		return
	}
	if err := s.store.EnsureDefault(scope.TenantID, defaultRecruitmentWorkflow(scope.TenantID)); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListWorkflows(scope.TenantID)
		if err != nil {
			writeRecruitmentError(w, err)
			return
		}
		members, err := s.activeMembers(scope.TenantID)
		if err != nil {
			writeRecruitmentError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "workflows": items, "members": members, "can_manage": scope.IsAdmin})
	case http.MethodPost:
		if !scope.IsAdmin {
			writeRecruitmentError(w, ErrRecruitmentAccess)
			return
		}
		var item RecruitmentWorkflow
		if err := recruitmentDecode(w, r, &item); err != nil {
			writeRecruitmentError(w, err)
			return
		}
		item.ID = recruitmentID()
		if err := s.validateWorkflow(scope, &item); err != nil {
			writeRecruitmentError(w, err)
			return
		}
		saved, err := s.store.SaveWorkflow(scope.TenantID, item, 0)
		if err != nil {
			writeRecruitmentError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "workflow": saved})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// WorkflowDetail 更新或删除指定模板，进行中的实例继续使用原版本快照。
func (s *RecruitmentService) WorkflowDetail(w http.ResponseWriter, r *http.Request) {
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	if !scope.IsAdmin {
		writeRecruitmentError(w, ErrRecruitmentAccess)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/recruitment/workflows/")
	if id == "" || strings.Contains(id, "/") {
		writeRecruitmentError(w, ErrNotFound)
		return
	}
	if r.Method == http.MethodDelete {
		revision, _ := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
		if revision == 0 {
			var input struct {
				Revision int64 `json:"revision"`
			}
			if err := recruitmentDecode(w, r, &input); err != nil {
				writeRecruitmentError(w, err)
				return
			}
			revision = input.Revision
		}
		if err := s.store.DeleteWorkflow(scope.TenantID, id, revision); err != nil {
			writeRecruitmentError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input RecruitmentWorkflow
	if err := recruitmentDecode(w, r, &input); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	input.ID = id
	if err := s.validateWorkflow(scope, &input); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	saved, err := s.store.SaveWorkflow(scope.TenantID, input, input.Revision)
	if err != nil {
		writeRecruitmentError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "workflow": saved})
}

// recruitmentCurrentNode 返回当前节点，已结束实例仍保留最后节点历史。
func recruitmentCurrentNode(app *RecruitmentApplication) *RecruitmentNodeInstance {
	if app.CurrentNode < 0 || app.CurrentNode >= len(app.Nodes) {
		return nil
	}
	return &app.Nodes[app.CurrentNode]
}

// recruitmentCanManage 只允许团队管理员、申请负责人或当前节点负责人推进。
func recruitmentCanManage(scope CandidateLibraryScope, app RecruitmentApplication) bool {
	if app.TenantID != scope.TenantID {
		return false
	}
	if scope.IsAdmin || strings.EqualFold(app.OwnerEmail, scope.UserEmail) {
		return true
	}
	node := recruitmentCurrentNode(&app)
	return node != nil && strings.EqualFold(node.OwnerEmail, scope.UserEmail)
}

// recruitmentCanView 仅向申请参与者授予该实例访问权限，不扩大全部简历权限。
func recruitmentCanView(scope CandidateLibraryScope, app RecruitmentApplication) bool {
	if app.TenantID != scope.TenantID {
		return false
	}
	if recruitmentCanManage(scope, app) {
		return true
	}
	for _, node := range app.Nodes {
		if strings.EqualFold(node.OwnerEmail, scope.UserEmail) || recruitmentContains(node.InterviewerEmails, scope.UserEmail) || recruitmentContains(node.ApproverEmails, scope.UserEmail) || recruitmentContains(node.CCEmails, scope.UserEmail) {
			return true
		}
	}
	for _, interview := range app.Interviews {
		if recruitmentContains(interview.InterviewerEmails, scope.UserEmail) {
			return true
		}
	}
	return false
}

// recruitmentPermissions 生成当前动作入口，并保留服务端独立授权检查。
func recruitmentPermissions(scope CandidateLibraryScope, app RecruitmentApplication) RecruitmentPermissions {
	active := app.Status == "active"
	manage := recruitmentCanManage(scope, app)
	node := recruitmentCurrentNode(&app)
	p := RecruitmentPermissions{CanManage: manage, CanAssign: active && (scope.IsAdmin || strings.EqualFold(app.OwnerEmail, scope.UserEmail)), CanAdvance: active && manage, CanSchedule: active && manage && node != nil && node.Type == "interview"}
	if !active || node == nil {
		return p
	}
	p.CanApprove = recruitmentContains(node.ApproverEmails, scope.UserEmail)
	for _, interview := range app.Interviews {
		if interview.NodeID == node.ID && interview.Attempt == node.Attempt && interview.Status != "cancelled" && recruitmentContains(interview.InterviewerEmails, scope.UserEmail) {
			p.CanFeedback = true
		}
	}
	return p
}

// recruitmentPendingFor 判断当前成员是否有该申请的待处理事项。
func recruitmentPendingFor(scope CandidateLibraryScope, app RecruitmentApplication) bool {
	for _, event := range app.Events {
		if recruitmentContains(event.NotifyEmails, scope.UserEmail) && !recruitmentContains(event.ReadBy, scope.UserEmail) {
			return true
		}
	}
	if app.Status != "active" {
		return false
	}
	node := recruitmentCurrentNode(&app)
	if node == nil {
		return false
	}
	if recruitmentCanManage(scope, app) {
		return true
	}
	if recruitmentContains(node.ApproverEmails, scope.UserEmail) {
		found := false
		for _, item := range node.Approvals {
			if strings.EqualFold(item.AuthorEmail, scope.UserEmail) {
				found = true
			}
		}
		if !found {
			return true
		}
	}
	for _, interview := range app.Interviews {
		if interview.NodeID != node.ID || interview.Attempt != node.Attempt || interview.Status == "cancelled" || !recruitmentContains(interview.InterviewerEmails, scope.UserEmail) {
			continue
		}
		found := false
		for _, feedback := range interview.Feedback {
			if strings.EqualFold(feedback.AuthorEmail, scope.UserEmail) {
				found = true
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// Applications 读取有权限的岗位流程或人工启动新实例，不根据历史推荐状态自动转换。
func (s *RecruitmentService) Applications(w http.ResponseWriter, r *http.Request) {
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.ListApplications(scope.TenantID)
		if err != nil {
			writeRecruitmentError(w, err)
			return
		}
		result := []RecruitmentApplication{}
		for _, item := range items {
			mine := r.URL.Query().Get("mine") == "true" || r.URL.Query().Get("mine") == "1"
			if !recruitmentCanView(scope, item) || r.URL.Query().Get("candidate_id") != "" && item.CandidateID != r.URL.Query().Get("candidate_id") || r.URL.Query().Get("position_id") != "" && item.PositionID != r.URL.Query().Get("position_id") || mine && !recruitmentPendingFor(scope, item) {
				continue
			}
			item.Permissions = recruitmentPermissions(scope, item)
			result = append(result, item)
		}
		writeJSON(w, 200, map[string]any{"ok": true, "applications": result})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		CandidateID string `json:"candidate_id"`
		PositionID  string `json:"position_id"`
		WorkflowID  string `json:"workflow_id"`
		OwnerEmail  string `json:"owner_email"`
	}
	if err := recruitmentDecode(w, r, &input); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	app, err := s.StartApplication(scope, input.CandidateID, input.PositionID, input.WorkflowID, input.OwnerEmail)
	if err != nil {
		writeRecruitmentError(w, err)
		return
	}
	app.Permissions = recruitmentPermissions(scope, app)
	writeJSON(w, 200, map[string]any{"ok": true, "application": app})
}

// StartApplication 根据可访问简历和岗位明确创建流程，保留原有推荐关系及全部历史。
func (s *RecruitmentService) StartApplication(scope CandidateLibraryScope, candidateID, positionID, workflowID, ownerEmail string) (RecruitmentApplication, error) {
	if err := s.validatePeople(scope.TenantID, scope.UserEmail); err != nil {
		return RecruitmentApplication{}, ErrRecruitmentAccess
	}
	if candidateID == builtinCandidateID {
		return RecruitmentApplication{}, recruitmentInputError("内置示例只供查看，请选择真实候选人")
	}
	if s.candidates == nil || s.positions == nil || s.store == nil {
		return RecruitmentApplication{}, ErrRecruitmentInvalid
	}
	candidate, err := s.candidates.GetPositionCandidate(scope.TenantID, candidateID, "", scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return RecruitmentApplication{}, err
	}
	positions, err := s.positions.ListPositions(scope.TenantID, scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return RecruitmentApplication{}, err
	}
	var position Position
	for _, item := range positions {
		if item.ID == positionID {
			position = item
			break
		}
	}
	if position.ID == "" {
		return RecruitmentApplication{}, ErrNotFound
	}
	ownerEmail = strings.ToLower(strings.TrimSpace(ownerEmail))
	if ownerEmail == "" {
		ownerEmail = scope.UserEmail
	}
	if err = s.validatePeople(scope.TenantID, ownerEmail); err != nil {
		return RecruitmentApplication{}, err
	}
	if err = s.store.EnsureDefault(scope.TenantID, defaultRecruitmentWorkflow(scope.TenantID)); err != nil {
		return RecruitmentApplication{}, err
	}
	workflows, err := s.store.ListWorkflows(scope.TenantID)
	if err != nil {
		return RecruitmentApplication{}, err
	}
	var workflow RecruitmentWorkflow
	for _, item := range workflows {
		if workflowID != "" && item.ID == workflowID || workflowID == "" && item.IsDefault {
			workflow = item
			break
		}
	}
	if workflow.ID == "" {
		return RecruitmentApplication{}, ErrNotFound
	}
	if err = s.validateWorkflow(scope, &workflow); err != nil {
		return RecruitmentApplication{}, err
	}
	now := time.Now().UTC()
	app := RecruitmentApplication{ID: recruitmentID(), TenantID: scope.TenantID, CandidateID: candidateID, CandidateName: candidate.CandidateName, CandidateEmail: candidate.Email, PositionID: position.ID, PositionName: position.Name, OwnerEmail: ownerEmail, WorkflowID: workflow.ID, WorkflowName: workflow.Name, WorkflowVersion: workflow.Version, Status: "active", CurrentNode: 0, Revision: 1, Nodes: []RecruitmentNodeInstance{}, Interviews: []RecruitmentInterview{}, Events: []RecruitmentEvent{}, CreatedAt: now, UpdatedAt: now}
	for i, node := range workflow.Nodes {
		if node.OwnerEmail == "" {
			node.OwnerEmail = ownerEmail
		}
		if node.Type == "approval" && len(node.ApproverEmails) == 0 {
			node.ApproverEmails = []string{node.OwnerEmail}
		}
		status := "pending"
		if i == 0 {
			status = "current"
		}
		app.Nodes = append(app.Nodes, RecruitmentNodeInstance{RecruitmentNode: node, Status: status, Attempt: 1, Approvals: []RecruitmentDecision{}})
	}
	for _, rec := range candidate.Recommendations {
		if rec.PositionID == position.ID {
			app.RecommendationID = rec.ID
			break
		}
	}
	recruitmentMapStage(&app)
	recruitmentAppendEvent(&app, scope.UserEmail, "started", "人工启用HR招聘流程", "")
	return s.store.CreateApplication(app)
}

// ApplicationDetail 仅在参与者授权后读取必要简历字段，避免给面试官扩大简历库查询权限。
func (s *RecruitmentService) ApplicationDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/recruitment/applications/")
	if strings.Contains(id, "/") {
		writeRecruitmentError(w, ErrNotFound)
		return
	}
	app, err := s.GetApplication(scope, id)
	if err != nil {
		writeRecruitmentError(w, err)
		return
	}
	candidate := map[string]any{}
	if s.candidates != nil {
		// 完整简历链接沿用原简历库权限；参与面试只授予下方受限资料投影。
		if _, fullReadErr := s.candidates.GetPositionCandidate(scope.TenantID, app.CandidateID, "", scope.UserEmail, scope.IsAdmin); fullReadErr == nil {
			app.Permissions.CanViewResume = true
		}
		item, readErr := s.candidates.GetPositionCandidate(scope.TenantID, app.CandidateID, "", scope.UserEmail, true)
		if readErr == nil {
			candidate = map[string]any{"id": item.ID, "candidate_name": item.CandidateName, "phone": item.Phone, "email": item.Email, "basic_info": item.BasicInfo, "work_years": item.WorkYears, "education_level": item.EducationLevel, "work_region": item.WorkRegion, "personal_description": item.PersonalDescription, "work_experiences": safeSlice(item.WorkExperiences), "educations": safeSlice(item.Educations), "project_experiences": safeSlice(item.ProjectExperiences)}
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "application": app, "candidate": candidate})
}

// GetApplication 对当前团队和参与者进行双重授权后返回实例。
func (s *RecruitmentService) GetApplication(scope CandidateLibraryScope, id string) (RecruitmentApplication, error) {
	if err := s.validatePeople(scope.TenantID, scope.UserEmail); err != nil {
		return RecruitmentApplication{}, ErrRecruitmentAccess
	}
	app, err := s.store.GetApplication(scope.TenantID, id)
	if err != nil {
		return app, err
	}
	if !recruitmentCanView(scope, app) {
		return RecruitmentApplication{}, ErrNotFound
	}
	app.Permissions = recruitmentPermissions(scope, app)
	return app, nil
}

// ApplicationAction 接收一个明确动作，在事务内校验权限、前置条件和修订号。
func (s *RecruitmentService) ApplicationAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	scope, ok := recruitmentRequestScope(s.auth, s.tenants, w, r)
	if !ok {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/recruitment/applications/"), "/actions")
	if id == "" || strings.Contains(id, "/") {
		writeRecruitmentError(w, ErrNotFound)
		return
	}
	var input RecruitmentAction
	if err := recruitmentDecode(w, r, &input); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	app, err := s.Act(scope, id, input)
	if err != nil {
		writeRecruitmentError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "application": app})
}

// recruitmentAppendEvent 追加带当前节点知情人的持久事件，不向候选人邮件暴露内部意见。
func recruitmentAppendEvent(app *RecruitmentApplication, author, kind, note, interviewID string) {
	node := recruitmentCurrentNode(app)
	id := ""
	recipients := []string{app.OwnerEmail}
	if node != nil {
		id = node.ID
		recipients = append(recipients, node.OwnerEmail)
		recipients = append(recipients, node.CCEmails...)
		recipients = append(recipients, node.ApproverEmails...)
		recipients = append(recipients, node.InterviewerEmails...)
	}
	if index := recruitmentInterviewIndex(app, interviewID); index >= 0 {
		recipients = append(recipients, app.Interviews[index].InterviewerEmails...)
	}
	app.Events = append(app.Events, RecruitmentEvent{ID: recruitmentID(), Type: kind, NodeID: id, InterviewID: interviewID, Note: note, AuthorEmail: author, NotifyEmails: recruitmentEmails(recipients), ReadBy: recruitmentEmails([]string{author}), CreatedAt: time.Now().UTC()})
}

// recruitmentMapStage 映射原推荐展示阶段，只更新HR实例而不回写或覆盖旧推荐。
func recruitmentMapStage(app *RecruitmentApplication) {
	if app.Status == "completed" && len(app.Nodes) > 0 && app.Nodes[len(app.Nodes)-1].Type == "onboarding" {
		app.Stage = "hired"
		return
	}
	if app.Status != "active" && app.Status != "completed" {
		app.Stage = "paused"
		return
	}
	node := recruitmentCurrentNode(app)
	if node == nil {
		app.Stage = "contacting"
		return
	}
	switch node.Type {
	case "interview":
		app.Stage = "interview"
	case "approval", "offer", "onboarding":
		app.Stage = "offer"
	default:
		app.Stage = "contacting"
	}
}

// recruitmentApprovalReady 根据全员会签或任一人通过规则检查节点审核结果。
func recruitmentApprovalReady(node RecruitmentNodeInstance) bool {
	if len(node.ApproverEmails) == 0 {
		return true
	}
	approved := 0
	for _, email := range node.ApproverEmails {
		for _, item := range node.Approvals {
			if strings.EqualFold(item.AuthorEmail, email) && item.Decision == "approved" {
				approved++
			}
		}
	}
	if node.ApprovalMode == "any" {
		return approved > 0
	}
	return approved == len(node.ApproverEmails)
}

// recruitmentAdvanceReady 检查本轮场次完成、所需面评及节点审核，旧轮次不能满足新轮次。
func recruitmentAdvanceReady(app RecruitmentApplication, node RecruitmentNodeInstance) error {
	if node.Type == "interview" {
		count := 0
		for _, interview := range app.Interviews {
			if interview.NodeID != node.ID || interview.Attempt != node.Attempt || interview.Status == "cancelled" {
				continue
			}
			count++
			if interview.Status != "completed" {
				return recruitmentInputError("请先完成这一轮的面试场次")
			}
			if node.RequiresFeedback {
				for _, email := range interview.InterviewerEmails {
					found := false
					for _, item := range interview.Feedback {
						if strings.EqualFold(item.AuthorEmail, email) {
							found = true
						}
					}
					if !found {
						return recruitmentInputError("还有面试官没提交面评，请等评价齐了再推进")
					}
				}
			}
		}
		if count == 0 {
			return recruitmentInputError("请先安排并完成至少一场面试")
		}
	}
	if !recruitmentApprovalReady(node) {
		return recruitmentInputError("这个节点还有审核没通过，请先处理审核")
	}
	if node.Type == "onboarding" && app.EntryDate == "" {
		return recruitmentInputError("请先填写实际入职日期")
	}
	if node.Type == "onboarding" && app.EntryDate > time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02") {
		return recruitmentInputError("入职日期还没到，请到实际入职后再确认")
	}
	return nil
}

// recruitmentInterviewIndex 查找场次在实例中的位置，不接收其他申请场次。
func recruitmentInterviewIndex(app *RecruitmentApplication, id string) int {
	for i, item := range app.Interviews {
		if item.ID == id {
			return i
		}
	}
	return -1
}

// recruitmentScheduleFields 校验和整理排期输入，视频地址仅允许HTTP或HTTPS。
func (s *RecruitmentService) recruitmentScheduleFields(scope CandidateLibraryScope, input RecruitmentAction, node RecruitmentNodeInstance) (RecruitmentInterview, error) {
	if input.StartsAt.IsZero() || input.StartsAt.Before(time.Now().Add(-time.Minute)) || input.StartsAt.After(time.Now().AddDate(2, 0, 0)) || input.DurationMinutes < 5 || input.DurationMinutes > 480 {
		return RecruitmentInterview{}, recruitmentInputError("请选择未来面试时间，时长需在5到480分钟内")
	}
	emails := recruitmentEmails(input.InterviewerEmails)
	if len(emails) == 0 {
		emails = node.InterviewerEmails
	}
	if len(emails) == 0 {
		return RecruitmentInterview{}, recruitmentInputError("请至少选择一位面试官")
	}
	if err := s.validatePeople(scope.TenantID, "", emails); err != nil {
		return RecruitmentInterview{}, err
	}
	if input.Mode != "onsite" && input.Mode != "video" && input.Mode != "phone" {
		return RecruitmentInterview{}, recruitmentInputError("请选择现场、视频或电话面试")
	}
	location := strings.TrimSpace(input.Location)
	meeting := strings.TrimSpace(input.MeetingURL)
	if len([]rune(location)) > 500 || len(meeting) > 2000 {
		return RecruitmentInterview{}, ErrRecruitmentInvalid
	}
	if input.Mode == "onsite" && location == "" {
		return RecruitmentInterview{}, recruitmentInputError("现场面试请填写地点")
	}
	if input.Mode == "video" && meeting == "" {
		return RecruitmentInterview{}, recruitmentInputError("请填写完整的视频会议链接")
	}
	if meeting != "" {
		parsed, err := url.Parse(meeting)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return RecruitmentInterview{}, recruitmentInputError("请填写完整的视频会议链接")
		}
	}
	return RecruitmentInterview{ID: recruitmentID(), NodeID: node.ID, Attempt: node.Attempt, StartsAt: input.StartsAt.UTC(), DurationMinutes: input.DurationMinutes, Mode: input.Mode, Location: location, MeetingURL: meeting, InterviewerEmails: emails, Status: "scheduled", Confirmation: "pending", Revision: 1, Feedback: []RecruitmentFeedback{}}, nil
}

// Act 原子执行推进、退回、排期、面评和审核，参与者只能处理自己被指派的任务。
func (s *RecruitmentService) Act(scope CandidateLibraryScope, id string, input RecruitmentAction) (RecruitmentApplication, error) {
	if err := s.validatePeople(scope.TenantID, scope.UserEmail); err != nil {
		return RecruitmentApplication{}, ErrRecruitmentAccess
	}
	if input.Revision < 1 || len([]rune(input.Note)) > 2000 || len([]rune(input.Content)) > 4000 {
		return RecruitmentApplication{}, ErrRecruitmentInvalid
	}
	app, err := s.store.MutateApplication(scope.TenantID, id, input.Revision, func(app *RecruitmentApplication) error {
		if !recruitmentCanView(scope, *app) {
			return ErrNotFound
		}
		if input.Action == "read_notifications" {
			for i := range app.Events {
				if recruitmentContains(app.Events[i].NotifyEmails, scope.UserEmail) {
					app.Events[i].ReadBy = recruitmentEmails(append(app.Events[i].ReadBy, scope.UserEmail))
				}
			}
			return nil
		}
		if app.Status != "active" {
			return recruitmentInputError("这条流程已结束，可以从简历重新启用新流程")
		}
		node := recruitmentCurrentNode(app)
		if node == nil {
			return ErrRecruitmentInvalid
		}
		manage := recruitmentCanManage(scope, *app)
		if input.Action != "feedback" && input.Action != "approve" && !manage {
			return ErrRecruitmentAccess
		}
		if input.NodeID != "" && input.Action != "assign" && input.NodeID != node.ID {
			return recruitmentInputError("请先切换到当前流程节点再处理")
		}
		switch input.Action {
		case "advance":
			if err := recruitmentAdvanceReady(*app, *node); err != nil {
				return err
			}
			node.Status = "completed"
			recruitmentAppendEvent(app, scope.UserEmail, "advanced", input.Note, "")
			if app.CurrentNode+1 == len(app.Nodes) {
				app.Status = "completed"
			} else {
				app.CurrentNode++
				app.Nodes[app.CurrentNode].Status = "current"
				recruitmentAppendEvent(app, scope.UserEmail, "node_entered", app.Nodes[app.CurrentNode].Name, "")
			}
		case "return":
			if input.TargetNodeIndex < 0 || input.TargetNodeIndex >= app.CurrentNode || strings.TrimSpace(input.Note) == "" {
				return recruitmentInputError("请选择之前的节点，并填写退回原因")
			}
			recruitmentAppendEvent(app, scope.UserEmail, "returned", "退回到"+app.Nodes[input.TargetNodeIndex].Name+"："+input.Note, "")
			for i := input.TargetNodeIndex; i < len(app.Nodes); i++ {
				app.Nodes[i].Status = "pending"
				app.Nodes[i].Attempt++
				app.Nodes[i].Approvals = []RecruitmentDecision{}
			}
			for i := range app.Interviews {
				if app.Interviews[i].Status == "scheduled" {
					app.Interviews[i].Status = "cancelled"
					app.Interviews[i].Revision++
				}
			}
			app.CurrentNode = input.TargetNodeIndex
			app.Nodes[app.CurrentNode].Status = "current"
			recruitmentAppendEvent(app, scope.UserEmail, "node_entered", "重新进入"+app.Nodes[app.CurrentNode].Name, "")
		case "end":
			if (input.Status != "rejected" && input.Status != "withdrawn") || strings.TrimSpace(input.Note) == "" {
				return recruitmentInputError("请选择淘汰或撤回，并填写原因")
			}
			app.Status = input.Status
			for i := range app.Interviews {
				if app.Interviews[i].Status == "scheduled" {
					app.Interviews[i].Status = "cancelled"
					app.Interviews[i].Revision++
				}
			}
			recruitmentAppendEvent(app, scope.UserEmail, "ended", input.Status+"："+input.Note, "")
		case "assign":
			if !scope.IsAdmin && !strings.EqualFold(app.OwnerEmail, scope.UserEmail) {
				return ErrRecruitmentAccess
			}
			target := -1
			for i, n := range app.Nodes {
				if n.ID == input.NodeID {
					target = i
				}
			}
			if target < app.CurrentNode {
				return recruitmentInputError("已完成节点保留原人员，请调整当前或后续节点")
			}
			if target < 0 {
				return ErrNotFound
			}
			owner := strings.ToLower(strings.TrimSpace(input.OwnerEmail))
			if owner == "" {
				return recruitmentInputError("请选择节点负责人")
			}
			interviewers := recruitmentEmails(input.InterviewerEmails)
			approvers := recruitmentEmails(input.ApproverEmails)
			cc := recruitmentEmails(input.CCEmails)
			if err := s.validatePeople(scope.TenantID, owner, interviewers, approvers, cc); err != nil {
				return err
			}
			targetNode := &app.Nodes[target]
			if input.ApprovalMode != "" {
				if input.ApprovalMode != "all" && input.ApprovalMode != "any" {
					return ErrRecruitmentInvalid
				}
				targetNode.ApprovalMode = input.ApprovalMode
			}
			if targetNode.Type == "approval" && len(approvers) == 0 {
				approvers = []string{owner}
			}
			targetNode.OwnerEmail = owner
			targetNode.InterviewerEmails = interviewers
			targetNode.ApproverEmails = approvers
			targetNode.CCEmails = cc
			targetNode.Approvals = []RecruitmentDecision{}
			recruitmentAppendEvent(app, scope.UserEmail, "assigned", "更新节点人员："+targetNode.Name, "")
		case "schedule", "reschedule":
			if node.Type != "interview" {
				return recruitmentInputError("请在面试节点安排面试")
			}
			interview, err := s.recruitmentScheduleFields(scope, input, *node)
			if err != nil {
				return err
			}
			localTime := interview.StartsAt.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04")
			note := fmt.Sprintf("%s，%d分钟，%s", localTime, interview.DurationMinutes, interview.Mode)
			if input.Action == "schedule" {
				app.Interviews = append(app.Interviews, interview)
			} else {
				index := recruitmentInterviewIndex(app, input.InterviewID)
				if index < 0 {
					return ErrNotFound
				}
				old := app.Interviews[index]
				if old.NodeID != node.ID || old.Attempt != node.Attempt || old.Status != "scheduled" {
					return recruitmentInputError("只能修改当前轮次尚未完成的场次")
				}
				interview.ID = old.ID
				interview.Revision = old.Revision + 1
				note = old.StartsAt.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04") + " → " + note
				app.Interviews[index] = interview
			}
			if strings.TrimSpace(input.Note) != "" {
				note += "；" + input.Note
			}
			recruitmentAppendEvent(app, scope.UserEmail, input.Action, note, interview.ID)
		case "assign_interviewers":
			index := recruitmentInterviewIndex(app, input.InterviewID)
			if index < 0 {
				return ErrNotFound
			}
			interview := &app.Interviews[index]
			if interview.NodeID != node.ID || interview.Attempt != node.Attempt || interview.Status == "cancelled" {
				return recruitmentInputError("请转交当前轮次尚有效的面试场次")
			}
			emails := recruitmentEmails(input.InterviewerEmails)
			if len(emails) == 0 {
				return recruitmentInputError("请至少选择一位面试官")
			}
			if err := s.validatePeople(scope.TenantID, "", emails); err != nil {
				return err
			}
			old := strings.Join(interview.InterviewerEmails, "、")
			interview.InterviewerEmails = emails
			interview.Revision++
			recruitmentAppendEvent(app, scope.UserEmail, "interviewers_assigned", old+" → "+strings.Join(emails, "、"), interview.ID)
		case "cancel_interview", "complete_interview":
			index := recruitmentInterviewIndex(app, input.InterviewID)
			if index < 0 {
				return ErrNotFound
			}
			interview := &app.Interviews[index]
			if interview.NodeID != node.ID || interview.Attempt != node.Attempt || interview.Status != "scheduled" {
				return recruitmentInputError("这场面试已处理，请刷新后查看")
			}
			if input.Action == "complete_interview" {
				if interview.StartsAt.After(time.Now()) {
					return recruitmentInputError("面试还没开始，请到面试后再标记完成")
				}
				interview.Status = "completed"
			} else {
				interview.Status = "cancelled"
			}
			interview.Revision++
			recruitmentAppendEvent(app, scope.UserEmail, input.Action, input.Note, interview.ID)
		case "feedback":
			index := recruitmentInterviewIndex(app, input.InterviewID)
			if index < 0 {
				return ErrNotFound
			}
			interview := &app.Interviews[index]
			if interview.NodeID != node.ID || interview.Attempt != node.Attempt || interview.Status == "cancelled" || !recruitmentContains(interview.InterviewerEmails, scope.UserEmail) {
				return ErrRecruitmentAccess
			}
			if interview.StartsAt.After(time.Now()) {
				return recruitmentInputError("面试还没开始，先等面试结束再记评价")
			}
			if (input.Conclusion != "pass" && input.Conclusion != "fail" && input.Conclusion != "hold") || strings.TrimSpace(input.Content) == "" {
				return recruitmentInputError("请选择面评结论，并填写评价")
			}
			feedback := RecruitmentFeedback{AuthorEmail: scope.UserEmail, Conclusion: input.Conclusion, Content: strings.TrimSpace(input.Content), CreatedAt: time.Now().UTC()}
			found := false
			for i, item := range interview.Feedback {
				if strings.EqualFold(item.AuthorEmail, scope.UserEmail) {
					interview.Feedback[i] = feedback
					found = true
				}
			}
			if !found {
				interview.Feedback = append(interview.Feedback, feedback)
			}
			recruitmentAppendEvent(app, scope.UserEmail, "feedback", input.Conclusion+"："+input.Content, interview.ID)
		case "approve":
			if !recruitmentContains(node.ApproverEmails, scope.UserEmail) {
				return ErrRecruitmentAccess
			}
			if input.Decision != "approved" && input.Decision != "rejected" {
				return ErrRecruitmentInvalid
			}
			if input.Decision == "rejected" && strings.TrimSpace(input.Content) == "" {
				return recruitmentInputError("审核未通过时请填写原因")
			}
			decision := RecruitmentDecision{AuthorEmail: scope.UserEmail, Decision: input.Decision, Content: input.Content, CreatedAt: time.Now().UTC()}
			found := false
			for i, item := range node.Approvals {
				if strings.EqualFold(item.AuthorEmail, scope.UserEmail) {
					node.Approvals[i] = decision
					found = true
				}
			}
			if !found {
				node.Approvals = append(node.Approvals, decision)
			}
			recruitmentAppendEvent(app, scope.UserEmail, "approval", input.Decision+"："+input.Content, "")
		case "set_entry_date":
			date, err := time.Parse("2006-01-02", input.EntryDate)
			if err != nil || date.Year() < 2000 || date.Year() > 2100 {
				return recruitmentInputError("请填写有效入职日期")
			}
			app.EntryDate = input.EntryDate
			recruitmentAppendEvent(app, scope.UserEmail, "entry_date", input.EntryDate, "")
		default:
			return ErrRecruitmentInvalid
		}
		recruitmentMapStage(app)
		return nil
	})
	if err != nil {
		return RecruitmentApplication{}, err
	}
	app.Permissions = recruitmentPermissions(scope, app)
	return app, nil
}

// MailContext 返回可信的发件上下文，仅允许负责人或管理员发送该申请邮件。
func (s *RecruitmentService) MailContext(scope CandidateLibraryScope, applicationID, interviewID string) (RecruitmentMailContext, error) {
	app, err := s.GetApplication(scope, applicationID)
	if err != nil {
		return RecruitmentMailContext{}, err
	}
	if !recruitmentCanManage(scope, app) {
		return RecruitmentMailContext{}, ErrRecruitmentAccess
	}
	node := recruitmentCurrentNode(&app)
	ctx := RecruitmentMailContext{ApplicationID: app.ID, CandidateEmail: app.CandidateEmail, CandidateName: app.CandidateName, PositionName: app.PositionName, EntryDate: app.EntryDate, ApplicationRevision: app.Revision, ApplicationStatus: app.Status}
	if s.candidates != nil {
		candidate, readErr := s.candidates.GetPositionCandidate(scope.TenantID, app.CandidateID, "", scope.UserEmail, true)
		if readErr != nil {
			if errors.Is(readErr, ErrNotFound) {
				return RecruitmentMailContext{}, recruitmentInputError("这份简历已删除，暂时不能向旧地址发送新邮件，请先重新添加候选人资料")
			}
			return RecruitmentMailContext{}, readErr
		}
		ctx.CandidateEmail = candidate.Email
		ctx.CandidateName = candidate.CandidateName
	}
	if node != nil {
		ctx.NodeName = node.Name
	}
	if interviewID != "" {
		index := recruitmentInterviewIndex(&app, interviewID)
		if index < 0 {
			return ctx, ErrNotFound
		}
		interview := app.Interviews[index]
		ctx.InterviewID = interview.ID
		ctx.InterviewTime = interview.StartsAt
		ctx.Duration = interview.DurationMinutes
		ctx.Mode = interview.Mode
		ctx.Location = interview.Location
		ctx.MeetingURL = interview.MeetingURL
		ctx.InterviewRevision = interview.Revision
		ctx.InterviewStatus = interview.Status
		for _, n := range app.Nodes {
			if n.ID == interview.NodeID {
				ctx.NodeName = n.Name
			}
		}
	}
	return ctx, nil
}

// InvitationToken 生成一场有效面试的随机确认令牌，仅保存哈希且不会替换同版本旧链接。
func (s *RecruitmentService) InvitationToken(scope CandidateLibraryScope, applicationID, interviewID string, revision int64) (string, error) {
	ctx, err := s.MailContext(scope, applicationID, interviewID)
	if err != nil {
		return "", err
	}
	if ctx.InterviewID == "" || ctx.InterviewRevision != revision {
		return "", ErrRecruitmentVersion
	}
	var data [32]byte
	if _, err = rand.Read(data[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(data[:])
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(14 * 24 * time.Hour)
	if after := ctx.InterviewTime.Add(24 * time.Hour); after.Before(expires) {
		expires = after
	}
	if !expires.After(time.Now()) {
		return "", recruitmentInputError("这场面试已过期，请先确认排期")
	}
	err = s.store.SaveInvitation(RecruitmentInvitation{TokenHash: hex.EncodeToString(hash[:]), TenantID: scope.TenantID, ApplicationID: applicationID, InterviewID: interviewID, InterviewRevision: revision, ExpiresAt: expires})
	return token, err
}

// RecordMailEvent 将已持久化的招聘邮件结果追加到申请历史，保留业务追踪信息。
func (s *RecruitmentService) RecordMailEvent(scope CandidateLibraryScope, applicationID, kind, summary string) error {
	for attempt := 0; attempt < 3; attempt++ {
		app, err := s.GetApplication(scope, applicationID)
		if err != nil {
			return err
		}
		if !recruitmentCanManage(scope, app) {
			return ErrRecruitmentAccess
		}
		_, err = s.store.MutateApplication(scope.TenantID, applicationID, app.Revision, func(item *RecruitmentApplication) error {
			recruitmentAppendEvent(item, scope.UserEmail, "mail_"+kind, summary, "")
			return nil
		})
		if !errors.Is(err, ErrRecruitmentVersion) {
			return err
		}
	}
	return ErrRecruitmentVersion
}

// recruitmentInvitationContext 验证链接时效、申请状态和场次版本，旧排期不能继续确认。
func (s *RecruitmentService) recruitmentInvitationContext(token string) (RecruitmentInvitation, RecruitmentApplication, RecruitmentInterview, error) {
	if len(token) != 64 {
		return RecruitmentInvitation{}, RecruitmentApplication{}, RecruitmentInterview{}, ErrNotFound
	}
	if _, err := hex.DecodeString(token); err != nil {
		return RecruitmentInvitation{}, RecruitmentApplication{}, RecruitmentInterview{}, ErrNotFound
	}
	hash := sha256.Sum256([]byte(token))
	inv, err := s.store.FindInvitation(hex.EncodeToString(hash[:]))
	if err != nil {
		return inv, RecruitmentApplication{}, RecruitmentInterview{}, err
	}
	if !inv.ExpiresAt.After(time.Now()) {
		return inv, RecruitmentApplication{}, RecruitmentInterview{}, ErrNotFound
	}
	app, err := s.store.GetApplication(inv.TenantID, inv.ApplicationID)
	if err != nil {
		return inv, app, RecruitmentInterview{}, err
	}
	index := recruitmentInterviewIndex(&app, inv.InterviewID)
	if index < 0 || app.Status != "active" {
		return inv, app, RecruitmentInterview{}, ErrNotFound
	}
	interview := app.Interviews[index]
	if interview.Status != "scheduled" || interview.Revision != inv.InterviewRevision {
		return inv, app, interview, ErrNotFound
	}
	return inv, app, interview, nil
}

// Invitation 向随机链接持有人展示单场邀请并接受确认、改期申请或拒绝，不公开内部意见。
func (s *RecruitmentService) Invitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := strings.TrimPrefix(r.URL.Path, "/api/public/recruitment/invitations/")
	inv, app, interview, err := s.recruitmentInvitationContext(token)
	if err != nil {
		writeError(w, http.StatusNotFound, "这条邀请已失效，请联系招聘负责人获取最新安排")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"ok": true, "invitation": map[string]any{"candidate_name": app.CandidateName, "position_name": app.PositionName, "starts_at": interview.StartsAt, "duration_minutes": interview.DurationMinutes, "mode": interview.Mode, "location": interview.Location, "meeting_url": interview.MeetingURL, "confirmation": interview.Confirmation, "expires_at": inv.ExpiresAt}})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err = recruitmentDecode(w, r, &input); err != nil {
		writeRecruitmentError(w, err)
		return
	}
	confirmation := ""
	switch input.Action {
	case "confirm":
		confirmation = "confirmed"
	case "reschedule":
		confirmation = "reschedule_requested"
	case "decline":
		confirmation = "declined"
	default:
		writeRecruitmentError(w, ErrRecruitmentInvalid)
		return
	}
	if len([]rune(input.Note)) > 1000 {
		writeRecruitmentError(w, ErrRecruitmentInvalid)
		return
	}
	_, err = s.store.MutateApplication(inv.TenantID, inv.ApplicationID, app.Revision, func(item *RecruitmentApplication) error {
		index := recruitmentInterviewIndex(item, inv.InterviewID)
		if index < 0 || item.Status != "active" || item.Interviews[index].Revision != inv.InterviewRevision || item.Interviews[index].Status != "scheduled" {
			return ErrNotFound
		}
		item.Interviews[index].Confirmation = confirmation
		item.Interviews[index].ConfirmationNote = input.Note
		recruitmentAppendEvent(item, "candidate", "candidate_"+confirmation, input.Note, inv.InterviewID)
		return nil
	})
	if err != nil {
		writeRecruitmentError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "confirmation": confirmation, "message": "回复已记好，招聘负责人会看到。"})
}
