// 本文件接收插件明确确认的 BOSS 附件入库意图，冻结人工核对值，并只向客户端返回后台任务回执。
package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const attachmentImportPath = "/api/candidates/attachment-imports"

// attachmentImportReview 只接收界面允许人工核对的字段，不接收客户端完整 AI 简历。
type attachmentImportReview struct {
	Name             string `json:"candidate_name"`
	Phone            string `json:"phone"`
	Gender           string `json:"gender"`
	Email            string `json:"email,omitempty"`
	Region           string `json:"work_region,omitempty"`
	Position         string `json:"current_position,omitempty"`
	ExpectedPosition string `json:"expected_position,omitempty"`
}

// attachmentImportNote 冻结新建候选人时用户确认的独立备注。
type attachmentImportNote struct {
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

// attachmentImportRecommendation 冻结新建候选人的岗位关联，不启动自动招聘任务。
type attachmentImportRecommendation struct {
	PositionID string `json:"position_id"`
	Stage      string `json:"stage"`
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

// attachmentImportRequest 把一次确认绑定到快照、人工修订和目标行版本；正文仅在确认后临时持久化。
type attachmentImportRequest struct {
	RequestKey     string                          `json:"request_key"`
	CaptureID      string                          `json:"capture_id"`
	Text           string                          `json:"text"`
	TextHash       string                          `json:"text_hash,omitempty"`
	Source         attachmentParseSource           `json:"source"`
	Operation      string                          `json:"operation"`
	CandidateID    string                          `json:"candidate_id,omitempty"`
	RowVersion     int64                           `json:"row_version,omitempty"`
	FullTaskID     string                          `json:"full_task_id,omitempty"`
	Review         attachmentImportReview          `json:"review"`
	ReviewFields   map[string]string               `json:"review_fields"`
	EditedFields   []string                        `json:"edited_fields"`
	FolderIDs      []string                        `json:"folder_ids"`
	AvatarDataURL  string                          `json:"avatar_data_url,omitempty"`
	Note           *attachmentImportNote           `json:"note,omitempty"`
	Recommendation *attachmentImportRecommendation `json:"recommendation,omitempty"`
}

// attachmentImportState 是只保存在服务端的确认意图和断点，不会通过 GET 返回原文或完整解析。
type attachmentImportState struct {
	CreatedAt          int64                   `json:"created_at"`
	Input              attachmentImportRequest `json:"input"`
	Scope              CandidateLibraryScope   `json:"scope"`
	Stage              string                  `json:"stage"`
	Fields             map[string]any          `json:"fields,omitempty"`
	SaveValidated      bool                    `json:"save_validated,omitempty"`
	CandidateID        string                  `json:"candidate_id,omitempty"`
	RowVersion         int64                   `json:"row_version,omitempty"`
	NoteDone           bool                    `json:"note_done,omitempty"`
	RecommendationDone bool                    `json:"recommendation_done,omitempty"`
	FollowupErrors     []string                `json:"followup_errors,omitempty"`
}

// attachmentImportTask 仅公开状态与已保存的编号，绝不公开完整解析供前端再次入库。
type attachmentImportTask struct {
	ID              string   `json:"id"`
	RequestKey      string   `json:"request_key"`
	CaptureID       string   `json:"capture_id"`
	TextHash        string   `json:"text_hash"`
	Status          string   `json:"status"`
	Stage           string   `json:"stage"`
	CandidateID     string   `json:"candidate_id,omitempty"`
	RowVersion      int64    `json:"row_version,omitempty"`
	ErrorCode       string   `json:"error_code,omitempty"`
	Message         string   `json:"message,omitempty"`
	PossiblyCharged bool     `json:"possibly_charged"`
	FollowupErrors  []string `json:"followup_errors,omitempty"`
}

// AttachmentImportService 复用解析回执、本人 AI 和简历库事务，在插件断开后继续已确认的保存。
type AttachmentImportService struct {
	parses     *AttachmentParseService
	candidates *CandidateService
	messages   *MessageService
}

// NewAttachmentImportService 创建服务端入库服务；普通基础识别仍由原解析服务处理。
func NewAttachmentImportService(parses *AttachmentParseService, candidates *CandidateService, messages *MessageService) *AttachmentImportService {
	return &AttachmentImportService{parses: parses, candidates: candidates, messages: messages}
}

// attachmentImportID 按账号和稳定请求键生成任务编号，与普通解析的幂等空间隔离。
func attachmentImportID(owner, key string) string {
	return attachmentDigest(owner + "\x00import\x00" + key)
}

// ServeHTTP 仅接受插件有限会话，提供提交、按编号读取和按原请求键恢复，不开放取消已确认入库。
func (s *AttachmentImportService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	session, err := s.parses.auth.SessionFromRequest(r)
	if err != nil || strings.TrimSpace(session.Email) == "" {
		attachmentParseError(w, 401, "SESSION_EXPIRED", "登录已经过期，请重新登录插件")
		return
	}
	if session.Kind != extensionSessionKind {
		attachmentParseError(w, 403, "EXTENSION_SESSION_REQUIRED", "请从 GoodHR 插件确认附件入库")
		return
	}
	if s.parses.store == nil {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "入库任务回执还没接好，我先不调用 AI，请稍后再试")
		return
	}
	owner := strings.ToLower(strings.TrimSpace(session.Email))
	suffix := strings.TrimPrefix(r.URL.Path, attachmentImportPath)
	if suffix == "" && r.Method == http.MethodPost {
		s.create(w, r, owner)
		return
	}
	if r.Method != http.MethodGet {
		attachmentParseError(w, 405, "METHOD_NOT_ALLOWED", "已确认的入库在后台继续，这个操作暂不支持")
		return
	}
	id := strings.TrimPrefix(suffix, "/")
	if suffix == "" {
		key := r.URL.Query().Get("request_key")
		if len(key) < 8 || len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
			attachmentParseError(w, 400, "INVALID_REQUEST", "请带上原来的保存请求编号")
			return
		}
		id = attachmentImportID(owner, key)
	}
	if len(id) != 64 {
		attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次入库任务没找到")
		return
	}
	if _, err := hex.DecodeString(id); err != nil {
		attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次入库任务没找到")
		return
	}
	record, err := s.parses.store.Get(r.Context(), owner, id)
	if errors.Is(err, ErrNotFound) || (err == nil && record.Import == nil) {
		attachmentParseError(w, 404, "TASK_NOT_FOUND", "这次入库任务没找到，或不属于当前账号")
		return
	}
	if err != nil {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "任务状态暂时没读到，请用原请求编号再查一次")
		return
	}
	record = s.reconcile(record)
	s.writeTask(w, 200, record)
}

