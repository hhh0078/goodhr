// Package httpapi 本文件负责候选人头像的鉴权上传、安全重编码、自托管保存和公开只读访问。
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	autoReplyMaxAvatarBytes     int64 = 1 << 20
	autoReplyMaxAvatarDimension       = 2048
	candidateAvatarPublicPrefix       = "/api/public/candidate-avatars/"
	candidateAvatarStorageDir         = "_candidate-avatars"
)

type autoReplyAvatarJSONRequest struct {
	CandidateID string `json:"candidate_id"`
	ImageBase64 string `json:"image_base64"`
}

// agentCandidateAvatar 接收本地 Agent 的 Base64 或 multipart 头像并关联正式候选人。
func (s *AutoReplyService) agentCandidateAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "候选人头像这里只支持上传")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, true, true)
	if !ok {
		return
	}
	candidateID, imageData, err := readAutoReplyAvatarUpload(w, r)
	if err != nil {
		return
	}
	avatarURL, err := s.saveCandidateAvatarImage(r.Context(), requestContext.Tenant.ID, candidateID, imageData)
	var validationErr *autoReplyValidationError
	if errors.As(err, &validationErr) {
		writeAutoReplyError(w, http.StatusBadRequest, "CANDIDATE_AVATAR_INVALID", validationErr.Error())
		return
	}
	if errors.Is(err, ErrNotFound) {
		writeAutoReplyError(w, http.StatusNotFound, "CANDIDATE_NOT_FOUND", "这份正式简历没有找到，头像暂时没保存")
		return
	}
	if err != nil {
		writeAutoReplyInternalError(w, "CANDIDATE_AVATAR_SAVE_FAILED", "候选人头像暂时没保存成功，简历不受影响", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "avatar_url": avatarURL})
}

// readAutoReplyAvatarUpload 按 Content-Type 读取 Base64 或 multipart 头像请求。
func readAutoReplyAvatarUpload(w http.ResponseWriter, r *http.Request) (string, []byte, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "AVATAR_CONTENT_TYPE_INVALID", "候选人头像上传格式没读明白")
		return "", nil, err
	}
	if mediaType == "application/json" {
		var payload autoReplyAvatarJSONRequest
		if err = decodeAutoReplyJSON(w, r, &payload); err != nil {
			return "", nil, err
		}
		imageData, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(payload.ImageBase64))
		if decodeErr != nil {
			writeAutoReplyError(w, http.StatusBadRequest, "AVATAR_BASE64_INVALID", "候选人头像 Base64 数据不完整")
			return "", nil, decodeErr
		}
		return strings.TrimSpace(payload.CandidateID), imageData, nil
	}
	if mediaType != "multipart/form-data" {
		err = fmt.Errorf("unsupported content type %s", mediaType)
		writeAutoReplyError(w, http.StatusUnsupportedMediaType, "AVATAR_CONTENT_TYPE_INVALID", "候选人头像只支持 Base64、PNG 或 JPEG 文件")
		return "", nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, autoReplyMaxAvatarBytes+autoReplyMultipartOverhead)
	if err = r.ParseMultipartForm(autoReplyMaxAvatarBytes + autoReplyMultipartOverhead); err != nil {
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "候选人头像不能超过1MB")
		return "", nil, err
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAutoReplyError(w, http.StatusBadRequest, "AVATAR_FILE_REQUIRED", "这次没有收到候选人头像")
		return "", nil, err
	}
	defer file.Close()
	if header.Size < 0 || header.Size > autoReplyMaxAvatarBytes {
		err = fmt.Errorf("avatar size out of range")
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "候选人头像不能超过1MB")
		return "", nil, err
	}
	imageData, err := io.ReadAll(io.LimitReader(file, autoReplyMaxAvatarBytes+1))
	if err != nil || int64(len(imageData)) > autoReplyMaxAvatarBytes {
		if err == nil {
			err = fmt.Errorf("avatar exceeds size limit")
		}
		writeAutoReplyError(w, http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "候选人头像不能超过1MB")
		return "", nil, err
	}
	return strings.TrimSpace(r.FormValue("candidate_id")), imageData, nil
}

// saveCandidateAvatarImage 校验候选人归属，把图片重编码为 PNG 并原子更新头像路径。
func (s *AutoReplyService) saveCandidateAvatarImage(ctx context.Context, tenantID string, candidateID string, imageData []byte) (string, error) {
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" {
		return "", newAutoReplyValidationError("候选人编号不能为空")
	}
	pngData, err := normalizeCandidateAvatarImage(imageData)
	if err != nil {
		return "", err
	}
	store, ok := s.candidates.(candidateAvatarStore)
	if !ok || store == nil {
		return "", fmt.Errorf("候选人头像存储还没准备好")
	}
	token, err := newCandidateAvatarToken()
	if err != nil {
		return "", err
	}
	directory := s.candidateAvatarDirectory()
	if err = os.MkdirAll(directory, 0o750); err != nil {
		return "", fmt.Errorf("创建候选人头像目录失败：%w", err)
	}
	filePath := filepath.Join(directory, token+".png")
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", fmt.Errorf("写入候选人头像失败：%w", err)
	}
	written, writeErr := file.Write(pngData)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || written != len(pngData) {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("候选人头像没有写完整")
	}
	avatarURL := candidateAvatarPublicPrefix + token + ".png"
	previousURL, err := store.UpdateCandidateAvatar(ctx, tenantID, candidateID, avatarURL)
	if err != nil {
		_ = os.Remove(filePath)
		return "", err
	}
	s.cleanupPreviousCandidateAvatar(ctx, store, previousURL, avatarURL)
	return avatarURL, nil
}

