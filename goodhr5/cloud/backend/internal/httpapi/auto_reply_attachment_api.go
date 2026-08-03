// Package httpapi 本文件负责自动回复简历附件的类型校验、20MB限制、持久化保存和元数据接口。
package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const autoReplyMultipartOverhead = 1 << 20

// Attachment 通过登录和团队权限校验后下载云端持久化目录中的简历附件。
func (s *AutoReplyService) Attachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "简历附件这里只支持下载")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	attachmentID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/attachments/"), "/")
	item, err := s.store.GetResumeAttachment(r.Context(), requestContext.Tenant.ID, attachmentID)
	if err != nil {
		writeAutoReplyStoreError(w, err, "简历附件没找到")
		return
	}
	path, err := s.resumeStoragePath(item.StoragePath)
	if err != nil {
		writeAutoReplyInternalError(w, "RESUME_PATH_INVALID", "简历附件保存路径不安全，我先不敢打开", err)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeAutoReplyError(w, http.StatusNotFound, "RESUME_FILE_NOT_FOUND", "简历附件记录还在，但文件暂时没找到")
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		writeAutoReplyInternalError(w, "RESUME_READ_FAILED", "简历附件暂时没读出来", err)
		return
	}
	w.Header().Set("Content-Type", item.MIMEType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.OriginalName}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, item.OriginalName, stat.ModTime(), file)
}

// agentAttachments 读取附件元数据或保存本地 Agent 下载到的简历附件。
func (s *AutoReplyService) agentAttachments(w http.ResponseWriter, r *http.Request) {
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.store.ListResumeAttachments(
			r.Context(), requestContext.Tenant.ID,
			strings.TrimSpace(r.URL.Query().Get("candidate_id")),
			strings.TrimSpace(r.URL.Query().Get("conversation_id")),
		)
		if err != nil {
			writeAutoReplyStoreError(w, err, "简历附件暂时没读出来")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "attachments": items})
		return
	}
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "简历附件只支持读取或上传")
		return
	}
	s.saveResumeAttachment(w, r, requestContext)
}

