// 本文件提供手动浏览器插件的两阶段附件文字解析任务；只解析用户提交的纯文本，不控制招聘页面，也不自动入库。
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const attachmentParsePath = "/api/candidates/attachment-parses"

var attachmentHTMLPattern = regexp.MustCompile(`(?is)<\s*/?\s*(?:!doctype|html|head|body|title|div|span|script|style|iframe|canvas|section|article|header|footer|main|nav|aside|address|h[1-6]|table|thead|tbody|tfoot|tr|th|td|caption|colgroup|col|p|br|hr|img|picture|source|a|input|button|form|label|select|option|textarea|fieldset|legend|meta|link|ul|ol|li|dl|dt|dd|strong|em|b|i|u|s|small|mark|sub|sup|blockquote|pre|code|figure|figcaption|video|audio|object|embed|svg|path|template)\b[^>]*>`)

// attachmentParseSource 只接收页面来源标识，不接收附件下载地址、Cookie 或用户指定的 AI 服务参数。
type attachmentParseSource struct {
	PlatformID          string `json:"platform_id"`
	SourceURL           string `json:"source_url,omitempty"`
	PlatformCandidateID string `json:"platform_candidate_id,omitempty"`
}

// attachmentParseRequest 将阶段、当前页面快照和付费幂等键绑定为一次操作。
type attachmentParseRequest struct {
	Phase      string                `json:"phase"`
	RequestKey string                `json:"request_key"`
	CaptureID  string                `json:"capture_id"`
	Text       string                `json:"text"`
	TextHash   string                `json:"text_hash,omitempty"`
	Source     attachmentParseSource `json:"source,omitempty"`
}

// attachmentParseTask 是插件可见合同；凭证、账号和原始简历不会出现在任务回执中。
type attachmentParseTask struct {
	ID              string         `json:"id"`
	Phase           string         `json:"phase"`
	CaptureID       string         `json:"capture_id"`
	TextHash        string         `json:"text_hash"`
	RequestKey      string         `json:"request_key"`
	Status          string         `json:"status"`
	Resume          map[string]any `json:"resume,omitempty"`
	ErrorCode       string         `json:"error_code,omitempty"`
	Message         string         `json:"message,omitempty"`
	PossiblyCharged bool           `json:"possibly_charged"`
}

// AttachmentParseService 隔离用户、候选人快照和并发模型请求，复用本人 AI 配置与现有计费 HTTP 路径。
type AttachmentParseService struct {
	auth   *AuthService
	ai     *AIConfigService
	store  attachmentParseStore
	mu     sync.Mutex
	active map[string]context.CancelFunc
	slots  chan struct{}
}

// NewAttachmentParseService 创建插件附件任务服务，Redis 回执在进程重启后仍禁止重复付费请求。
func NewAttachmentParseService(auth *AuthService, ai *AIConfigService) *AttachmentParseService {
	return &AttachmentParseService{auth: auth, ai: ai, store: newAttachmentParseStore(auth.store), active: map[string]context.CancelFunc{}, slots: make(chan struct{}, 4)}
}

// ServeHTTP 认证后仅开放创建、读取和取消操作，不接受以解析接口直接保存候选人。
func (s *AttachmentParseService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, err := s.auth.SessionFromRequest(r)
	if err != nil || strings.TrimSpace(session.Email) == "" {
		attachmentParseError(w, 401, "SESSION_EXPIRED", "登录已经过期，请重新登录插件")
		return
	}
	if session.Kind != extensionSessionKind {
		attachmentParseError(w, 403, "EXTENSION_SESSION_REQUIRED", "请从 GoodHR 浏览器插件发起附件解析")
		return
	}
	if s.store == nil {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "附件任务回执还没接好，我先不调用 AI，请稍后再试")
		return
	}
	owner := strings.ToLower(strings.TrimSpace(session.Email))
	suffix := strings.TrimPrefix(r.URL.Path, attachmentParsePath)
	if suffix == "" && r.Method == http.MethodPost {
		s.create(w, r, owner)
		return
	}
	parts := strings.Split(strings.Trim(suffix, "/"), "/")
	if len(parts) > 2 || len(parts[0]) != 64 {
		attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次附件任务没找到，请回原页面重新操作")
		return
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次附件任务没找到")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.read(w, r, owner, parts[0], false)
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost {
		s.read(w, r, owner, parts[0], true)
		return
	}
	attachmentParseError(w, 405, "METHOD_NOT_ALLOWED", "这个附件操作暂时不支持")
}

