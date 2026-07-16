// 本文件负责超级管理员自定义邮件、批量发送、上传图片和打开追踪。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type AdminEmailService struct {
	auth          *AuthService
	store         EmailCampaignStore
	mailer        Mailer
	systemConfigs SystemConfigStore
}

type sendAdminEmailRequest struct {
	Subject             string            `json:"subject"`
	HTML                string            `json:"html"`
	Mode                string            `json:"mode"`
	Emails              []string          `json:"emails"`
	Tags                []string          `json:"tags"`
	Flows               []string          `json:"flows"`
	LastLoginBeforeDays int               `json:"last_login_before_days"`
	Meta                map[string]string `json:"meta"`
}

type automaticEmailResult struct {
	Job     string         `json:"job"`
	Batches []EmailBatch   `json:"batches"`
	Skipped []string       `json:"skipped"`
	Preview map[string]int `json:"preview,omitempty"`
}

// flowReminderRequest 表示一次外部定时流程提醒任务的筛选和安全参数。
type flowReminderRequest struct {
	Flows        []string `json:"flows"`
	StalledHours int      `json:"stalled_hours"`
	CreatedDay   string   `json:"created_day"`
	Limit        int      `json:"limit"`
	DryRun       bool     `json:"dry_run"`
}

// NewAdminEmailService 创建超管邮件服务。
func NewAdminEmailService(auth *AuthService, store EmailCampaignStore, mailer Mailer, systemConfigs SystemConfigStore) *AdminEmailService {
	return &AdminEmailService{auth: auth, store: store, mailer: mailer, systemConfigs: systemConfigs}
}

// Collection 分发超管邮件批次列表和发送请求。
func (s *AdminEmailService) Collection(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.List(w, r)
	case http.MethodPost:
		s.Send(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// Detail 返回指定邮件批次详情。
func (s *AdminEmailService) Detail(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/emails/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "batch id required")
		return
	}
	batch, recipients, err := s.store.GetBatch(id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "email batch not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load email batch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "batch": batch, "recipients": recipients})
}

// List 返回最近邮件批次。
func (s *AdminEmailService) List(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListBatches(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed load emails")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "batches": items})
}

// Send 创建邮件批次并异步发送。
func (s *AdminEmailService) Send(w http.ResponseWriter, r *http.Request) {
	var req sendAdminEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	session, _ := s.auth.SessionFromRequest(r)
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		writeError(w, http.StatusBadRequest, "subject required")
		return
	}
	html := strings.TrimSpace(req.HTML)
	if html == "" {
		writeError(w, http.StatusBadRequest, "html required")
		return
	}
	emails, summary, err := s.resolveRecipients(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(emails) == 0 {
		writeError(w, http.StatusBadRequest, "没有匹配到收件人")
		return
	}
	cfg := s.recoveryConfig()
	html = appendEmailFooter(html, cfg.Wechat, cfg.Website)
	batch, recipients, err := s.store.CreateBatch(subject, summary, "", session.Email, emails)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed create email batch")
		return
	}
	go s.sendBatch(batch, recipients, html, publicBaseURL(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "batch": batch})
}

// UploadImage 保存富文本图片并返回 URL。
func (s *AdminEmailService) UploadImage(w http.ResponseWriter, r *http.Request) {
	if !s.requireSuperAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "图片太大了，我先接不住")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file required")
		return
	}
	defer file.Close()
	if header.Size > 8<<20 {
		writeError(w, http.StatusBadRequest, "图片太大了，我先接不住")
		return
	}
	ext, ok := safeImageExt(header.Filename, header.Header.Get("Content-Type"))
	if !ok {
		writeError(w, http.StatusBadRequest, "只支持 png、jpg、gif、webp 图片")
		return
	}
	dir := filepath.Join("uploads", "email", time.Now().Format("20060102"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed create upload dir")
		return
	}
	name := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	path := filepath.Join(dir, name)
	dst, err := os.Create(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed save image")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, io.LimitReader(file, 8<<20)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed write image")
		return
	}
	url := "/" + filepath.ToSlash(path)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": url, "absolute_url": publicBaseURL(r) + url})
}