// create 在付费调用之前持久保存确认意图；同键重试直接返回原任务，不重新解析或重复写入。
func (s *AttachmentImportService) create(w http.ResponseWriter, r *http.Request, owner string) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input attachmentImportRequest
	if err := decoder.Decode(&input); err != nil {
		attachmentParseError(w, 400, "INVALID_REQUEST", "入库资料没有读完整，请回原附件重试")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		attachmentParseError(w, 400, "INVALID_REQUEST", "一次请只确认一份附件")
		return
	}
	if message := validateAttachmentImportRequest(&input); message != "" {
		attachmentParseError(w, 400, "INVALID_REQUEST", message)
		return
	}
	id := attachmentImportID(owner, input.RequestKey)
	encoded, _ := json.Marshal(input)
	fingerprint := attachmentDigest(string(encoded))
	if old, err := s.parses.store.Get(r.Context(), owner, id); err == nil {
		if old.Import == nil || old.Fingerprint != fingerprint {
			attachmentParseError(w, 409, "REQUEST_CONFLICT", "这个保存编号已确认过另一份资料，请先查看原任务")
			return
		}
		s.writeTask(w, 200, s.reconcile(old))
		return
	} else if !errors.Is(err, ErrNotFound) {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "保存回执暂时没读到，请保留原请求编号重试")
		return
	}
	scope, ok := s.candidates.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.UserEmail = owner; scope.TenantID == "" {
		attachmentParseError(w, 503, "SCOPE_UNAVAILABLE", "团队信息还没准备好")
		return
	}
	if code, message := s.preflight(scope, input); code != "" {
		attachmentParseError(w, 409, code, message)
		return
	}
	var fields map[string]any
	if input.FullTaskID != "" {
		old, err := s.parses.store.Get(r.Context(), owner, input.FullTaskID)
		if err != nil || old.Import != nil || old.Task.Phase != "full" || old.Task.Status != "completed" || old.Task.CaptureID != input.CaptureID || old.Task.TextHash != input.TextHash || candidateString(old.Task.Resume, "platform_id") != input.Source.PlatformID || candidateString(old.Task.Resume, "source_url") != input.Source.SourceURL || candidateString(old.Task.Resume, "platform_candidate_id") != input.Source.PlatformCandidateID {
			attachmentParseError(w, 409, "FULL_TASK_MISMATCH", "之前的完整解析不属于这份附件，请核对原来的任务")
			return
		}
		fields, err = attachmentImportFields(old.Task.Resume, input)
		if err != nil {
			attachmentParseError(w, 400, "INVALID_REVIEW", "核对资料还没准备好，请检查姓名、手机号和履历日期")
			return
		}
	} else if _, code, message := s.userAI(owner); code != "" {
		attachmentParseError(w, 422, code, message)
		return
	}
	record := attachmentParseRecord{Owner: owner, Fingerprint: fingerprint, Version: 1, LeaseUntil: time.Now().Add(aiRequestTimeout + time.Minute).UnixMilli(), Task: attachmentParseTask{ID: id, Phase: "import", CaptureID: input.CaptureID, TextHash: input.TextHash, RequestKey: input.RequestKey, Status: "queued"}, Import: &attachmentImportState{CreatedAt: time.Now().UnixMilli(), Input: input, Scope: scope, Stage: "queued", Fields: fields}}
	actual, created, err := s.parses.store.Reserve(r.Context(), record)
	if errors.Is(err, errAttachmentBusy) {
		attachmentParseError(w, 429, "TASK_BUSY", err.Error())
		return
	}
	if err != nil {
		attachmentParseError(w, 503, "TASK_STORE_UNAVAILABLE", "确认意图还没存好，我先不调用 AI，请用原请求编号重试")
		return
	}
	if actual.Fingerprint != fingerprint {
		attachmentParseError(w, 409, "REQUEST_CONFLICT", "这个保存编号已用于另一份资料")
		return
	}
	if created {
		go s.run(record)
		s.writeTask(w, 202, record)
		return
	}
	s.writeTask(w, 200, s.reconcile(actual))
}