// cleanupPreviousCandidateAvatar 在头像替换后清理已无候选人或公开推荐引用的旧自托管文件。
// 引用查询失败时宁可保留旧图，避免影响已经生成的公开推荐报告。
func (s *AutoReplyService) cleanupPreviousCandidateAvatar(ctx context.Context, store candidateAvatarStore, previousURL string, currentURL string) {
	previousURL = strings.TrimSpace(previousURL)
	if previousURL == "" || previousURL == strings.TrimSpace(currentURL) {
		return
	}
	path, err := s.candidateAvatarPathFromURL(previousURL)
	if err != nil {
		return
	}
	referenced, err := store.CandidateAvatarPubliclyReferenced(ctx, previousURL)
	if err != nil || referenced {
		return
	}
	_ = os.Remove(path)
}

// normalizeCandidateAvatarImage 接受真实 PNG/JPEG，限制1MB和2048像素后完整解码并重新编码 PNG。
func normalizeCandidateAvatarImage(imageData []byte) ([]byte, error) {
	if len(imageData) == 0 {
		return nil, newAutoReplyValidationError("候选人头像不能为空")
	}
	if int64(len(imageData)) > autoReplyMaxAvatarBytes {
		return nil, newAutoReplyValidationError("候选人头像不能超过1MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, newAutoReplyValidationError("候选人头像只支持真实的 PNG 或 JPEG 图片")
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > autoReplyMaxAvatarDimension || config.Height > autoReplyMaxAvatarDimension {
		return nil, newAutoReplyValidationError("候选人头像宽高不能超过2048像素")
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(imageData))
	if err != nil || decodedFormat != format {
		return nil, newAutoReplyValidationError("候选人头像没有完整解码")
	}
	var normalized bytes.Buffer
	if err = png.Encode(&normalized, decoded); err != nil {
		return nil, fmt.Errorf("候选人头像重新编码失败：%w", err)
	}
	if int64(normalized.Len()) > autoReplyMaxAvatarBytes {
		return nil, newAutoReplyValidationError("候选人头像转成安全图片后超过1MB，请换一张小一点的")
	}
	return normalized.Bytes(), nil
}

// PublicCandidateAvatar 公开读取不可猜测 token 对应的自托管 PNG 头像。
func (s *AutoReplyService) PublicCandidateAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	avatarURL := candidateAvatarPublicPrefix + strings.TrimPrefix(r.URL.Path, candidateAvatarPublicPrefix)
	path, err := s.candidateAvatarPathFromURL(avatarURL)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	store, ok := s.candidates.(candidateAvatarStore)
	if !ok || store == nil {
		http.NotFound(w, r)
		return
	}
	referenced, err := store.CandidateAvatarPubliclyReferenced(r.Context(), avatarURL)
	if err != nil || !referenced {
		if err == nil {
			_ = os.Remove(path)
		}
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// 头像引用会在候选人删除或推荐分享撤销后失效，禁止浏览器和中间缓存继续保留旧图。
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

// candidateAvatarDirectory 返回复用自动回复持久化卷的头像子目录。
func (s *AutoReplyService) candidateAvatarDirectory() string {
	root := strings.TrimSpace(s.resumeDir)
	if root == "" {
		root = "data/auto-reply-resumes"
	}
	return filepath.Join(root, candidateAvatarStorageDir)
}

// candidateAvatarPathFromURL 验证自有头像 URL 并返回不会逃逸持久化目录的文件路径。
func (s *AutoReplyService) candidateAvatarPathFromURL(avatarURL string) (string, error) {
	return candidateAvatarStoragePath(s.resumeDir, avatarURL)
}

// candidateAvatarAvailable 判断数据库头像路径对应的自托管文件是否仍可正常读取。
// 持久化卷文件丢失时返回 false，让本地 Agent 在下一轮自动重新上传。
func (s *AutoReplyService) candidateAvatarAvailable(avatarURL string) bool {
	if strings.TrimSpace(avatarURL) == "" {
		return false
	}
	path, err := s.candidateAvatarPathFromURL(avatarURL)
	if err != nil {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && stat.Mode().IsRegular() && stat.Size() > 0
}

// candidateAvatarStoragePath 验证自托管头像 URL，并返回不会逃逸持久化目录的文件路径。
// resumeDir 为自动回复文件根目录，avatarURL 必须是系统生成的64位随机 PNG 路径。
func candidateAvatarStoragePath(resumeDir string, avatarURL string) (string, error) {
	avatarURL = strings.TrimSpace(avatarURL)
	if !strings.HasPrefix(avatarURL, candidateAvatarPublicPrefix) {
		return "", fmt.Errorf("候选人头像不是自托管地址")
	}
	filename := strings.TrimPrefix(avatarURL, candidateAvatarPublicPrefix)
	if len(filename) != 68 || !strings.HasSuffix(filename, ".png") || !isLowerHex(filename[:64]) {
		return "", fmt.Errorf("候选人头像地址格式不正确")
	}
	root := strings.TrimSpace(resumeDir)
	if root == "" {
		root = "data/auto-reply-resumes"
	}
	return filepath.Join(root, candidateAvatarStorageDir, filename), nil
}

// newCandidateAvatarToken 生成32字节随机头像访问 token。
func newCandidateAvatarToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成候选人头像地址失败：%w", err)
	}
	return hex.EncodeToString(buffer), nil
}

// isLowerHex 判断字符串是否只包含小写十六进制字符。
func isLowerHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
