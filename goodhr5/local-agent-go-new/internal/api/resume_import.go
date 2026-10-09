// Package api 文件作用：接收本地简历批量导入请求，逐次验证云端账号，仅负责参数、权限与业务分派。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	resumeflow "goodhr5/local-agent-go-new/internal/flow/resume_import"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
)

// initResumeImports 组装本地解析能力；测试可传入管理器，旧版配置为空时不会影响其它接口。
func (s *Server) initResumeImports(manager *resumeflow.Manager) {
	s.resumeImports = manager
	if manager != nil || s.store == nil || s.cloud == nil || !filepath.IsAbs(s.cfg.DataDir) {
		return
	}
	root := filepath.Join(s.cfg.DataDir, "resume-import")
	extractor := &resumeflow.Extractor{Root: root}
	if s.ocr != nil {
		extractor.OCR = s.ocr
	}
	value, err := resumeflow.New(s.store, s.cloud, ai.New(), extractor, root)
	if err != nil {
		log.Printf("本地简历导入初始化未完成：%v", err)
		return
	}
	s.resumeImports = value
}

// registerResumeImportRoutes 注册批次创建、恢复、取消、重试与人工确认接口。
func (s *Server) registerResumeImportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/resume-import/tasks", s.handleResumeImportCreate)
	mux.HandleFunc("GET /api/v1/resume-import/tasks", s.handleResumeImportList)
	mux.HandleFunc("GET /api/v1/resume-import/tasks/{task_id}", s.handleResumeImportGet)
	mux.HandleFunc("POST /api/v1/resume-import/tasks/{task_id}/cancel", s.handleResumeImportCancel)
	mux.HandleFunc("POST /api/v1/resume-import/tasks/{task_id}/retry", s.handleResumeImportRetry)
	mux.HandleFunc("POST /api/v1/resume-import/tasks/{task_id}/confirm", s.handleResumeImportConfirm)
	mux.HandleFunc("POST /api/v1/resume-import/tasks/{task_id}/mark-saved", s.handleResumeImportMarkSaved)
}

// resumeImportOwner 使用请求中的网页会话验证账号，不从绑定缓存复制Token也不接受扩展局部权限。
func (s *Server) resumeImportOwner(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if s.resumeImports == nil || s.cloud == nil {
		writeError(w, 503, "RESUME_IMPORT_UNAVAILABLE", fmt.Errorf("本地简历解析还没准备好，请更新并重启本地程序"))
		return "", "", false
	}
	header := strings.Fields(r.Header.Get("Authorization"))
	if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") {
		writeError(w, 401, "LOGIN_REQUIRED", fmt.Errorf("先登录GoodHR，再继续整理简历"))
		return "", "", false
	}
	session, err := s.cloud.ValidateSession(r.Context(), header[1])
	if err != nil {
		status := http.StatusBadGateway
		var apiErr *cloud.APIError
		if errors.As(err, &apiErr) {
			status = apiErr.StatusCode
		}
		writeError(w, status, "LOGIN_CHECK_FAILED", err)
		return "", "", false
	}
	if session.SessionKind == "browser_extension" {
		writeError(w, 403, "WEB_SESSION_REQUIRED", fmt.Errorf("AI导入和草稿只在GoodHR网页里使用，请从简历库继续"))
		return "", "", false
	}
	return session.UserID, header[1], true
}