// OpenPixel 标记邮件被打开并返回 1x1 gif。
func (s *AdminEmailService) OpenPixel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id != "" {
		_ = s.store.MarkRecipientOpened(id)
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{71, 73, 70, 56, 57, 97, 1, 0, 1, 0, 128, 0, 0, 255, 255, 255, 0, 0, 0, 33, 249, 4, 1, 0, 0, 0, 0, 44, 0, 0, 0, 0, 1, 0, 1, 0, 0, 2, 2, 68, 1, 0, 59})
}

// PublicJob 处理外部定时任务触发的自动邮件。
// w 为响应对象，r 为请求对象；路径格式为 /api/public/email-jobs/{job}。
func (s *AdminEmailService) PublicJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.validJobToken(r) {
		writeError(w, http.StatusUnauthorized, "token 不对，我先不敢发邮件")
		return
	}
	job := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/public/email-jobs/"), "/")
	if job == "flow-reminder" {
		req, err := flowReminderRequestFromHTTP(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		result, err := s.sendFlowReminder(req, publicBaseURL(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
		return
	}
	result, err := s.SendAutomaticJob(job, publicBaseURL(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

// flowReminderRequestFromHTTP 读取公共流程提醒任务的 GET 查询参数或 POST JSON。
func flowReminderRequestFromHTTP(r *http.Request) (flowReminderRequest, error) {
	req := flowReminderRequest{}
	if r.Method == http.MethodPost && r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return req, errors.New("invalid json body")
		}
	}
	query := r.URL.Query()
	if raw := firstNonEmpty(query.Get("flows"), query.Get("flow")); raw != "" {
		req.Flows = strings.FieldsFunc(raw, func(value rune) bool { return value == ',' || value == '，' || value == ';' || value == '；' })
	}
	if raw := strings.TrimSpace(query.Get("stalled_hours")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return req, errors.New("stalled_hours must be an integer")
		}
		req.StalledHours = value
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return req, errors.New("limit must be an integer")
		}
		req.Limit = value
	}
	if raw := strings.TrimSpace(query.Get("created_day")); raw != "" {
		req.CreatedDay = raw
	}
	if raw := strings.TrimSpace(query.Get("dry_run")); raw != "" {
		req.DryRun = raw == "1" || strings.EqualFold(raw, "true")
	}
	return normalizeFlowReminderRequest(req)
}

// normalizeFlowReminderRequest 校验并补全流程提醒参数，避免一次误发过多邮件。
func normalizeFlowReminderRequest(req flowReminderRequest) (flowReminderRequest, error) {
	if req.StalledHours == 0 {
		req.StalledHours = 24
	}
	if req.StalledHours < 1 || req.StalledHours > 24*365 {
		return req, errors.New("stalled_hours must be between 1 and 8760")
	}
	if req.Limit == 0 {
		req.Limit = 1000
	}
	if req.Limit < 1 || req.Limit > 5000 {
		return req, errors.New("limit must be between 1 and 5000")
	}
	if req.CreatedDay != "" {
		if _, err := time.Parse(time.DateOnly, req.CreatedDay); err != nil {
			return req, errors.New("created_day must use YYYY-MM-DD")
		}
	}
	seen := map[string]bool{}
	flows := make([]string, 0, len(req.Flows))
	for _, flow := range req.Flows {
		flow = strings.TrimSpace(flow)
		if flow == "" || seen[flow] {
			continue
		}
		if userFlowStepIndex(flow) < 0 {
			return req, fmt.Errorf("unsupported flow: %s", flow)
		}
		seen[flow] = true
		flows = append(flows, flow)
	}
	req.Flows = flows
	return req, nil
}

// sendFlowReminder 按当前流程节点分组发送不同教程邮件。
func (s *AdminEmailService) sendFlowReminder(req flowReminderRequest, baseURL string) (automaticEmailResult, error) {
	result := automaticEmailResult{Job: "flow-reminder", Preview: map[string]int{}}
	users, err := s.store.FindTargetUsers(EmailTargetFilter{FlowSteps: req.Flows, CreatedDay: req.CreatedDay, FlowInactiveHours: req.StalledHours})
	if err != nil {
		return result, err
	}
	if !req.DryRun && len(users) > req.Limit {
		return result, fmt.Errorf("matched %d users, exceeds limit %d; use dry_run first and raise limit explicitly", len(users), req.Limit)
	}
	grouped := map[string][]string{}
	for _, user := range users {
		key := flowKey(user.Flow)
		if key != "completed" {
			grouped[key] = append(grouped[key], user.Email)
		}
	}
	cfg := s.recoveryConfig()
	day := time.Now().Format(time.DateOnly)
	for _, key := range userFlowStepOrder {
		emails := grouped[key]
		if len(emails) == 0 {
			continue
		}
		result.Preview[key] = len(emails)
		if req.DryRun {
			continue
		}
		tpl, ok := cfg.Templates[key]
		if !ok || strings.TrimSpace(tpl.Subject) == "" || strings.TrimSpace(tpl.HTML) == "" {
			result.Skipped = append(result.Skipped, key+": template missing")
			continue
		}
		sourceKey := "flow-reminder:" + day + ":" + key
		if exists, _ := s.store.SourceKeyExists(sourceKey); exists {
			result.Skipped = append(result.Skipped, sourceKey)
			continue
		}
		batch, recipients, err := s.store.CreateBatch(tpl.Subject, fmt.Sprintf("流程提醒：%s：停留至少%d小时", key, req.StalledHours), sourceKey, "system", emails)
		if err != nil {
			result.Skipped = append(result.Skipped, err.Error())
			continue
		}
		result.Batches = append(result.Batches, batch)
		go s.sendBatch(batch, recipients, appendEmailFooter(tpl.HTML, cfg.Wechat, cfg.Website), baseURL)
	}
	return result, nil
}

// StartRecoveryScheduler 启动每日自动挽回邮件任务。
func (s *AdminEmailService) StartRecoveryScheduler() {
	go func() {
		for {
			next := nextRecoveryRun(time.Now(), s.recoveryHour())
			time.Sleep(time.Until(next))
			s.SendScheduledRecovery()
		}
	}()
}

// SendAutomaticJob 按任务名发送自动邮件并创建发送记录。
// job 为自动邮件任务名，baseURL 用于追加已读追踪图片。
func (s *AdminEmailService) SendAutomaticJob(job string, baseURL string) (automaticEmailResult, error) {
	job = strings.TrimSpace(job)
	switch job {
	case "yesterday-incomplete":
		return s.sendYesterdayIncomplete(baseURL), nil
	case "inactive-3-days":
		return s.sendInactiveDays(3, baseURL), nil
	case "inactive-7-days":
		return s.sendInactiveDays(7, baseURL), nil
	case "inactive-30-days":
		return s.sendInactiveDays(30, baseURL), nil
	default:
		return automaticEmailResult{}, fmt.Errorf("未知邮件任务：%s", job)
	}
}

// SendScheduledRecovery 给注册满 1、3、7、30 天且仍未完成流程的用户发送定时提醒。
func (s *AdminEmailService) SendScheduledRecovery() {
	cfg := s.recoveryConfig()
	if !cfg.Enabled {
		return
	}
	for _, days := range []int{1, 3, 7, 30} {
		_ = s.sendIncompleteMilestone(days, cfg.Website)
	}
}

// sendYesterdayIncomplete 给昨天注册且流程未完成的用户发送提醒。
// baseURL 用于已读追踪图片，返回创建的邮件批次。
func (s *AdminEmailService) sendYesterdayIncomplete(baseURL string) automaticEmailResult {
	return s.sendIncompleteMilestone(1, baseURL)
}

// sendIncompleteMilestone 给注册满指定天数且流程仍未完成的用户发送分阶段提醒。
// days 为注册后的提醒天数，baseURL 用于已读追踪图片。
func (s *AdminEmailService) sendIncompleteMilestone(days int, baseURL string) automaticEmailResult {
	cfg := s.recoveryConfig()
	if days != 1 && days != 3 && days != 7 && days != 30 {
		return automaticEmailResult{Job: fmt.Sprintf("incomplete-day-%d", days), Skipped: []string{"unsupported milestone"}}
	}
	registeredAt := time.Now().AddDate(0, 0, -days)
	day := registeredAt.Format(time.DateOnly)
	users, err := s.store.FindTargetUsers(EmailTargetFilter{CreatedDay: day})
	result := automaticEmailResult{Job: fmt.Sprintf("incomplete-day-%d", days)}
	if err != nil {
		result.Skipped = append(result.Skipped, err.Error())
		return result
	}
	grouped := map[string][]string{}
	for _, user := range users {
		key := flowKey(user.Flow)
		if key == "completed" {
			continue
		}
		grouped[key] = append(grouped[key], user.Email)
	}
	for key, emails := range grouped {
		tpl, ok := cfg.Templates[key]
		if !ok || strings.TrimSpace(tpl.Subject) == "" || strings.TrimSpace(tpl.HTML) == "" {
			result.Skipped = append(result.Skipped, key)
			continue
		}
		sourceKey := incompleteMilestoneSourceKey(day, days, key)
		exists, _ := s.store.SourceKeyExists(sourceKey)
		if exists {
			result.Skipped = append(result.Skipped, sourceKey)
			continue
		}
		html := appendReminderScheduleNotice(tpl.HTML, registeredAt, days)
		html = appendEmailFooter(html, cfg.Wechat, cfg.Website)
		targetSummary := fmt.Sprintf("自动流程提醒：注册后第%d天：%s：%s", days, key, day)
		batch, recipients, err := s.store.CreateBatch(tpl.Subject, targetSummary, sourceKey, "system", emails)
		if err == nil {
			result.Batches = append(result.Batches, batch)
			go s.sendBatch(batch, recipients, html, baseURL)
		} else {
			result.Skipped = append(result.Skipped, err.Error())
		}
	}
	return result
}

// incompleteMilestoneSourceKey 返回分阶段流程提醒的幂等键。
// registeredDay 为注册日期，days 为提醒天数，flow 为当前流程节点。
func incompleteMilestoneSourceKey(registeredDay string, days int, flow string) string {
	if days == 1 {
		return "recovery:" + registeredDay + ":" + flow
	}
	return fmt.Sprintf("recovery:%s:day-%d:%s", registeredDay, days, flow)
}

// appendReminderScheduleNotice 在邮件正文中提前说明后续提醒日期和停止条件。
// html 为流程教程正文，registeredAt 为注册日期，currentDays 为本次提醒天数。
func appendReminderScheduleNotice(html string, registeredAt time.Time, currentDays int) string {
	futureDates := make([]string, 0, 3)
	for _, days := range []int{1, 3, 7, 30} {
		if days > currentDays {
			futureDates = append(futureDates, registeredAt.AddDate(0, 0, days).Format("2006年1月2日"))
		}
	}
	message := "这是本轮流程提醒的最后一次；之后不会再按这组时间发送流程提醒。"
	if len(futureDates) > 0 {
		message = "为了让你提前了解提醒安排，也避免突然打扰：如果届时招聘流程仍未完成，我们还会在 " + strings.Join(futureDates, "、") + " 各发送一次进度提醒；流程完成后不会再发送这组提醒。"
	}
	notice := `<div style="margin:20px 0 0;padding:14px 16px;border-radius:10px;background:#f4f8f5;color:#536158;font-size:13px;line-height:1.75;"><strong style="color:#27352d;">后续提醒安排</strong><br>` + template.HTMLEscapeString(message) + `</div>`
	if strings.Contains(html, "{{footer}}") {
		return strings.Replace(html, "{{footer}}", notice+"{{footer}}", 1)
	}
	return strings.TrimSpace(html) + notice
}

// sendInactiveDays 给精确 N 天未登录的用户发送提醒。
// days 为未登录天数，baseURL 用于已读追踪图片。
func (s *AdminEmailService) sendInactiveDays(days int, baseURL string) automaticEmailResult {
	key := fmt.Sprintf("inactive_%d_days", days)
	cfg := s.recoveryConfig()
	result := automaticEmailResult{Job: fmt.Sprintf("inactive-%d-days", days)}
	tpl, ok := cfg.Templates[key]
	if !ok || strings.TrimSpace(tpl.Subject) == "" || strings.TrimSpace(tpl.HTML) == "" {
		result.Skipped = append(result.Skipped, key)
		return result
	}
	day := time.Now().Format(time.DateOnly)
	sourceKey := "recovery:" + day + ":" + key
	exists, _ := s.store.SourceKeyExists(sourceKey)
	if exists {
		result.Skipped = append(result.Skipped, sourceKey)
		return result
	}
	users, err := s.store.FindTargetUsers(EmailTargetFilter{LastLoginExactDays: days})
	if err != nil {
		result.Skipped = append(result.Skipped, err.Error())
		return result
	}
	emails := make([]string, 0, len(users))
	for _, user := range users {
		emails = append(emails, user.Email)
	}
	if len(emails) == 0 {
		result.Skipped = append(result.Skipped, "no recipients")
		return result
	}
	batch, recipients, err := s.store.CreateBatch(tpl.Subject, fmt.Sprintf("自动挽回：%d天未登录", days), sourceKey, "system", emails)
	if err != nil {
		result.Skipped = append(result.Skipped, err.Error())
		return result
	}
	result.Batches = append(result.Batches, batch)
	go s.sendBatch(batch, recipients, appendEmailFooter(tpl.HTML, cfg.Wechat, cfg.Website), baseURL)
	return result
}

func (s *AdminEmailService) sendBatch(batch EmailBatch, recipients []EmailRecipient, html string, baseURL string) {
	baseURL = s.automaticEmailBaseURL(baseURL)
	for _, item := range recipients {
		tracked := html + trackingPixel(baseURL, item.ID)
		err := s.mailer.SendCustomHTML(item.Email, batch.Subject, tracked, "")
		if err != nil {
			_ = s.store.MarkRecipientFailed(item.ID, err.Error())
			continue
		}
		_ = s.store.MarkRecipientSent(item.ID)
	}
}

// automaticEmailBaseURL 返回邮件查看追踪图片使用的完整公网地址。
// baseURL 优先使用当前请求地址，其次使用环境变量，最后使用邮件配置的网站地址。
func (s *AdminEmailService) automaticEmailBaseURL(baseURL string) string {
	if value := strings.TrimRight(strings.TrimSpace(baseURL), "/"); value != "" {
		return value
	}
	if value := strings.TrimRight(strings.TrimSpace(os.Getenv("GOODHR_PUBLIC_BASE_URL")), "/"); value != "" {
		return value
	}
	return strings.TrimRight(strings.TrimSpace(s.recoveryConfig().Website), "/")
}

func (s *AdminEmailService) resolveRecipients(req sendAdminEmailRequest) ([]string, string, error) {
	seen := map[string]bool{}
	add := func(email string) {
		if normalized, ok := normalizeEmail(email); ok {
			seen[normalized] = true
		}
	}
	if req.Mode == "all" {
		users, err := s.store.FindTargetUsers(EmailTargetFilter{})
		if err != nil {
			return nil, "", err
		}
		for _, user := range users {
			add(user.Email)
		}
		return sortedEmails(seen), "全部用户", nil
	}
	for _, email := range req.Emails {
		for _, part := range strings.FieldsFunc(email, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == ';' || r == '；' || r == ' ' }) {
			add(part)
		}
	}
	if len(req.Tags) > 0 || len(req.Flows) > 0 || req.LastLoginBeforeDays > 0 {
		users, err := s.store.FindTargetUsers(EmailTargetFilter{Tags: req.Tags, FlowSteps: req.Flows, LastLoginBeforeDays: req.LastLoginBeforeDays})
		if err != nil {
			return nil, "", err
		}
		for _, user := range users {
			add(user.Email)
		}
	}
	parts := []string{}
	if len(req.Emails) > 0 {
		parts = append(parts, "指定邮箱")
	}
	if len(req.Tags) > 0 {
		parts = append(parts, "画像："+strings.Join(req.Tags, ","))
	}
	if len(req.Flows) > 0 {
		parts = append(parts, "流程："+strings.Join(req.Flows, ","))
	}
	if req.LastLoginBeforeDays > 0 {
		parts = append(parts, fmt.Sprintf("至少%d天未登录", req.LastLoginBeforeDays))
	}
	return sortedEmails(seen), strings.Join(parts, "；"), nil
}

