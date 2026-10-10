// 本文件提供团队公司信息、招聘邮件模板、预览、授权发送和历史查询的HTTP接口。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// RecruitmentMailService 将团队模板、受限业务上下文和平台邮件传输组成招聘通知服务。
type RecruitmentMailService struct {
	auth        *AuthService
	store       RecruitmentMailStore
	tenants     TenantStore
	mailer      Mailer
	recruitment *RecruitmentService
}

// NewRecruitmentMailService 创建招聘邮件服务，不自动发送或启动后台任务。
func NewRecruitmentMailService(auth *AuthService, store RecruitmentMailStore, tenants TenantStore, mailer Mailer, recruitment *RecruitmentService) *RecruitmentMailService {
	return &RecruitmentMailService{auth: auth, store: store, tenants: tenants, mailer: mailer, recruitment: recruitment}
}

// recruitmentMailInput 使用可选正文覆盖区分未修改和明确清空，实际发送不接受客户端收件人。
type recruitmentMailInput struct {
	ContextFingerprint string  `json:"context_fingerprint,omitempty"`
	ApplicationID      string  `json:"application_id"`
	InterviewID        string  `json:"interview_id"`
	TemplateID         string  `json:"template_id"`
	Subject            *string `json:"subject,omitempty"`
	Body               *string `json:"body,omitempty"`
	IdempotencyKey     string  `json:"idempotency_key"`
}

// recruitmentMailPreviewInput 只为预览开放示例变量，真实发送使用不同的严格请求合同。
type recruitmentMailPreviewInput struct {
	recruitmentMailInput
	Context map[string]string `json:"context,omitempty"`
	Example bool              `json:"example"`
}

// recruitmentMailReadJSON 限制大小、拒绝未知字段和尾随内容，避免越权字段被悄悄忽略。
func recruitmentMailReadJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 160*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("内容没读完整，请检查后再试")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("请只提交一份完整内容")
	}
	return nil
}

// scope 复用招聘流程认证与团队范围，并检查邮件存储是否就绪。
func (s *RecruitmentMailService) scope(w http.ResponseWriter, r *http.Request) (CandidateLibraryScope, bool) {
	if s == nil || s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "招聘邮件还没准备好")
		return CandidateLibraryScope{}, false
	}
	return recruitmentRequestScope(s.auth, s.tenants, w, r)
}

// requireAdmin 限制公司与模板维护到当前团队管理员。
func (s *RecruitmentMailService) requireAdmin(w http.ResponseWriter, scope CandidateLibraryScope) bool {
	if !scope.IsAdmin {
		writeError(w, http.StatusForbidden, "公司信息和邮件模板需要团队管理员维护")
		return false
	}
	return true
}

// writeRecruitmentMailError 使用稳定HTTP状态返回可执行的招聘邮件提示。
func writeRecruitmentMailError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "这条记录暂时无法访问")
	case errors.Is(err, ErrRecruitmentMailConflict):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error(), "code": "recruitment_send_key_conflict"})
	case errors.Is(err, ErrRecruitmentVersion):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrRecruitmentAccess):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrRecruitmentInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		if strings.HasPrefix(err.Error(), "招聘：") {
			writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "招聘："))
			return
		}
		log.Print("GoodHR recruitment mail storage or context failed")
		writeError(w, http.StatusInternalServerError, "招聘邮件暂时没处理好，请稍后再试")
	}
}