// handleResumeImportCreate 接收有大小上限的文件流，创建后立即返回排队任务。
func (s *Server) handleResumeImportCreate(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, resumeflow.MaxTotalBytes+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		writeError(w, 400, "IMPORT_UPLOAD_INVALID", fmt.Errorf("文件没接收完整，请检查每份10MB、每批60MB后再试"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["files"]
	if len(files) > resumeflow.MaxFiles {
		writeError(w, 400, "IMPORT_TOO_MANY_FILES", fmt.Errorf("每批最多20份，请分开导入"))
		return
	}
	uploads := []resumeflow.Upload{}
	closers := []io.Closer{}
	defer func() {
		for _, file := range closers {
			_ = file.Close()
		}
	}()
	for _, header := range files {
		if header.Size <= 0 || header.Size > resumeflow.MaxFileBytes {
			writeError(w, 400, "IMPORT_FILE_TOO_LARGE", fmt.Errorf("%s为空或超过10MB，请重新选择", header.Filename))
			return
		}
		file, err := header.Open()
		if err != nil {
			writeError(w, 400, "IMPORT_READ_FAILED", fmt.Errorf("文件没读出来，请重新选择"))
			return
		}
		closers = append(closers, file)
		uploads = append(uploads, resumeflow.Upload{Name: header.Filename, Reader: file})
	}
	task, err := s.resumeImports.Create(r.Context(), owner, token, uploads, r.FormValue("text"))
	s.writeResumeImportTask(w, task, err)
}

// handleResumeImportList 返回当前账号的本地草稿历史，队列恢复只使用已验证的当前会话。
func (s *Server) handleResumeImportList(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	writeSuccess(w, 200, struct {
		Tasks []resumeflow.Task `json:"tasks"`
	}{s.resumeImports.List(owner, token)})
}

// handleResumeImportGet 读取一个账号隔离的批次。
func (s *Server) handleResumeImportGet(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	task, err := s.resumeImports.Get(owner, token, r.PathValue("task_id"))
	s.writeResumeImportTask(w, task, err)
}

// handleResumeImportCancel 取消后台解析并保留已经完成的草稿。
func (s *Server) handleResumeImportCancel(w http.ResponseWriter, r *http.Request) {
	owner, _, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	task, err := s.resumeImports.Cancel(owner, r.PathValue("task_id"))
	s.writeResumeImportTask(w, task, err)
}

// handleResumeImportRetry 只分派用户明确选中的失败或排队项。
func (s *Server) handleResumeImportRetry(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	var request struct {
		ItemIDs []string `json:"item_ids"`
	}
	if !decodeResumeImportBody(w, r, &request) {
		return
	}
	task, err := s.resumeImports.Retry(owner, token, r.PathValue("task_id"), request.ItemIDs)
	s.writeResumeImportTask(w, task, err)
}

// handleResumeImportConfirm 保存人工确认内容；部分成功条目只需传入id补齐剩余记录。
func (s *Server) handleResumeImportConfirm(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	var request struct {
		Items []resumeflow.ConfirmItem `json:"items"`
	}
	if !decodeResumeImportBody(w, r, &request) {
		return
	}
	task, err := s.resumeImports.Confirm(r.Context(), owner, token, r.PathValue("task_id"), request.Items)
	s.writeResumeImportTask(w, task, err)
}

// handleResumeImportMarkSaved 验证云端候选人访问权限后标记独立保存成功。
func (s *Server) handleResumeImportMarkSaved(w http.ResponseWriter, r *http.Request) {
	owner, token, ok := s.resumeImportOwner(w, r)
	if !ok {
		return
	}
	var request struct {
		Items []resumeflow.SavedItem `json:"items"`
	}
	if !decodeResumeImportBody(w, r, &request) {
		return
	}
	task, err := s.resumeImports.MarkSaved(r.Context(), owner, token, r.PathValue("task_id"), request.Items)
	s.writeResumeImportTask(w, task, err)
}

// decodeResumeImportBody 限制JSON正文大小且拒绝多余的第二个对象。
func decodeResumeImportBody(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(target) != nil {
		writeError(w, 400, "IMPORT_BODY_INVALID", fmt.Errorf("请求内容没读出来，请重新确认"))
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, 400, "IMPORT_BODY_INVALID", fmt.Errorf("请求内容不完整，请重新确认"))
		return false
	}
	return true
}

// writeResumeImportTask 输出统一任务合同，权限和忙碌错误保持稳定HTTP状态。
func (s *Server) writeResumeImportTask(w http.ResponseWriter, task resumeflow.Task, err error) {
	if err != nil {
		status := 400
		if errors.Is(err, resumeflow.ErrNotFound) {
			status = 404
		}
		if errors.Is(err, resumeflow.ErrBusy) {
			status = 409
		}
		writeError(w, status, "RESUME_IMPORT_FAILED", err)
		return
	}
	writeSuccess(w, 200, struct {
		Task resumeflow.Task `json:"task"`
	}{task})
}