func (s *AdminEmailService) requireSuperAdmin(w http.ResponseWriter, r *http.Request) bool {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return false
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return false
	}
	return true
}

func sortedEmails(seen map[string]bool) []string {
	emails := make([]string, 0, len(seen))
	for email := range seen {
		emails = append(emails, email)
	}
	slices.Sort(emails)
	return emails
}

func safeImageExt(filename string, contentType string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		exts, _ := mime.ExtensionsByType(contentType)
		if len(exts) > 0 {
			ext = exts[0]
		}
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return ext, true
	default:
		return "", false
	}
}

func publicBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https"
		if strings.HasPrefix(r.Host, "localhost") || strings.HasPrefix(r.Host, "127.0.0.1") {
			proto = "http"
		}
	}
	return proto + "://" + r.Host
}

func trackingPixel(baseURL string, recipientID string) string {
	if strings.TrimSpace(baseURL) == "" {
		return ""
	}
	return `<img src="` + template.HTMLEscapeString(strings.TrimRight(baseURL, "/")+"/api/public/mail/open?id="+recipientID) + `" width="1" height="1" style="display:none" alt="">`
}

func (s *AdminEmailService) validJobToken(r *http.Request) bool {
	token := strings.TrimSpace(os.Getenv("GOODHR_EMAIL_JOB_TOKEN"))
	if token == "" {
		return false
	}
	got := strings.TrimSpace(r.URL.Query().Get("token"))
	if got == "" {
		got = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
	}
	return got == token
}