// validateRecruitmentCompany 规范必填署名与联系邮箱，并阻止邮件头换行注入。
func validateRecruitmentCompany(item *RecruitmentCompany) error {
	item.CompanyName = strings.TrimSpace(item.CompanyName)
	item.ContactName = strings.TrimSpace(item.ContactName)
	item.ContactPhone = strings.TrimSpace(item.ContactPhone)
	item.ContactEmail = strings.TrimSpace(item.ContactEmail)
	item.CompanyAddress = strings.TrimSpace(item.CompanyAddress)
	if item.CompanyName == "" || item.ContactName == "" || item.ContactEmail == "" {
		return errors.New("请先填写公司名称、招聘联系人和联系邮箱")
	}
	if len([]rune(item.CompanyName)) > 100 || len([]rune(item.ContactName)) > 100 || len([]rune(item.ContactPhone)) > 100 || len([]rune(item.CompanyAddress)) > 500 {
		return errors.New("公司信息有些长，请稍微缩短一下")
	}
	for _, value := range []string{item.CompanyName, item.ContactName, item.ContactPhone, item.ContactEmail, item.CompanyAddress} {
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("公司信息请填写单行内容")
		}
	}
	email, valid := normalizeEmail(item.ContactEmail)
	if !valid {
		return errors.New("联系邮箱看起来没填完整")
	}
	item.ContactEmail = email
	return nil
}

// Company 读取团队公司配置，管理员可保存后用于邮件署名和回复地址。
func (s *RecruitmentMailService) Company(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPut {
		if !s.requireAdmin(w, scope) {
			return
		}
		var item RecruitmentCompany
		if err := recruitmentMailReadJSON(w, r, &item); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateRecruitmentCompany(&item); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.SaveCompany(scope.TenantID, item); err != nil {
			writeRecruitmentMailError(w, err)
			return
		}
	}
	item, err := s.store.GetCompany(scope.TenantID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "company": item, "can_manage": scope.IsAdmin})
}

// validateRecruitmentMailTemplate 保留纯文本模板并限制分类、标题和正文长度。
func validateRecruitmentMailTemplate(item *RecruitmentMailTemplate) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Category = strings.TrimSpace(item.Category)
	item.Subject = strings.TrimSpace(item.Subject)
	item.Body = strings.TrimSpace(item.Body)
	if item.Category == "" {
		item.Category = "custom"
	}
	if item.Name == "" || item.Subject == "" || item.Body == "" {
		return errors.New("模板名称、邮件标题和正文都要填一下")
	}
	if len([]rune(item.Name)) > 100 || len([]rune(item.Subject)) > 300 || len([]rune(item.Body)) > 60000 {
		return errors.New("模板内容有些长，请缩短后再保存")
	}
	if strings.ContainsAny(item.Subject, "\r\n") {
		return errors.New("邮件标题不能换行")
	}
	switch item.Category {
	case "interview_first", "interview_followup", "reschedule", "cancel", "result", "offer", "onboarding", "custom":
	default:
		return errors.New("请选择已有的模板场景")
	}
	return nil
}

// Templates 列出团队模板或由管理员新建，预置内容仅在团队首次访问时初始化。
func (s *RecruitmentMailService) Templates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		if !s.requireAdmin(w, scope) {
			return
		}
		var item RecruitmentMailTemplate
		if err := recruitmentMailReadJSON(w, r, &item); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := validateRecruitmentMailTemplate(&item); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		saved, err := s.store.SaveTemplate(scope.TenantID, item, true)
		if err != nil {
			writeRecruitmentMailError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "template": saved})
		return
	}
	items, err := s.store.ListTemplates(scope.TenantID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "templates": items, "can_manage": scope.IsAdmin})
}

// TemplateDetail 编辑或删除本团队模板，发送历史始终保留原正文。
func (s *RecruitmentMailService) TemplateDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok || !s.requireAdmin(w, scope) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/recruitment/mail-templates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "模板编号没认出来")
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.store.DeleteTemplate(scope.TenantID, id); err != nil {
			writeRecruitmentMailError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	var item RecruitmentMailTemplate
	if err := recruitmentMailReadJSON(w, r, &item); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item.ID = id
	if err := validateRecruitmentMailTemplate(&item); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.SaveTemplate(scope.TenantID, item, false)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "template": saved})
}

// Placeholders 返回可点击插入的中文占位符目录及明确示例。
func (s *RecruitmentMailService) Placeholders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if _, ok := s.scope(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "placeholders": recruitmentMailPlaceholders()})
}

