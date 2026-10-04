// Package cloud 本文件负责候选人头像的 Base64 优先上传和 multipart 文件后备上传。
package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

// UploadAutoReplyAvatarBase64 使用 JSON Base64 优先上传候选人头像。
// candidateID 为云端正式候选人编号，imageData 为不超过1MB的 PNG 或 JPEG 原图。
func (c *Client) UploadAutoReplyAvatarBase64(ctx context.Context, credentials AgentCredentials, candidateID string, imageData []byte) (AutoReplyAvatarUploadResult, error) {
	if err := validateAutoReplyAvatarUpload(credentials, candidateID, int64(len(imageData))); err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	payload := struct {
		CandidateID string `json:"candidate_id"`
		ImageBase64 string `json:"image_base64"`
	}{
		CandidateID: strings.TrimSpace(candidateID),
		ImageBase64: base64.StdEncoding.EncodeToString(imageData),
	}
	var result AutoReplyAvatarUploadResult
	if err := c.doWithMachineID(ctx, "POST", autoReplyAgentBasePath+"/avatars", credentials.Token, credentials.MachineID, payload, &result); err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	if strings.TrimSpace(result.AvatarURL) == "" {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("云端没有返回候选人头像地址")
	}
	return result, nil
}

// UploadAutoReplyAvatarFile 使用 multipart 文件上传作为 Base64 请求失败后的后备方式。
// candidateID 为云端正式候选人编号，filePath 为本地 PNG 或 JPEG 文件。
func (c *Client) UploadAutoReplyAvatarFile(ctx context.Context, credentials AgentCredentials, candidateID string, filePath string) (AutoReplyAvatarUploadResult, error) {
	file, stat, err := openAutoReplyAvatar(filePath)
	if err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	defer file.Close()
	if err = validateAutoReplyAvatarUpload(credentials, candidateID, stat.Size()); err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(strings.TrimSpace(filePath)))
	if err != nil {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("准备候选人头像上传失败")
	}
	written, err := io.Copy(part, file)
	if err != nil || written != stat.Size() {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("读取候选人头像失败")
	}
	if err = writer.WriteField("candidate_id", strings.TrimSpace(candidateID)); err != nil {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("准备候选人头像信息失败")
	}
	if err = writer.Close(); err != nil {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("完成候选人头像上传数据失败")
	}
	var result AutoReplyAvatarUploadResult
	if err = c.doBody(ctx, "POST", autoReplyAgentBasePath+"/avatars", credentials.Token, credentials.MachineID, writer.FormDataContentType(), &body, &result); err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	if strings.TrimSpace(result.AvatarURL) == "" {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("云端没有返回候选人头像地址")
	}
	return result, nil
}

// validateAutoReplyAvatarUpload 校验头像上传必需的设备凭证、候选人编号和大小。
func validateAutoReplyAvatarUpload(credentials AgentCredentials, candidateID string, size int64) error {
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return err
	}
	if strings.TrimSpace(candidateID) == "" {
		return fmt.Errorf("候选人编号不能为空")
	}
	if size <= 0 {
		return fmt.Errorf("候选人头像不能为空")
	}
	if size > AutoReplyMaxAvatarBytes {
		return fmt.Errorf("候选人头像不能超过1MB")
	}
	return nil
}

// openAutoReplyAvatar 打开头像文件并返回不泄露本地完整路径的安全错误。
func openAutoReplyAvatar(path string) (*os.File, os.FileInfo, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil, fmt.Errorf("候选人头像路径不能为空")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, safeAutoReplyFileError("打开候选人头像失败", err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, safeAutoReplyFileError("读取候选人头像信息失败", err)
	}
	if !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("候选人头像必须是普通文件")
	}
	return file, stat, nil
}