type recoveryEmailConfig struct {
	Enabled   bool                             `json:"enabled"`
	Hour      int                              `json:"hour"`
	Wechat    string                           `json:"wechat"`
	Website   string                           `json:"website"`
	Templates map[string]recoveryEmailTemplate `json:"templates"`
}

type recoveryEmailTemplate struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

// defaultRecoveryEmailTemplates 返回自动邮件的默认模板。
// 模板不包含已读追踪图，追踪图由 sendBatch 统一追加。
func defaultRecoveryEmailTemplates() map[string]recoveryEmailTemplate {
	subjects := map[string]string{
		"agent_detected":          "需要帮助吗？GoodHR 本地程序还未启动",
		"runtime_ready":           "需要帮助吗？运行组件还未安装完成",
		"position_created":        "需要帮助吗？招聘岗位还未创建",
		"task_created":            "需要帮助吗？招聘任务还未创建",
		"platform_login_verified": "需要帮助吗？招聘平台还未登录",
		"task_started":            "需要帮助吗？招聘任务还未成功启动",
		"first_resume_processed":  "需要帮助吗？还未成功处理第一份简历",
		"first_greet_success":     "需要帮助吗？还未成功向候选人打招呼",
		"inactive_3_days":         "3 天没见你了，我先小声冒个泡",
		"inactive_7_days":         "一周没见，GoodHR 还在原地等你",
		"inactive_30_days":        "一个月没见，我来弱弱问候一下",
	}
	return map[string]recoveryEmailTemplate{
		"agent_detected":          {Subject: subjects["agent_detected"], HTML: automaticEmailTemplateHTML("agent_detected")},
		"runtime_ready":           {Subject: subjects["runtime_ready"], HTML: automaticEmailTemplateHTML("runtime_ready")},
		"position_created":        {Subject: subjects["position_created"], HTML: automaticEmailTemplateHTML("position_created")},
		"task_created":            {Subject: subjects["task_created"], HTML: automaticEmailTemplateHTML("task_created")},
		"platform_login_verified": {Subject: subjects["platform_login_verified"], HTML: automaticEmailTemplateHTML("platform_login_verified")},
		"task_started":            {Subject: subjects["task_started"], HTML: automaticEmailTemplateHTML("task_started")},
		"first_resume_processed":  {Subject: subjects["first_resume_processed"], HTML: automaticEmailTemplateHTML("first_resume_processed")},
		"first_greet_success":     {Subject: subjects["first_greet_success"], HTML: automaticEmailTemplateHTML("first_greet_success")},
		"inactive_3_days":         {Subject: subjects["inactive_3_days"], HTML: automaticEmailTemplateHTML("inactive_3_days")},
		"inactive_7_days":         {Subject: subjects["inactive_7_days"], HTML: automaticEmailTemplateHTML("inactive_7_days")},
		"inactive_30_days":        {Subject: subjects["inactive_30_days"], HTML: automaticEmailTemplateHTML("inactive_30_days")},
	}
}