// mailText 解析本团队模板和可选纯文本覆盖，不读取客户端提供的邮件变量。
func (s *RecruitmentMailService) mailText(tenantID string, input recruitmentMailInput) (string, string, error) {
	var subject, body string
	if input.TemplateID != "" {
		item, err := s.store.GetTemplate(tenantID, input.TemplateID)
		if err != nil {
			return "", "", err
		}
		subject, body = item.Subject, item.Body
	}
	if input.Subject != nil {
		subject = *input.Subject
	}
	if input.Body != nil {
		body = *input.Body
	}
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(body) == "" || len([]rune(subject)) > 300 || len([]rune(body)) > 60000 || strings.ContainsAny(subject, "\r\n") {
		return "", "", ErrRecruitmentInvalid
	}
	return subject, body, nil
}

// mailContext 只允许招聘负责人、当前节点负责人或管理员读取可信发送上下文。
func (s *RecruitmentMailService) mailContext(scope CandidateLibraryScope, input recruitmentMailInput) (RecruitmentMailContext, error) {
	if s.recruitment == nil {
		return RecruitmentMailContext{}, errors.New("招聘流程还没准备好")
	}
	if strings.TrimSpace(input.ApplicationID) == "" {
		return RecruitmentMailContext{}, ErrRecruitmentInvalid
	}
	return s.recruitment.MailContext(scope, input.ApplicationID, input.InterviewID)
}

// Preview 渲染邮件预览；浏览器示例变量不会被真实发送接口接受。
func (s *RecruitmentMailService) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok {
		return
	}
	var input recruitmentMailPreviewInput
	if err := recruitmentMailReadJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.Context) > 32 {
		writeError(w, http.StatusBadRequest, "示例变量有些多，请按占位符目录填写")
		return
	}
	for key, value := range input.Context {
		if len([]rune(key)) > 100 || len([]rune(value)) > 2000 {
			writeError(w, http.StatusBadRequest, "示例变量内容有些长，请缩短后再预览")
			return
		}
	}
	subject, body, err := s.mailText(scope.TenantID, input.recruitmentMailInput)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	company, err := s.store.GetCompany(scope.TenantID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	example := input.ApplicationID == "" || input.Example || len(input.Context) > 0
	values := recruitmentMailExamples()
	var actual RecruitmentMailContext
	if input.ApplicationID != "" {
		ctx, err := s.mailContext(scope, input.recruitmentMailInput)
		if err != nil {
			writeRecruitmentMailError(w, err)
			return
		}
		if input.InterviewID != "" {
			ctx.ConfirmationURL = "确认链接会在发送时生成"
		}
		actual = ctx
		values = recruitmentMailValues(company, ctx, time.Now())
	} else {
		for key, value := range recruitmentMailValues(company, RecruitmentMailContext{}, time.Now()) {
			if value != "" {
				values[key] = value
			}
		}
	}
	for key, value := range input.Context {
		values[key] = value
	}
	preview := renderRecruitmentMail(subject, body, values, example)
	preview.ReplyTo = company.ContactEmail
	if input.ApplicationID != "" {
		preview.Recipient = actual.CandidateEmail
	} else {
		preview.Recipient = "candidate@example.com"
	}
	if !example {
		addRecruitmentMailCompanyRequirements(&preview, company)
		if companyErr := validateRecruitmentCompany(&company); companyErr != nil {
			preview.Warnings = append(preview.Warnings, companyErr.Error())
		}
		if _, valid := normalizeEmail(actual.CandidateEmail); !valid || strings.ContainsAny(actual.CandidateEmail, "\r\n") {
			preview.Warnings = append(preview.Warnings, "这份简历还没有有效邮箱，请先补充后再邀请")
		}
		if !recruitmentMailTransportConfigured(s.mailer) {
			preview.Warnings = append(preview.Warnings, "平台邮件发送还没配置好，当前只能预览")
		}
		if input.TemplateID != "" {
			template, templateErr := s.store.GetTemplate(scope.TenantID, input.TemplateID)
			if templateErr != nil {
				writeRecruitmentMailError(w, templateErr)
				return
			}
			if sceneErr := validateRecruitmentMailScene(template.Category, actual); sceneErr != nil {
				preview.Warnings = append(preview.Warnings, sceneErr.Error())
			}
		}
		if strings.Contains(subject+body, "{确认链接}") {
			if sceneErr := validateRecruitmentMailScene("interview_first", actual); sceneErr != nil {
				preview.Warnings = append(preview.Warnings, sceneErr.Error())
			}
		}
		if len(preview.Warnings) > 0 {
			preview.CanSend = false
		}
		preview.ContextFingerprint = recruitmentMailContextFingerprint(company, actual, subject, body)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "preview": preview})
}

