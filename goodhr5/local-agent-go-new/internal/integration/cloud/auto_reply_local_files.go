// Package cloud 本文件管理本机附件相对路径和独立候选人身份，所有网络同步通过持久待办执行。
package cloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// localAttachment 将附件内容元数据与账号私有目录下的相对路径一起持久化。
type localAttachment struct {
	StoredResumeAttachment
	RelativePath string `json:"relative_path"`
}

// localCloudLink 保存本地实体与已授权云端结果的关联或同步摘要。
type localCloudLink struct {
	Value string `json:"value"`
}

// saveLocalAutoReplyAttachment 先复制并校验文件，再用短事务登记引用；支持Windows和两种Mac架构。
func (c *Client) saveLocalAutoReplyAttachment(ctx context.Context, credentials AgentCredentials, upload AutoReplyAttachmentUpload) (StoredResumeAttachment, error) {
	if !c.LocalAutoReplyEnabled() {
		return StoredResumeAttachment{}, fmt.Errorf("本地自动回复存储尚未准备好")
	}
	if !filepath.IsAbs(c.localAutoDir) {
		return StoredResumeAttachment{}, fmt.Errorf("本地简历目录尚未准备好")
	}
	file, stat, err := openAutoReplyAttachment(upload.FilePath)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	defer file.Close()
	directory := filepath.Join(c.localAutoDir, autoReplyDigest(c.localAutoReplyKey(credentials)), "attachments")
	if err = os.MkdirAll(directory, 0700); err != nil {
		return StoredResumeAttachment{}, err
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
	path := filepath.Join(directory, digest+strings.ToLower(filepath.Ext(upload.FilePath)))
	relative, err := filepath.Rel(c.localAutoDir, path)
	if err != nil {
		return StoredResumeAttachment{}, err
	}
	item := StoredResumeAttachment{ID: id, CandidateID: upload.CandidateID, ConversationID: upload.ConversationID, SourceMessageID: upload.SourceMessageID, PlatformID: upload.PlatformID, OriginalName: filepath.Base(upload.FilePath), SHA256: digest, SizeBytes: size, ExtractedText: upload.ExtractedText, CreatedAt: time.Now().UTC()}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conv, _, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", upload.ConversationID)
		if err != nil {
			return err
		}
		if conv.DeletionBoundary != nil {
			// 上传协议传本机 messages 主键，确认 belongs-to 后按真实平台键构建证明。
			message, found, err := getAutoReplyEntity[AutoReplyMessage](tx, "messages", upload.SourceMessageID)
			if err != nil {
				return err
			}
			if !found || message.ConversationID != conv.ID || (upload.CandidateID != "" && upload.CandidateID != conv.CandidateID) {
				return fmt.Errorf("附件与本次重新联系的来源不一致")
			}
			if _, err := contactAttachmentProof(tx, conv, localAutoReplyMessageKey(message)); err != nil {
				return err
			}
			id = "attachment-" + autoReplyDigest(conv.ID+"|"+upload.SourceMessageID+"|"+digest)
			item.ID = id
		}
		old, exists, err := getAutoReplyEntity[localAttachment](tx, "attachments", id)
		if err != nil {
			return err
		}
		if exists {
			item = old.StoredResumeAttachment
		}
		// 先在同一写事务核对删除墓碑，再发布暂存副本；共享附件的引用核验和最后一次回收由SQLite串行。
		if err := tx.Put("attachments", id, localAttachment{StoredResumeAttachment: item, RelativePath: relative}); err != nil {
			return err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			// Windows不能Rename覆盖已有文件；macOS采用相同的按摘要复用约定。
			if err := os.Rename(temporary.Name(), path); err != nil {
				return fmt.Errorf("保存本地简历附件失败")
			}
		} else if err != nil {
			return err
		}
		return nil
	})
	return item, err
}