// userAI 读取本人当前 AI 配置，不使用系统默认账号，也不把凭证写入任务记录。
func (s *AttachmentImportService) userAI(owner string) (AIConfig, string, string) {
	cfg, err := s.parses.ai.store.UserConfig(owner)
	if errors.Is(err, ErrNotFound) || (err == nil && (strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" || strings.TrimSpace(cfg.APIKey) == "")) {
		return cfg, "AI_NOT_CONFIGURED", "请先在 GoodHR 个人配置中设置 AI 接口、模型和 Key，再回来添加附件"
	}
	if err != nil {
		return cfg, "AI_CONFIG_UNAVAILABLE", "你的 AI 配置暂时没读到，请稍后再试"
	}
	if validateAIConfigTestRequest(aiConfigRequest{BaseURL: cfg.BaseURL, Model: cfg.Model, APIKey: cfg.APIKey}) != nil {
		return cfg, "AI_CONFIG_INVALID", "AI 接口地址不可用，请检查个人配置的公网 HTTPS 地址"
	}
	return cfg, "", ""
}

// preflight 在 AI 之前核对目标行、重复手机号、收藏夹和岗位权限，最终保存仍由原事务再次校验。
func (s *AttachmentImportService) preflight(scope CandidateLibraryScope, input attachmentImportRequest) (string, string) {
	store, ok := s.candidates.store.(CandidateLibraryStore)
	if !ok {
		return "SAVE_UNAVAILABLE", "简历库还没准备好，请稍后再试"
	}
	if input.Operation == "update" {
		item, err := s.candidates.store.GetPositionCandidate(scope.TenantID, input.CandidateID, "", scope.UserEmail, scope.IsAdmin)
		if err != nil {
			return "CANDIDATE_NOT_FOUND", "这份目标简历暂时无法访问，请重新查重"
		}
		if item.RowVersion != input.RowVersion {
			return "CANDIDATE_VERSION_CONFLICT", "这份简历刚刚更新过，请重新查重再确认"
		}
	}
	matches, hidden, err := store.FindCandidateDuplicates(scope, CandidateDuplicateQuery{Phone: input.Review.Phone, PlatformID: input.Source.PlatformID, PlatformCandidateID: input.Source.PlatformCandidateID, ExcludeID: input.CandidateID})
	if err != nil {
		return "SAVE_UNAVAILABLE", "查重结果暂时没读到，请稍后重试"
	}
	if hidden {
		return "CANDIDATE_DUPLICATE", "有重复资料暂时无权查看，请先处理查重结果"
	}
	for _, match := range matches {
		if match.MatchType == "exact" {
			return "CANDIDATE_DUPLICATE", "候选人已经入库，请先查重并选择对应简历"
		}
	}
	if len(input.FolderIDs) > 0 {
		folders, ok := s.candidates.store.(CandidateFolderStore)
		if !ok {
			return "FOLDER_UNAVAILABLE", "收藏夹暂时没准备好"
		}
		items, err := folders.ListCandidateFolders(scope)
		if err != nil {
			return "FOLDER_UNAVAILABLE", "收藏夹暂时没读到"
		}
		available := map[string]bool{}
		for _, item := range items {
			available[item.ID] = true
		}
		for _, id := range input.FolderIDs {
			if !available[id] {
				return "FOLDER_UNAVAILABLE", "收藏夹已经变化，请重新选择"
			}
		}
	}
	if input.Recommendation != nil {
		if _, err := s.recommendationPosition(scope, input.Recommendation.PositionID); err != nil {
			return "POSITION_UNAVAILABLE", "推荐岗位已经变化，请重新选择开启的岗位"
		}
	}
	return "", ""
}

// writeTask 仅序列化公开回执；服务端原文、人工资料和完整解析始终留在内部状态。
func (s *AttachmentImportService) writeTask(w http.ResponseWriter, code int, record attachmentParseRecord) {
	state := record.Import
	task := attachmentImportTask{ID: record.Task.ID, RequestKey: record.Task.RequestKey, CaptureID: record.Task.CaptureID, TextHash: record.Task.TextHash, Status: record.Task.Status, Stage: state.Stage, CandidateID: state.CandidateID, RowVersion: state.RowVersion, ErrorCode: record.Task.ErrorCode, Message: record.Task.Message, PossiblyCharged: record.Task.PossiblyCharged, FollowupErrors: state.FollowupErrors}
	writeJSON(w, code, map[string]any{"ok": true, "task": task})
}

var attachmentReviewDate = regexp.MustCompile(`^(?:19|20)\d{2}(?:-(?:0[1-9]|1[0-2]))?$`)
var attachmentReviewMappings = map[string][3]string{"work_experiences": {"first-company", "first-work-start", "first-work-end"}, "educations": {"first-school", "first-school-start", "first-school-end"}}

// validateAttachmentImportRequest 复用纯文字和来源校验，并拒绝未冻结、越权或不完整的保存资料。
func validateAttachmentImportRequest(input *attachmentImportRequest) string {
	parse := attachmentParseRequest{Phase: "full", RequestKey: input.RequestKey, CaptureID: input.CaptureID, Text: input.Text, TextHash: input.TextHash, Source: input.Source}
	if message := validateAttachmentParseRequest(&parse); message != "" {
		return message
	}
	input.TextHash, input.Source = parse.TextHash, parse.Source
	if input.Operation != "create" && input.Operation != "update" {
		return "请选择添加或更新简历"
	}
	if input.Operation == "create" && (input.CandidateID != "" || input.RowVersion != 0) {
		return "添加简历时不能指定已有候选人"
	}
	if input.Operation == "update" && (input.CandidateID == "" || len(input.CandidateID) > 100 || strings.Contains(input.CandidateID, "/") || input.RowVersion < 1) {
		return "请先查重并确认目标简历版本"
	}
	if input.Operation == "update" && (input.Note != nil || input.Recommendation != nil) {
		return "更新简历时请用独立按钮记录备注和推荐"
	}
	if input.FullTaskID != "" {
		if len(input.FullTaskID) != 64 {
			return "之前的完整解析编号没认出来"
		}
		if _, err := hex.DecodeString(input.FullTaskID); err != nil {
			return "之前的完整解析编号没认出来"
		}
	}
	for _, value := range []string{input.Review.Name, input.Review.Email, input.Review.Region, input.Review.Position, input.Review.ExpectedPosition} {
		if len([]rune(value)) > 500 {
			return "人工核对字段请控制在500字内"
		}
	}
	input.Review.Name = strings.TrimSpace(input.Review.Name)
	input.Review.Phone = normalizeCandidatePhone(attachmentFactText(input.Review.Phone))
	if input.Review.Name == "" || input.Review.Phone == "" {
		return "姓名和完整手机号还需要核对一下，我再帮你入库"
	}
	switch input.Review.Gender {
	case "male", "男":
		input.Review.Gender = "男"
	case "female", "女":
		input.Review.Gender = "女"
	case "":
	default:
		return "性别请留空或选择男、女"
	}
	allowed := map[string]bool{"name": true, "phone": true, "gender": true, "candidate-email": true, "city": true, "position": true, "expected-position": true}
	for _, keys := range attachmentReviewMappings {
		for index, key := range keys {
			allowed[key] = true
			value, present := input.ReviewFields[key]
			if !present || len([]rune(value)) > 500 {
				return "首份工作和学校的核对字段不完整，请重新确认"
			}
			value = strings.TrimSpace(value)
			if index > 0 && value != "" && !attachmentReviewDate.MatchString(value) && !(index == 2 && (value == "至今" || strings.EqualFold(value, "present") || strings.EqualFold(value, "current"))) {
				return "履历日期请填写年份或年月，不确定可以留空"
			}
			input.ReviewFields[key] = value
		}
	}
	if len(input.ReviewFields) != 6 || len(input.EditedFields) > len(allowed) {
		return "核对字段没认出来，请重新打开附件"
	}
	for _, key := range input.EditedFields {
		if !allowed[key] {
			return "这项人工修改暂时不支持"
		}
	}
	if input.FolderIDs == nil {
		input.FolderIDs = []string{}
	}
	ids, err := candidateFolderIDs(input.FolderIDs)
	if err != nil {
		return "收藏夹编号没认出来，请重新选择"
	}
	input.FolderIDs = ids
	avatar, err := normalizeCandidateAvatar(input.AvatarDataURL)
	if err != nil {
		return "头像格式没认出来，请重新打开附件"
	}
	input.AvatarDataURL = avatar
	if input.Note != nil {
		input.Note.Content = strings.TrimSpace(input.Note.Content)
		if input.Note.Content == "" || len([]rune(input.Note.Content)) > 1000 {
			return "备注请填写1到1000字"
		}
		if _, err := parseCandidateOccurredAt(input.Note.OccurredAt); err != nil {
			return "备注发生时间没认出来"
		}
	}
	if input.Recommendation != nil {
		item := input.Recommendation
		item.Stage = firstNonEmpty(strings.TrimSpace(item.Stage), "draft")
		if item.PositionID == "" || len(item.PositionID) > 100 || !validRecommendationStage(item.Stage) || len([]rune(item.Content)) > 2000 {
			return "推荐岗位、阶段或备注没认出来"
		}
		if _, err := parseCandidateOccurredAt(item.OccurredAt); err != nil {
			return "推荐发生时间没认出来"
		}
	}
	return ""
}

// attachmentImportPending 区分仍可安全恢复的入库和已结束回执；不恢复结果不明的模型请求。
func attachmentImportPending(record attachmentParseRecord) bool {
	return record.Import != nil && (attachmentTaskActive(record.Task.Status) || (record.Task.Status == "uncertain" && (record.Import.Stage == "saving" || record.Import.Stage == "followups")))
}

// attachmentImportContext 为持久状态提供独立于插件连接的有限超时。
func attachmentImportContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}