// addRecruitmentMailCompanyRequirements 强制公司署名、联系人和回复邮箱先配置完整。
func addRecruitmentMailCompanyRequirements(preview *RecruitmentMailPreview, company RecruitmentCompany) {
	for key, value := range map[string]string{"公司名称": company.CompanyName, "招聘联系人": company.ContactName, "联系邮箱": company.ContactEmail} {
		if strings.TrimSpace(value) == "" {
			exists := false
			for _, missing := range preview.MissingPlaceholders {
				if missing == key {
					exists = true
				}
			}
			if !exists {
				preview.MissingPlaceholders = append(preview.MissingPlaceholders, key)
			}
		}
	}
	if len(preview.MissingPlaceholders) > 0 {
		preview.CanSend = false
	}
}

// recruitmentMailFingerprint 对原请求计算指纹，时间和之后改动的模板不会改变已发送快照。
func recruitmentMailFingerprint(input recruitmentMailInput) string {
	input.IdempotencyKey = ""
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// recruitmentMailContextFingerprint 锁定HR实际预览的署名、可信人员场次和模板内容，随机确认链接不参与比较。
func recruitmentMailContextFingerprint(company RecruitmentCompany, ctx RecruitmentMailContext, subject, body string) string {
	ctx.ConfirmationURL = ""
	company.CompanyName = strings.TrimSpace(company.CompanyName)
	company.ContactName = strings.TrimSpace(company.ContactName)
	company.ContactPhone = strings.TrimSpace(company.ContactPhone)
	company.ContactEmail = strings.ToLower(strings.TrimSpace(company.ContactEmail))
	company.CompanyAddress = strings.TrimSpace(company.CompanyAddress)
	raw, _ := json.Marshal(struct {
		Company       RecruitmentCompany
		Context       RecruitmentMailContext
		Subject, Body string
	}{company, ctx, subject, body})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Send 从服务端读取收件人与场次，先抢占和保存正文，再用平台SMTP发送一次。
func (s *RecruitmentMailService) Send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok {
		return
	}
	var input recruitmentMailInput
	if err := recruitmentMailReadJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || strings.ContainsAny(input.IdempotencyKey, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "请提供这次发送的唯一编号")
		return
	}
	if s.recruitment == nil {
		writeError(w, http.StatusServiceUnavailable, "招聘流程还没准备好")
		return
	}
	app, err := s.recruitment.GetApplication(scope, input.ApplicationID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	if !recruitmentCanManage(scope, app) {
		writeRecruitmentMailError(w, ErrRecruitmentAccess)
		return
	}
	fingerprint := recruitmentMailFingerprint(input)
	old, found, err := s.store.FindDelivery(scope.TenantID, scope.UserEmail, input.IdempotencyKey)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	if found {
		if old.Fingerprint != fingerprint {
			writeRecruitmentMailError(w, ErrRecruitmentMailConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "delivery": old, "replayed": true})
		return
	}
	ctx, err := s.mailContext(scope, input)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	company, err := s.store.GetCompany(scope.TenantID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	subject, body, err := s.mailText(scope.TenantID, input)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	contextFingerprint := recruitmentMailContextFingerprint(company, ctx, subject, body)
	if input.ContextFingerprint == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "请先预览这封邮件，再确认发送", "code": "recruitment_preview_required"})
		return
	}
	if input.ContextFingerprint != contextFingerprint {
		// 并发原请求可能已在读取上下文期间完成发送，先回放同编号快照，避免被发送事件的新修订拦住。
		old, found, lookupErr := s.store.FindDelivery(scope.TenantID, scope.UserEmail, input.IdempotencyKey)
		if lookupErr != nil {
			writeRecruitmentMailError(w, lookupErr)
			return
		}
		if found {
			if old.Fingerprint != fingerprint {
				writeRecruitmentMailError(w, ErrRecruitmentMailConflict)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "delivery": old, "replayed": true})
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "收件人、公司信息或面试安排在预览后有变化，请重新预览再确认发送", "code": "recruitment_preview_changed"})
		return
	}
	if err = validateRecruitmentCompany(&company); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	recipient, valid := normalizeEmail(ctx.CandidateEmail)
	if !valid || strings.ContainsAny(ctx.CandidateEmail, "\r\n") {
		writeError(w, http.StatusBadRequest, "这份简历还没有有效邮箱，请先补充后再邀请")
		return
	}
	if input.TemplateID != "" {
		template, sceneErr := s.store.GetTemplate(scope.TenantID, input.TemplateID)
		if sceneErr != nil {
			writeRecruitmentMailError(w, sceneErr)
			return
		}
		if sceneErr = validateRecruitmentMailScene(template.Category, ctx); sceneErr != nil {
			writeError(w, http.StatusBadRequest, sceneErr.Error())
			return
		}
	}
	needsConfirmation := strings.Contains(subject+body, "{确认链接}")
	if needsConfirmation {
		if sceneErr := validateRecruitmentMailScene("interview_first", ctx); sceneErr != nil {
			writeError(w, http.StatusBadRequest, sceneErr.Error())
			return
		}
		if ctx.InterviewID == "" {
			writeError(w, http.StatusBadRequest, "这封邮件使用确认链接，请先选择面试场次")
			return
		}
		ctx.ConfirmationURL = "确认链接会在发送时生成"
	}
	preview := renderRecruitmentMail(subject, body, recruitmentMailValues(company, ctx, time.Now()), false)
	preview.ContextFingerprint = contextFingerprint
	preview.Recipient = recipient
	preview.ReplyTo = company.ContactEmail
	addRecruitmentMailCompanyRequirements(&preview, company)
	if !preview.CanSend {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "邮件还有变量没填完整，请看预览提示", "preview": preview})
		return
	}
	if !recruitmentMailTransportConfigured(s.mailer) {
		writeError(w, http.StatusServiceUnavailable, "平台邮件发送还没配置好，当前只能预览")
		return
	}
	item := RecruitmentMailDelivery{TenantID: scope.TenantID, ActorEmail: scope.UserEmail, ApplicationID: ctx.ApplicationID, InterviewID: ctx.InterviewID, TemplateID: input.TemplateID, Recipient: recipient, ReplyTo: company.ContactEmail, Subject: preview.Subject, Body: preview.Body, HTML: preview.HTML, IdempotencyKey: input.IdempotencyKey, Fingerprint: fingerprint, ApplicationRevision: ctx.ApplicationRevision, InterviewRevision: ctx.InterviewRevision}
	item, claimed, err := s.store.ClaimDelivery(item)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	if !claimed {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "delivery": item, "replayed": true})
		return
	}
	if needsConfirmation {
		token, tokenErr := s.recruitment.InvitationToken(scope, ctx.ApplicationID, ctx.InterviewID, ctx.InterviewRevision)
		if tokenErr != nil {
			s.finishPreparationFailure(w, item, tokenErr)
			return
		}
		baseURL := deploymentPublicBaseURL()
		ctx.ConfirmationURL = strings.TrimRight(baseURL, "/") + "/interview-invitation/" + token
		preview = renderRecruitmentMail(subject, body, recruitmentMailValues(company, ctx, item.CreatedAt), false)
		preview.ContextFingerprint = contextFingerprint
		preview.Recipient = recipient
		preview.ReplyTo = company.ContactEmail
		item.Subject = preview.Subject
		item.Body = preview.Body
		item.HTML = preview.HTML
	}
	item, err = s.store.PrepareDelivery(item)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	latest, contextErr := s.mailContext(scope, input)
	if contextErr != nil {
		s.finishPreparationFailure(w, item, contextErr)
		return
	}
	if latest.ApplicationRevision != ctx.ApplicationRevision || latest.InterviewRevision != ctx.InterviewRevision || latest.CandidateEmail != ctx.CandidateEmail || latest.CandidateName != ctx.CandidateName {
		s.finishPreparationFailure(w, item, ErrRecruitmentVersion)
		return
	}
	sendErr := sendRecruitmentMail(s.mailer, item.Recipient, item.ReplyTo, item.Subject, item.HTML, item.Body)
	status, message := "sent", ""
	if sendErr != nil {
		status = "failed"
		message = "邮件服务暂时没有接受这封邮件，请检查发送记录后再处理"
		if IsMailDeliveryUncertain(sendErr) {
			status = "uncertain"
			message = "邮件服务是否已接收暂时无法确认，请先核实，避免重复邀请"
		}
	}
	saved, finishErr := s.store.FinishDelivery(scope.TenantID, item.ID, status, message)
	if finishErr != nil {
		writeError(w, http.StatusInternalServerError, "发送结果还没记下来，请打开历史核实，先别重复发送")
		return
	}
	if status == "sent" {
		if eventErr := s.recruitment.RecordMailEvent(scope, ctx.ApplicationID, "sent", "招聘邮件已交给邮件服务："+item.Subject); eventErr != nil {
			log.Print("GoodHR recruitment mail sent event save failed")
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "delivery": saved, "preview": preview})
}

