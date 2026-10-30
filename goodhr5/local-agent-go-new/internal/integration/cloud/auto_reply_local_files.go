// Package cloud 本文件负责本机简历附件、候选人入库和脱敏通知，完整文件不会上传云端。
package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// saveLocalAutoReplyAttachment 校验并复制真实附件到私有本地目录，再记录哈希和归属。
func (c *Client) saveLocalAutoReplyAttachment(ctx context.Context, credentials AgentCredentials, upload AutoReplyAttachmentUpload) (StoredResumeAttachment, error) {
	if !c.LocalAutoReplyEnabled() {
		return StoredResumeAttachment{}, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	file, stat, err := openAutoReplyAttachment(upload.FilePath)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	defer file.Close()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	if !filepath.IsAbs(c.localAutoDir) {
		return StoredResumeAttachment{}, fmt.Errorf("本地简历目录尚未准备好")
	}
	directory := filepath.Join(c.localAutoDir, autoReplyDigest(c.localAutoReplyKey(credentials)), "attachments")
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return StoredResumeAttachment{}, fmt.Errorf("准备本地简历目录失败")
	}
	temporary, err := os.CreateTemp(directory, "resume-*")
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	defer os.Remove(temporary.Name())
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(file, AutoReplyMaxAttachmentBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil || size != stat.Size() || size > AutoReplyMaxAttachmentBytes {
		return StoredResumeAttachment{}, fmt.Errorf("简历附件没有完整保存在本机")
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	id := "attachment-" + autoReplyDigest(upload.ConversationID+digest)
	if existing, ok := data.Attachments[id]; ok {
		return existing, nil
	}
	path := filepath.Join(directory, digest+strings.ToLower(filepath.Ext(upload.FilePath)))
	if err = os.Rename(temporary.Name(), path); err != nil {
		return StoredResumeAttachment{}, fmt.Errorf("保存本地简历附件失败")
	}
	item := StoredResumeAttachment{ID: id, CandidateID: upload.CandidateID, ConversationID: upload.ConversationID, SourceMessageID: upload.SourceMessageID, PlatformID: upload.PlatformID, OriginalName: filepath.Base(upload.FilePath), SHA256: digest, SizeBytes: size, ExtractedText: upload.ExtractedText, CreatedAt: time.Now().UTC()}
	data.Attachments[id] = item
	data.AttachmentPaths[id] = path
	if err = c.saveLocalAutoReply(ctx, credentials, data); err != nil {
		return StoredResumeAttachment{}, err
	}
	return item, nil
}

// LocalAutoReplyAttachmentPaths 仅向本地解析器返回已确认归属的持久附件路径。
func (c *Client) LocalAutoReplyAttachmentPaths(ctx context.Context, credentials AgentCredentials, conversationID string, ids []string) ([]string, error) {
	if !c.LocalAutoReplyEnabled() {
		return nil, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, id := range ids {
		item, ok := data.Attachments[id]
		if !ok || item.ConversationID != conversationID {
			return nil, fmt.Errorf("简历附件与当前候选人不一致")
		}
		path := data.AttachmentPaths[id]
		if path == "" {
			return nil, fmt.Errorf("本地简历附件没找到")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// saveLocalAutoReplyCandidate 先保存本机简历，仅在岗位明确授权后异步同步去原文的结构化资料。
func (c *Client) saveLocalAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, input AutoReplyCandidateInput) (AutoReplyCandidateSaveResult, error) {
	if !c.LocalAutoReplyEnabled() {
		return AutoReplyCandidateSaveResult{}, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return AutoReplyCandidateSaveResult{}, err
	}
	id := "candidate-" + autoReplyDigest(input.Phone)
	identityID := "identity-" + autoReplyDigest(input.PlatformID+"|"+input.PlatformAccountID+"|"+input.PlatformCandidateID)
	if previous, ok := data.Identities[identityID]; ok && previous.CandidateID != "" {
		id = previous.CandidateID
	}
	candidate := AutoReplyStoredCandidate{ID: id, CandidateName: input.CandidateName, AvatarURL: input.AvatarURL, Gender: input.Gender, BirthYM: input.BirthYM, BirthYMPrecision: input.BirthYMPrecision, Phone: input.Phone, Email: input.Email, Wechat: input.Wechat, WorkRegion: input.WorkRegion, WorkYears: input.WorkYears, ExpectedSalaryMin: input.ExpectedSalaryMin, ExpectedSalaryMax: input.ExpectedSalaryMax, BasicInfo: input.BasicInfo, EducationLevel: input.EducationLevel, ExpectedPosition: input.ExpectedPosition, OnlineStatus: input.OnlineStatus, PersonalDescription: input.PersonalDescription, WorkStatus: input.WorkStatus, RawText: input.RawText, WorkExperiences: input.WorkExperiences, Educations: input.Educations, Certificates: input.Certificates, Honors: input.Honors, ProjectExperiences: input.ProjectExperiences, Communications: input.ColleagueCommunications}
	data.Candidates[id] = candidate
	identity := data.Identities[identityID]
	identity.ID = identityID
	identity.CandidateID = id
	identity.PlatformID = input.PlatformID
	identity.PlatformAccountID = input.PlatformAccountID
	identity.PlatformCandidateID = input.PlatformCandidateID
	identity.CandidateName = input.CandidateName
	identity.NormalizedPhone = input.Phone
	data.Identities[identityID] = identity
	if err = c.saveLocalAutoReply(ctx, credentials, data); err != nil {
		return AutoReplyCandidateSaveResult{}, err
	}
	return AutoReplyCandidateSaveResult{OK: true, CandidateID: id, PlatformIdentity: identity}, nil
}

// SyncAutoReplyStructuredCandidate 在当前岗位授权后同步结构化资料；失败由调用方记录而不回滚本地结果。
func (c *Client) SyncAutoReplyStructuredCandidate(ctx context.Context, credentials AgentCredentials, localCandidateID string, input AutoReplyCandidateInput) error {
	if !c.LocalAutoReplyEnabled() {
		return nil
	}
	safe := input
	safe.RawText = ""
	safe.BasicInfo = ""
	safe.ColleagueCommunications = nil
	safe.AvatarURL = ""
	encoded, err := json.Marshal(safe)
	if err != nil {
		return err
	}
	cacheKey := localCandidateID + "|" + input.PositionID
	hash := autoReplyDigest(string(encoded))
	c.localAutoMu.Lock()
	cached, cacheErr := c.loadLocalAutoReply(ctx, credentials)
	c.localAutoMu.Unlock()
	if cacheErr != nil {
		return cacheErr
	}
	if cached.CloudCandidateHashes[cacheKey] == hash {
		return nil
	}
	var result AutoReplyCandidateSaveResult
	if err = c.doBody(ctx, http.MethodPost, autoReplyAgentBasePath+"/candidates", credentials.Token, credentials.MachineID, "application/json", bytes.NewReader(encoded), &result); err != nil {
		return err
	}
	if result.CandidateID == "" {
		return fmt.Errorf("云端没有确认结构化简历保存结果")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return err
	}
	data.CloudCandidates[localCandidateID] = result.CandidateID
	data.CloudCandidateHashes[cacheKey] = hash
	return c.saveLocalAutoReply(ctx, credentials, data)
}

// saveLocalAutoReplyAvatar 把真实头像作为本地图片数据保存，不产生公开文件或远程地址。
func (c *Client) saveLocalAutoReplyAvatar(ctx context.Context, credentials AgentCredentials, id string, imageData []byte) (AutoReplyAvatarUploadResult, error) {
	if !c.LocalAutoReplyEnabled() {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	candidate, ok := data.Candidates[id]
	if !ok {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("本地候选人还没保存")
	}
	mime := http.DetectContentType(imageData)
	if mime != "image/png" && mime != "image/jpeg" {
		return AutoReplyAvatarUploadResult{}, fmt.Errorf("候选人头像需要PNG或JPEG图片")
	}
	candidate.AvatarURL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(imageData)
	data.Candidates[id] = candidate
	if err = c.saveLocalAutoReply(ctx, credentials, data); err != nil {
		return AutoReplyAvatarUploadResult{}, err
	}
	return AutoReplyAvatarUploadResult{AvatarURL: candidate.AvatarURL}, nil
}

// notifyLocalAutoReply 先在本机去重，云端只收到人工协助摘要，不接收聊天原文或AI输出。
func (c *Client) notifyLocalAutoReply(ctx context.Context, credentials AgentCredentials, input AutoReplyNotification) (AutoReplyNotificationResult, error) {
	if !c.LocalAutoReplyEnabled() {
		return AutoReplyNotificationResult{}, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	c.localAutoMu.Lock()
	defer c.localAutoMu.Unlock()
	data, err := c.loadLocalAutoReply(ctx, credentials)
	if err != nil {
		return AutoReplyNotificationResult{}, err
	}
	id := autoReplyDigest(input.ConversationID + input.BasedOnMessageKey + input.ReasonKey)
	if result, ok := data.Notifications[id]; ok && result.EmailSent {
		result.Duplicate = true
		return result, nil
	}
	safe := input
	safe.ID = id
	safe.ConversationID = ""
	safe.LatestMessage = ""
	safe.Reason = "需要人工协助，请在本地运行记录查看详情。"
	safe.ReasonKey = autoReplyDigest(input.ReasonKey)
	safe.BasedOnMessageKey = autoReplyDigest(input.BasedOnMessageKey)
	encoded, _ := json.Marshal(safe)
	var result AutoReplyNotificationResult
	if err = c.doBody(ctx, http.MethodPost, autoReplyAgentBasePath+"/notifications", credentials.Token, credentials.MachineID, "application/json", bytes.NewReader(encoded), &result); err != nil {
		return result, err
	}
	result.Notification = input
	data.Notifications[id] = result
	if err = c.saveLocalAutoReply(ctx, credentials, data); err != nil {
		return result, err
	}
	return result, nil
}