// create 先核对已有回执，再读取本人的 AI 配置；只有成功预占任务的一次请求才能启动模型。
func (s *AttachmentParseService) create(w http.ResponseWriter, r *http.Request, owner string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input attachmentParseRequest
	if err := decoder.Decode(&input); err != nil {
		attachmentParseError(w, 400, "INVALID_REQUEST", "附件文字没有读完整，请重新打开附件再试")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		attachmentParseError(w, 400, "INVALID_REQUEST", "一次请只提交一份附件")
		return
	}
	if message := validateAttachmentParseRequest(&input); message != "" {
		attachmentParseError(w, 400, "INVALID_REQUEST", message)
		return
	}
	id := attachmentDigest(owner + "\x00" + input.Phase + "\x00" + input.RequestKey)
	encoded, _ := json.Marshal(input)
	fingerprint := attachmentDigest(string(encoded))
	if old, err := s.store.Get(r.Context(), owner, id); err == nil {
		if old.Fingerprint != fingerprint {
			attachmentParseError(w, 409, "REQUEST_CONFLICT", "这次请求编号对应另一份附件，请重新发起")
			return
		}
		s.writeTask(w, http.StatusOK, s.expireLease(old))
		return
	} else if !errors.Is(err, ErrNotFound) {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "任务回执暂时没存好，我先不调用 AI，请稍后重试")
		return
	}
	cfg, err := s.ai.store.UserConfig(owner)
	if errors.Is(err, ErrNotFound) || (err == nil && (strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" || strings.TrimSpace(cfg.APIKey) == "")) {
		attachmentParseError(w, 422, "AI_NOT_CONFIGURED", "请先在 GoodHR 个人配置中设置 AI 接口、模型和 Key，再回来识别附件")
		return
	}
	if err != nil {
		attachmentParseError(w, 503, "AI_CONFIG_UNAVAILABLE", "你的 AI 配置暂时没读到，请稍后再试")
		return
	}
	if err := validateAIConfigTestRequest(aiConfigRequest{BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.APIKey}); err != nil {
		attachmentParseError(w, 422, "AI_CONFIG_INVALID", "AI 接口地址不可用，请在个人配置中检查公网 HTTPS 地址")
		return
	}
	record := attachmentParseRecord{Owner: owner, Fingerprint: fingerprint, Version: 1, LeaseUntil: time.Now().Add(aiRequestTimeout + time.Minute).UnixMilli(), Task: attachmentParseTask{ID: id, Phase: input.Phase, CaptureID: input.CaptureID, TextHash: input.TextHash, RequestKey: input.RequestKey, Status: "queued"}}
	actual, created, err := s.store.Reserve(r.Context(), record)
	if errors.Is(err, errAttachmentBusy) {
		attachmentParseError(w, 429, "TASK_BUSY", err.Error())
		return
	}
	if err != nil {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "任务回执暂时没存好，我先不调用 AI，请稍后重试")
		return
	}
	if actual.Fingerprint != fingerprint {
		attachmentParseError(w, 409, "REQUEST_CONFLICT", "这次请求编号对应另一份附件，请重新发起")
		return
	}
	if !created {
		s.writeTask(w, http.StatusOK, s.expireLease(actual))
		return
	}
	go s.run(record, input, cfg)
	s.writeTask(w, http.StatusAccepted, record)
}

// read 返回原账号的任务或原子取消任务；取消不会把已开始的付费调用误报为一定免费。
func (s *AttachmentParseService) read(w http.ResponseWriter, r *http.Request, owner, id string, cancelTask bool) {
	for attempt := 0; attempt < 4; attempt++ {
		record, err := s.store.Get(r.Context(), owner, id)
		if err == nil && record.Import != nil {
			attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次解析任务没找到")
			return
		}
		if errors.Is(err, ErrNotFound) {
			attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次附件任务没找到，或不属于当前账号")
			return
		}
		if err != nil {
			attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "任务状态暂时没读到，请稍后重试")
			return
		}
		if !cancelTask || !attachmentTaskActive(record.Task.Status) {
			s.writeTask(w, 200, s.expireLease(record))
			return
		}
		version := record.Version
		record.Version++
		record.Task.PossiblyCharged = record.Task.Status == "running"
		record.Task.Status, record.Task.ErrorCode, record.Task.Message = "cancelled", "CANCELLED", "已停止回填；若 AI 请求已经发出，可能仍有费用，请核对后再重试"
		ok, err := s.store.Swap(r.Context(), record, version)
		if err != nil {
			attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "取消状态暂时没存好，请重试取消")
			return
		}
		if !ok {
			continue
		}
		s.mu.Lock()
		cancel := s.active[id]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		s.writeTask(w, 200, record)
		return
	}
	attachmentParseError(w, 409, "TASK_CHANGED", "任务状态刚有变化，请再读取一次")
}

// run 在持久记录开始状态后只请求一次模型；迟到结果必须通过版本交换才能回填。
func (s *AttachmentParseService) run(record attachmentParseRecord, input attachmentParseRequest, cfg AIConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), aiRequestTimeout)
	s.mu.Lock()
	s.active[record.Task.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.active, record.Task.ID); s.mu.Unlock() }()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		s.finish(record, "failed", "AI_NOT_STARTED", "当前请求还没发出，请稍后手动重试", false, nil)
		return
	}
	current, err := s.store.Get(ctx, record.Owner, record.Task.ID)
	if err != nil || current.Task.Status != "queued" {
		return
	}
	record = current
	record.Task.Status = "running"
	record.Version++
	if ok, err := s.store.Swap(ctx, record, current.Version); err != nil || !ok {
		return
	}
	// 取消可能来自另一服务实例；轮询回执把跨实例取消传递给上游 HTTP 请求。
	watchDone := make(chan struct{})
	defer close(watchDone)
	go s.watchCancellation(ctx, record, cancel, watchDone)
	resume, code, message, uncertain := s.parseAI(ctx, cfg, input)
	if code != "" {
		status := "failed"
		if uncertain {
			status = "uncertain"
		}
		s.finish(record, status, code, message, true, nil)
		return
	}
	s.finish(record, "completed", "", "附件识别完成，请核对后再保存", false, resume)
}