// validateRecruitmentMailScene 避免向已取消、结束或过期的场次继续发出面试邀请。
func validateRecruitmentMailScene(category string, ctx RecruitmentMailContext) error {
	switch category {
	case "interview_first", "interview_followup", "reschedule":
		if ctx.InterviewID == "" {
			return errors.New("请先选择已排期的面试场次")
		}
		if ctx.ApplicationStatus != "active" || ctx.InterviewStatus != "scheduled" || !ctx.InterviewTime.After(time.Now()) {
			return errors.New("这场面试已经取消、结束或过期，请刷新排期后再邀请")
		}
	case "cancel":
		if ctx.InterviewID == "" || ctx.InterviewStatus != "cancelled" {
			return errors.New("请先取消所选面试安排，再发送取消通知")
		}
	}
	return nil
}

// finishPreparationFailure 保存未开始SMTP的明确失败，不使同编号重新获得发送权。
func (s *RecruitmentMailService) finishPreparationFailure(w http.ResponseWriter, item RecruitmentMailDelivery, cause error) {
	_, err := s.store.FinishDelivery(item.TenantID, item.ID, "failed", "面试安排已变化，这封邮件没有开始发送，请刷新后再处理")
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	writeRecruitmentMailError(w, cause)
}

// History 先验证应聘记录访问与发送权限，再读取当前团队该记录邮件快照。
func (s *RecruitmentMailService) History(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scope(w, r)
	if !ok {
		return
	}
	applicationID := strings.TrimSpace(r.URL.Query().Get("application_id"))
	if s.recruitment == nil {
		writeError(w, http.StatusServiceUnavailable, "招聘流程还没准备好")
		return
	}
	app, err := s.recruitment.GetApplication(scope, applicationID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	if !recruitmentCanManage(scope, app) {
		writeRecruitmentMailError(w, ErrRecruitmentAccess)
		return
	}
	items, err := s.store.ListDeliveries(scope.TenantID, applicationID)
	if err != nil {
		writeRecruitmentMailError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deliveries": items})
}