// appendEmailFooter 给自动邮件追加统一反馈文案。
// html 为邮件正文，wechat 为作者微信号。
func appendEmailFooter(html string, wechat string, website string) string {
	footer := automaticEmailTemplateHTML("footer")
	if strings.TrimSpace(footer) == "" {
		footer = `<p style="margin-top:18px;color:#66756b;font-size:13px;">联系电话：{{wechat}} · 微信号：{{wechat}} · 官网：<a href="{{website}}">{{website}}</a></p>`
	}
	footer = strings.ReplaceAll(footer, "{{wechat}}", template.HTMLEscapeString(strings.TrimSpace(wechat)))
	website = strings.TrimRight(strings.TrimSpace(website), "/")
	footer = strings.ReplaceAll(footer, "{{website}}", template.HTMLEscapeString(website))
	if strings.Contains(html, "{{footer}}") {
		return strings.ReplaceAll(html, "{{footer}}", footer)
	}
	return strings.TrimSpace(html) + footer
}

// automaticEmailTemplateHTML 读取自动邮件 HTML 模板文件。
// name 为模板名，不包含 .html 后缀。
func automaticEmailTemplateHTML(name string) string {
	for _, base := range []string{
		filepath.Join("templates", "automatic_emails", name+".html"),
		filepath.Join("..", "..", "templates", "automatic_emails", name+".html"),
		filepath.Join("cloud", "backend", "templates", "automatic_emails", name+".html"),
		filepath.Join("goodhr5", "cloud", "backend", "templates", "automatic_emails", name+".html"),
	} {
		raw, err := os.ReadFile(base)
		if err == nil {
			return strings.TrimSpace(string(raw))
		}
	}
	return ""
}

func (s *AdminEmailService) recoveryConfig() recoveryEmailConfig {
	cfg := recoveryEmailConfig{Enabled: false, Hour: 9, Wechat: "17607080935", Website: "https://goodhr5.58it.cn", Templates: defaultRecoveryEmailTemplates()}
	item, err := s.systemConfigs.Get("system.email_recovery")
	if err != nil {
		return cfg
	}
	custom := recoveryEmailConfig{}
	if err := json.Unmarshal([]byte(item.ConfigValue), &custom); err == nil {
		cfg.Enabled = custom.Enabled
		if custom.Hour != 0 {
			cfg.Hour = custom.Hour
		}
		if strings.TrimSpace(custom.Wechat) != "" {
			cfg.Wechat = custom.Wechat
		}
		if strings.TrimSpace(custom.Website) != "" {
			cfg.Website = custom.Website
		}
		for key, tpl := range custom.Templates {
			cfg.Templates[key] = tpl
		}
	}
	return cfg
}

func (s *AdminEmailService) recoveryHour() int {
	hour := s.recoveryConfig().Hour
	if hour < 0 || hour > 23 {
		return 9
	}
	return hour
}

func nextRecoveryRun(now time.Time, hour int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