// watchCancellation 只读取持久状态，不重新启动或重复发送模型请求。
func (s *AttachmentParseService) watchCancellation(ctx context.Context, record attachmentParseRecord, cancel context.CancelFunc, done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			current, err := s.store.Get(ctx, record.Owner, record.Task.ID)
			if err == nil && current.Task.Status != "running" {
				cancel()
				return
			}
		}
	}
}

// finish 只更新仍属于本次调用的状态，已取消或被接管的任务不会泄露迟到结果。
func (s *AttachmentParseService) finish(record attachmentParseRecord, status, code, message string, charged bool, resume map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, err := s.store.Get(ctx, record.Owner, record.Task.ID)
	if err != nil || current.Version != record.Version || !attachmentTaskActive(current.Task.Status) {
		return
	}
	record.Task.Status, record.Task.ErrorCode, record.Task.Message, record.Task.PossiblyCharged, record.Task.Resume = status, code, message, charged, resume
	record.Version++
	_, _ = s.store.Swap(ctx, record, current.Version)
}

// expireLease 把崩溃后仍在处理的回执标为结果不明；读取状态绝不恢复付费调用。
func (s *AttachmentParseService) expireLease(record attachmentParseRecord) attachmentParseRecord {
	if !attachmentTaskActive(record.Task.Status) || record.LeaseUntil > time.Now().UnixMilli() {
		return record
	}
	old := record.Version
	record.Version++
	if record.Task.Status == "running" {
		record.Task.Status, record.Task.ErrorCode, record.Task.Message, record.Task.PossiblyCharged = "uncertain", "AI_RESULT_UNCERTAIN", "上次解析结果没确认，可能已计费；请核对后手动重试", true
	} else {
		record.Task.Status, record.Task.ErrorCode, record.Task.Message = "failed", "AI_NOT_STARTED", "上次任务尚未开始，请重新发起"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok, _ := s.store.Swap(ctx, record, old); !ok {
		if current, err := s.store.Get(ctx, record.Owner, record.Task.ID); err == nil {
			return current
		}
	}
	return record
}

// writeTask 只写公开任务合同，不包含账号、凭证、上游地址或内部内容指纹。
func (s *AttachmentParseService) writeTask(w http.ResponseWriter, status int, record attachmentParseRecord) {
	writeJSON(w, status, map[string]any{"ok": true, "task": record.Task})
}

// attachmentParseError 使用稳定错误码提示插件，避免把上游原文、Key 或个人信息带进错误响应。
func attachmentParseError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "error_code": code, "error": message, "message": message})
}

// attachmentDigest 使用 UTF-8 原文计算摘要，不修剪原文，便于与插件的文本快照逐字校验。
func attachmentDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// validateAttachmentParseRequest 限制来源和大小；客户端不能指定租户、账号、模型或远端下载目标。
func validateAttachmentParseRequest(input *attachmentParseRequest) string {
	if input.Phase != "basic" && input.Phase != "full" {
		return "请选择基础识别或完整解析"
	}
	if len(input.RequestKey) < 8 || len(input.RequestKey) > 128 || strings.ContainsAny(input.RequestKey, "\r\n\x00") {
		return "这次解析请求编号不完整，请重新打开附件"
	}
	if strings.TrimSpace(input.CaptureID) == "" || len(input.CaptureID) > 160 {
		return "当前附件标识不完整，请重新打开附件"
	}
	if strings.TrimSpace(input.Text) == "" || !utf8.ValidString(input.Text) || utf8.RuneCountInString(input.Text) > 200000 {
		return "附件文字为空或超过20万字，请分开整理"
	}
	if attachmentHTMLPattern.MatchString(input.Text) {
		return "请先移除附件的 HTML 标签，只提交已显示的纯文字"
	}
	hash := attachmentDigest(input.Text)
	if input.TextHash != "" && !strings.EqualFold(input.TextHash, hash) {
		return "附件文字已经变化，请重新读取后再试"
	}
	input.TextHash = hash
	if input.Source.PlatformID == "" {
		input.Source.PlatformID = "boss"
	}
	if input.Source.PlatformID != "boss" || len(input.Source.PlatformCandidateID) > 200 || len(input.Source.SourceURL) > 2048 {
		return "附件来源信息不完整"
	}
	if input.Source.SourceURL != "" {
		u, err := url.Parse(input.Source.SourceURL)
		if err != nil || u.Scheme != "https" || u.Host != "www.zhipin.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "附件来源请使用不带临时参数的 BOSS 页面地址"
		}
	}
	return ""
}