// saveResumeAttachment 验证并原子保存单个简历附件，数据库失败时清理新文件。
func (s *AutoReplyService) saveResumeAttachment(w http.ResponseWriter, r *http.Request, requestContext autoReplyRequestContext) {
	r.Body = http.MaxBytesReader(w, r.Body, AutoReplyMaxAttachmentBytes+autoReplyMultipartOverhead)
	if err := r.ParseMultipartForm(AutoReplyMaxAttachmentBytes + autoReplyMultipartOverhead); err != nil {
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "RESUME_TOO_LARGE", "简历附件不能超过20MB")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_FILE_REQUIRED", "这次没有收到简历附件")
		return
	}
	defer file.Close()
	if header.Size < 0 || header.Size > AutoReplyMaxAttachmentBytes {
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "RESUME_TOO_LARGE", "简历附件不能超过20MB")
		return
	}
	candidateID := strings.TrimSpace(r.FormValue("candidate_id"))
	conversationID := strings.TrimSpace(r.FormValue("conversation_id"))
	if candidateID == "" && conversationID == "" {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_OWNER_REQUIRED", "简历附件需要关联候选人或临时会话")
		return
	}
	ownerSegment := firstNonEmpty(candidateID, conversationID)
	if !safeAutoReplyStorageSegment(requestContext.Tenant.ID) || !safeAutoReplyStorageSegment(ownerSegment) {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_OWNER_INVALID", "简历附件关联标识格式不正确")
		return
	}
	originalName := filepath.Base(strings.TrimSpace(header.Filename))
	ext := strings.ToLower(filepath.Ext(originalName))
	prefix := make([]byte, 512)
	prefixSize, readErr := io.ReadFull(file, prefix)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_READ_FAILED", "简历附件没读完整，请重新上传")
		return
	}
	prefix = prefix[:prefixSize]
	mimeType, valid := detectResumeMIME(ext, prefix)
	if !valid {
		writeAutoReplyError(w, http.StatusBadRequest, "RESUME_TYPE_NOT_ALLOWED", "简历只支持 PDF、DOC、DOCX、JPG 和 PNG")
		return
	}
	root := strings.TrimSpace(s.resumeDir)
	if root == "" {
		root = "data/auto-reply-resumes"
	}
	relativeDir := filepath.Join(requestContext.Tenant.ID, ownerSegment)
	absoluteDir := filepath.Join(root, relativeDir)
	if err := os.MkdirAll(absoluteDir, 0o750); err != nil {
		writeAutoReplyInternalError(w, "RESUME_DIRECTORY_FAILED", "简历保存目录暂时没准备好", err)
		return
	}
	temporary, err := os.CreateTemp(absoluteDir, ".resume-upload-*")
	if err != nil {
		writeAutoReplyInternalError(w, "RESUME_SAVE_FAILED", "简历附件暂时没保存成功", err)
		return
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.MultiReader(bytes.NewReader(prefix), io.LimitReader(file, AutoReplyMaxAttachmentBytes+1)))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil {
		writeAutoReplyInternalError(w, "RESUME_SAVE_FAILED", "简历附件暂时没保存完整，请重新试一次", fmt.Errorf("copy=%v close=%v", copyErr, closeErr))
		return
	}
	if written > AutoReplyMaxAttachmentBytes {
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "RESUME_TOO_LARGE", "简历附件不能超过20MB")
		return
	}
	sha := hex.EncodeToString(hash.Sum(nil))
	fileName := sha + ext
	relativePath := filepath.Join(relativeDir, fileName)
	absolutePath := filepath.Join(root, relativePath)
	createdFile := false
	if _, statErr := os.Stat(absolutePath); os.IsNotExist(statErr) {
		if err = os.Rename(temporaryPath, absolutePath); err != nil {
			writeAutoReplyInternalError(w, "RESUME_SAVE_FAILED", "简历附件暂时没放进保存目录", err)
			return
		}
		createdFile = true
	}
	userID, err := s.store.activeTenantUserID(r.Context(), requestContext.Tenant.ID, requestContext.Session.Email)
	if err != nil {
		if createdFile {
			_ = os.Remove(absolutePath)
		}
		writeAutoReplyStoreError(w, err, "简历上传账号没确认成功")
		return
	}
	item, err := s.store.SaveResumeAttachment(r.Context(), StoredResumeAttachment{
		TenantID: requestContext.Tenant.ID, CandidateID: candidateID, ConversationID: conversationID,
		SourceMessageID: strings.TrimSpace(r.FormValue("source_message_id")),
		PlatformID:      strings.TrimSpace(r.FormValue("platform_id")), OriginalName: originalName,
		StoragePath: filepath.ToSlash(relativePath), SHA256: sha, MIMEType: mimeType,
		SizeBytes: written, ExtractedText: r.FormValue("extracted_text"), CreatedByUserID: userID,
	})
	if err != nil {
		if createdFile {
			_ = os.Remove(absolutePath)
		}
		writeAutoReplyStoreError(w, err, "简历附件元数据没保存成功")
		return
	}
	if createdFile && item.StoragePath != filepath.ToSlash(relativePath) {
		_ = os.Remove(absolutePath)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "attachment": item})
}

// detectResumeMIME 根据扩展名和文件头确认简历是真实的受支持文件。
func detectResumeMIME(ext string, prefix []byte) (string, bool) {
	switch ext {
	case ".pdf":
		return "application/pdf", bytes.HasPrefix(prefix, []byte("%PDF-"))
	case ".doc":
		ole := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
		return "application/msword", bytes.HasPrefix(prefix, ole) || bytes.HasPrefix(prefix, []byte("{\\rtf"))
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", bytes.HasPrefix(prefix, []byte("PK\x03\x04"))
	case ".jpg", ".jpeg":
		return "image/jpeg", len(prefix) >= 3 && prefix[0] == 0xff && prefix[1] == 0xd8 && prefix[2] == 0xff
	case ".png":
		return "image/png", bytes.HasPrefix(prefix, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a})
	default:
		return "", false
	}
}

// safeAutoReplyStorageSegment 确认数据库标识可直接作为持久化目录中的单层名称。
func safeAutoReplyStorageSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// resumeStoragePath 返回附件在云端持久化目录中的绝对路径，供受保护下载接口复用。
func (s *AutoReplyService) resumeStoragePath(relativePath string) (string, error) {
	root := strings.TrimSpace(s.resumeDir)
	if root == "" {
		root = "data/auto-reply-resumes"
	}
	clean := filepath.Clean(strings.TrimSpace(relativePath))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("简历附件路径不安全")
	}
	return filepath.Join(root, clean), nil
}