// LocalAutoReplyAttachmentPaths 从当前数据目录解析相对路径，并复核归属与文件哈希以安全支持数据搬家。
func (c *Client) LocalAutoReplyAttachmentPaths(ctx context.Context, credentials AgentCredentials, conversationID string, ids []string) ([]string, error) {
	records := []localAttachment{}
	err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		for _, id := range ids {
			item, ok, err := getAutoReplyEntity[localAttachment](tx, "attachments", id)
			if err != nil {
				return err
			}
			if !ok || item.ConversationID != conversationID {
				return fmt.Errorf("简历附件与当前会话不一致")
			}
			records = append(records, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, item := range records {
		clean := filepath.Clean(item.RelativePath)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("本地附件目录归属不正确")
		}
		path := filepath.Join(c.localAutoDir, clean)
		file, stat, err := openAutoReplyAttachment(path)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, file)
		file.Close()
		if readErr != nil || stat.Size() != item.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return nil, fmt.Errorf("本地附件校验未通过，请重新获取附件")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// newLocalAutoReplyCandidateID 生成不依赖手机号和云端入库的本地稳定编号。
func newLocalAutoReplyCandidateID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "candidate-" + hex.EncodeToString(raw), nil
}

// saveLocalAutoReplyCandidate 按已有身份定位本地资料，并只用本轮非空字段补充，不以空值覆盖旧来源。
func (c *Client) saveLocalAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, input AutoReplyCandidateInput) (result AutoReplyCandidateSaveResult, err error) {
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		identityID := "identity-" + autoReplyDigest(input.PlatformID+"|"+input.PlatformAccountID+"|"+input.PlatformCandidateID)
		conv, err := candidateContact(tx, input.LocalCandidateID)
		if err != nil {
			return err
		}
		if conv.DeletionBoundary != nil {
			if conv.PlatformID != input.PlatformID || conv.PlatformAccountID != input.PlatformAccountID || conv.DeletionBoundary.SourceHash != autoReplyDigest(input.PlatformID+"\x00"+input.PlatformCandidateID) {
				return fmt.Errorf("新简历来源与重新联系的会话不一致")
			}
			if _, err := storedContactAttachmentProof(tx, conv, input.ResumeSourceMessageID); err != nil {
				return err
			}
			identityID = conv.PlatformIdentityID
		}
		identity, _, err := getAutoReplyEntity[CandidatePlatformIdentity](tx, "identities", identityID)
		if err != nil {
			return err
		}
		id := input.LocalCandidateID
		if id == "" {
			id = identity.CandidateID
		}
		if identity.CandidateID != "" && id != identity.CandidateID {
			return fmt.Errorf("简历与当前平台身份不一致，请人工确认")
		}
		if id == "" && input.Phone != "" {
			items, err := scopedAutoReplyPhoneCandidates(tx, input.PlatformID, input.PlatformAccountID, input.Phone)
			if err != nil {
				return err
			}
			if len(items) > 1 {
				return fmt.Errorf("同手机号对应多份资料，请人工确认")
			}
			if len(items) == 1 {
				id = items[0].ID
			}
		}
		var candidate AutoReplyStoredCandidate
		if id != "" {
			candidate, _, err = getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", id)
			if err != nil {
				return err
			}
		} else {
			id, err = newLocalAutoReplyCandidateID()
			if err != nil {
				return err
			}
		}
		candidate.ID = id
		candidate.ResumeStructured = true
		mergeLocalAutoReplyCandidate(&candidate, input)
		if err = tx.Put("candidates", id, candidate); err != nil {
			return err
		}
		if input.PlatformCandidateID != "" {
			identity.ID = identityID
			identity.CandidateID = id
			identity.PlatformID = input.PlatformID
			identity.PlatformAccountID = input.PlatformAccountID
			identity.PlatformCandidateID = input.PlatformCandidateID
			identity.CandidateName = candidate.CandidateName
			identity.NormalizedPhone = candidate.Phone
			identity.LastSeenAt = time.Now().UTC()
			if identity.FirstSeenAt.IsZero() {
				identity.FirstSeenAt = identity.LastSeenAt
			}
			if err = tx.Put("identities", identityID, identity); err != nil {
				return err
			}
		}
		result = AutoReplyCandidateSaveResult{OK: true, CandidateID: id, PlatformIdentity: identity}
		return nil
	})
	return
}

// BackfillAutoReplyBirthMonth 在原子事务内复核会话、候选人原文、来源消息及其附件，只填仍缺失的出生年月。
func (c *Client) BackfillAutoReplyBirthMonth(ctx context.Context, credentials AgentCredentials, conversationID string, before AutoReplyStoredCandidate, month string) (changed bool, err error) {
	if _, parseErr := time.Parse("2006-01", month); parseErr != nil || before.ID == "" || before.ResumeSourceMessageID == "" {
		return false, nil
	}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		conversation, found, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", conversationID)
		if err != nil || !found || conversation.CandidateID != before.ID || (conversation.ResumeRetry != nil && conversation.ResumeRetry.SourceMessageID != before.ResumeSourceMessageID) {
			return err
		}
		current, found, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", before.ID)
		if err != nil || !found || !current.ResumeStructured || current.BirthYM != "" || current.BirthYMPrecision != "" || current.ResumeSourceMessageID != before.ResumeSourceMessageID || current.RawText != before.RawText {
			return err
		}
		row, found, err := tx.MessageByKey(conversationID, current.ResumeSourceMessageID)
		if err != nil || !found {
			return err
		}
		var message AutoReplyMessage
		if err := json.Unmarshal(row.Data, &message); err != nil {
			return err
		}
		if message.Direction != "candidate" || message.MessageType != "resume" {
			return nil
		}
		attachments, err := readLocalAutoReplyAttachments(tx, "", conversationID)
		if err != nil {
			return err
		}
		for _, attachment := range attachments {
			if attachment.SourceMessageID == row.ID && (attachment.CandidateID == "" || attachment.CandidateID == current.ID) {
				current.BirthYM, current.BirthYMPrecision = month, "month"
				changed = true
				return tx.Put("candidates", current.ID, current)
			}
		}
		return nil
	})
	return changed && err == nil, err
}

