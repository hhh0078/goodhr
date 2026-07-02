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

// StartRecoveryScheduler 启动每日自动挽回邮件任务。
func (s *AdminEmailService) StartRecoveryScheduler() {
	go func() {
		for {
			next := nextRecoveryRun(time.Now(), s.recoveryHour())
			time.Sleep(time.Until(next))
			s.SendYesterdayRecovery()
		}
	}()
}

// SendYesterdayRecovery 给昨天注册且卡在流程节点的用户发送挽回邮件。
func (s *AdminEmailService) SendYesterdayRecovery() {
	cfg := s.recoveryConfig()
	if !cfg.Enabled {
		return
	}
	day := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	users, err := s.store.FindTargetUsers(EmailTargetFilter{CreatedDay: day})
	if err != nil {
		return
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
			continue
		}
		sourceKey := "recovery:" + day + ":" + key
		exists, _ := s.store.SourceKeyExists(sourceKey)
		if exists {
			continue
		}
		html := tpl.HTML + `<p style="margin-top:18px;color:#66756b;font-size:13px;">需要人工帮忙的话，可以加微信：` + template.HTMLEscapeString(cfg.Wechat) + `</p>`
		batch, recipients, err := s.store.CreateBatch(tpl.Subject, "自动挽回："+key+"："+day, sourceKey, "system", emails)
		if err == nil {
			go s.sendBatch(batch, recipients, html, "")
		}
	}
}

func (s *AdminEmailService) sendBatch(batch EmailBatch, recipients []EmailRecipient, html string, baseURL string) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = strings.TrimRight(os.Getenv("GOODHR_PUBLIC_BASE_URL"), "/")
	}
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

type recoveryEmailConfig struct {
	Enabled   bool                             `json:"enabled"`
	Hour      int                              `json:"hour"`
	Wechat    string                           `json:"wechat"`
	Templates map[string]recoveryEmailTemplate `json:"templates"`
}

type recoveryEmailTemplate struct {
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

func (s *AdminEmailService) recoveryConfig() recoveryEmailConfig {
	cfg := recoveryEmailConfig{Enabled: false, Hour: 9, Wechat: "a1224299352", Templates: map[string]recoveryEmailTemplate{}}
	item, err := s.systemConfigs.Get("system.email_recovery")
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal([]byte(item.ConfigValue), &cfg)
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
