// Package cloud 本文件负责本地自动回复简历附件的大小、哈希和 multipart 上传。
package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// UploadAutoReplyAttachment 校验20MB边界并上传附件，再核对云端保存的大小和 SHA-256。
func (c *Client) UploadAutoReplyAttachment(ctx context.Context, credentials AgentCredentials, upload AutoReplyAttachmentUpload) (StoredResumeAttachment, error) {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return StoredResumeAttachment{}, err
	}
	if strings.TrimSpace(upload.CandidateID) == "" && strings.TrimSpace(upload.ConversationID) == "" {
		return StoredResumeAttachment{}, fmt.Errorf("简历附件需要关联候选人或临时会话")
	}
	file, stat, err := openAutoReplyAttachment(upload.FilePath)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(strings.TrimSpace(upload.FilePath)))
	if err != nil {
		return StoredResumeAttachment{}, fmt.Errorf("准备简历附件上传失败")
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(part, hash), file)
	if err != nil {
		return StoredResumeAttachment{}, fmt.Errorf("读取简历附件失败")
	}
	fields := map[string]string{
		"candidate_id":      strings.TrimSpace(upload.CandidateID),
		"conversation_id":   strings.TrimSpace(upload.ConversationID),
		"source_message_id": strings.TrimSpace(upload.SourceMessageID),
		"platform_id":       strings.TrimSpace(upload.PlatformID),
		"extracted_text":    upload.ExtractedText,
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return StoredResumeAttachment{}, fmt.Errorf("准备简历附件信息失败")
		}
	}
	if err := writer.Close(); err != nil {
		return StoredResumeAttachment{}, fmt.Errorf("完成简历附件上传数据失败")
	}
	if written != stat.Size() {
		return StoredResumeAttachment{}, fmt.Errorf("简历附件读取不完整")
	}
	expectedHash := hex.EncodeToString(hash.Sum(nil))
	var response struct {
		Attachment StoredResumeAttachment `json:"attachment"`
	}
	if err := c.doBody(ctx, "POST", autoReplyAgentBasePath+"/attachments", credentials.Token, credentials.MachineID, writer.FormDataContentType(), &body, &response); err != nil {
		return StoredResumeAttachment{}, err
	}
	if response.Attachment.SizeBytes != written || !strings.EqualFold(response.Attachment.SHA256, expectedHash) {
		return StoredResumeAttachment{}, fmt.Errorf("云端简历附件校验没有通过，请重新上传")
	}
	return response.Attachment, nil
}

// openAutoReplyAttachment 打开附件并返回不泄露本地完整路径的安全错误。
func openAutoReplyAttachment(path string) (*os.File, os.FileInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, fmt.Errorf("简历附件路径不能为空")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, safeAutoReplyFileError("打开简历附件失败", err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, safeAutoReplyFileError("读取简历附件信息失败", err)
	}
	if !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("简历附件必须是普通文件")
	}
	if stat.Size() <= 0 {
		_ = file.Close()
		return nil, nil, fmt.Errorf("简历附件不能为空文件")
	}
	if stat.Size() > AutoReplyMaxAttachmentBytes {
		_ = file.Close()
		return nil, nil, fmt.Errorf("简历附件不能超过20MB，当前大小为%s字节", strconv.FormatInt(stat.Size(), 10))
	}
	return file, stat, nil
}

// safeAutoReplyFileError 把文件系统错误转换为不包含本地完整路径的中文提示。
func safeAutoReplyFileError(action string, err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s：文件不存在", action)
	case errors.Is(err, os.ErrPermission):
		return fmt.Errorf("%s：没有读取权限", action)
	default:
		return fmt.Errorf("%s", action)
	}
}