// mergeLocalAutoReplyCandidate 保留已有资料，只接受本轮有值的结构化字段。
func mergeLocalAutoReplyCandidate(target *AutoReplyStoredCandidate, input AutoReplyCandidateInput) {
	for _, pair := range []struct {
		target *string
		value  string
	}{{&target.ResumeSourceMessageID, input.ResumeSourceMessageID}, {&target.CandidateName, input.CandidateName}, {&target.AvatarURL, input.AvatarURL}, {&target.Gender, input.Gender}, {&target.BirthYM, input.BirthYM}, {&target.BirthYMPrecision, input.BirthYMPrecision}, {&target.Phone, input.Phone}, {&target.Email, input.Email}, {&target.Wechat, input.Wechat}, {&target.WorkRegion, input.WorkRegion}, {&target.WorkYears, input.WorkYears}, {&target.BasicInfo, input.BasicInfo}, {&target.EducationLevel, input.EducationLevel}, {&target.ExpectedPosition, input.ExpectedPosition}, {&target.OnlineStatus, input.OnlineStatus}, {&target.PersonalDescription, input.PersonalDescription}, {&target.WorkStatus, input.WorkStatus}, {&target.RawText, input.RawText}} {
		if strings.TrimSpace(pair.value) != "" {
			*pair.target = pair.value
		}
	}
	if input.ExpectedSalaryMin != nil {
		target.ExpectedSalaryMin = input.ExpectedSalaryMin
	}
	if input.ExpectedSalaryMax != nil {
		target.ExpectedSalaryMax = input.ExpectedSalaryMax
	}
	if len(input.WorkExperiences) > 0 {
		target.WorkExperiences = input.WorkExperiences
	}
	if len(input.Educations) > 0 {
		target.Educations = input.Educations
	}
	if len(input.Certificates) > 0 {
		target.Certificates = input.Certificates
	}
	if len(input.Honors) > 0 {
		target.Honors = input.Honors
	}
	if len(input.ProjectExperiences) > 0 {
		target.ProjectExperiences = input.ProjectExperiences
	}
	if len(input.ColleagueCommunications) > 0 {
		target.Communications = input.ColleagueCommunications
	}
}

// saveLocalAutoReplyAvatar 只更新一位候选人的本地头像，不产生公开地址。
func (c *Client) saveLocalAutoReplyAvatar(ctx context.Context, credentials AgentCredentials, id string, imageData []byte) (result AutoReplyAvatarUploadResult, err error) {
	mime := http.DetectContentType(imageData)
	if mime != "image/png" && mime != "image/jpeg" {
		return result, fmt.Errorf("候选人头像需要PNG或JPEG图片")
	}
	err = c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		candidate, ok, err := getAutoReplyEntity[AutoReplyStoredCandidate](tx, "candidates", id)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("本地候选人还没保存")
		}
		candidate.AvatarURL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(imageData)
		result.AvatarURL = candidate.AvatarURL
		return tx.Put("candidates", id, candidate)
	})
	return
}

// structuredLocalAutoReplyCandidate 从当前已提交的本地资料生成同步快照，防止旧后台调用覆盖新资料。
func structuredLocalAutoReplyCandidate(candidate AutoReplyStoredCandidate) StructuredCandidate {
	return StructuredCandidate{CandidateName: candidate.CandidateName, BirthYM: candidate.BirthYM, Phone: candidate.Phone, Email: candidate.Email, Wechat: candidate.Wechat, WorkRegion: candidate.WorkRegion, WorkYears: candidate.WorkYears, ExpectedSalaryMin: candidate.ExpectedSalaryMin, ExpectedSalaryMax: candidate.ExpectedSalaryMax, EducationLevel: candidate.EducationLevel, ExpectedPosition: candidate.ExpectedPosition, OnlineStatus: candidate.OnlineStatus, PersonalDescription: candidate.PersonalDescription, WorkStatus: candidate.WorkStatus, RawText: candidate.RawText, WorkExperiences: candidate.WorkExperiences, Educations: candidate.Educations, Certificates: candidate.Certificates, Honors: candidate.Honors, ProjectExperiences: candidate.ProjectExperiences, ColleagueCommunications: candidate.Communications}
}
