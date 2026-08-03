// 本文件负责自动回复数据进入云端存储前的标准化和边界校验。
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var autoReplyPhoneDigits = regexp.MustCompile(`[^0-9]`)

// normalizeCandidatePhone 返回只保留数字的团队内候选人手机号身份。
func normalizeCandidatePhone(value string) string {
	return autoReplyPhoneDigits.ReplaceAllString(strings.TrimSpace(value), "")
}

// normalizeAutoReplyDedupeKey 根据自然语言生成稳定、不可反推原文的去重键。
func normalizeAutoReplyDedupeKey(value string) string {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// validateCompanyProfile 校验团队公司档案必填项和长度。
func validateCompanyProfile(item CompanyProfile) error {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		return errors.New("公司档案名称不能为空")
	}
	if len([]rune(name)) > 100 {
		return errors.New("公司档案名称不能超过100字")
	}
	for label, value := range map[string]string{
		"公司地址": item.Address, "联系方式": item.Contact,
		"公司概况": item.Overview, "其他公司信息": item.ExtraInfo,
	} {
		if len([]rune(value)) > 10000 {
			return fmt.Errorf("%s不能超过10000字", label)
		}
	}
	return nil
}

// validatePositionAutoReplyConfig 校验岗位自动回复配置和条件。
func validatePositionAutoReplyConfig(item PositionAutoReplyConfig) error {
	if strings.TrimSpace(item.PositionID) == "" || strings.TrimSpace(item.TenantID) == "" {
		return errors.New("岗位和团队不能为空")
	}
	if item.Enabled && strings.TrimSpace(item.CompanyProfileID) == "" {
		return errors.New("开启自动回复前需要选择公司档案")
	}
	if len([]rune(item.PositionDescription)) > 20000 {
		return errors.New("自动回复岗位描述不能超过20000字")
	}
	if strings.TrimSpace(item.ResumeRequestMessage) == "" {
		return errors.New("索要简历话术不能为空")
	}
	if len([]rune(item.ResumeRequestMessage)) > 500 {
		return errors.New("索要简历话术不能超过500字")
	}
	if item.PollIntervalSeconds < 1 || item.PollIntervalSeconds > 300 {
		return errors.New("自动回复检查间隔需要在1到300秒之间")
	}
	if item.MaxThreadsPerCheckpoint < 1 || item.MaxThreadsPerCheckpoint > 20 {
		return errors.New("单次处理会话数需要在1到20之间")
	}
	seen := make(map[string]struct{}, len(item.Conditions))
	for _, condition := range item.Conditions {
		if err := validatePositionReplyCondition(condition); err != nil {
			return err
		}
		key := normalizeAutoReplyDedupeKey(condition.Content)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("岗位条件重复：%s", strings.TrimSpace(condition.Content))
		}
		seen[key] = struct{}{}
	}
	return nil
}

// validatePositionReplyCondition 校验单条岗位自动回复条件。
func validatePositionReplyCondition(item PositionReplyCondition) error {
	if item.Type != "required" && item.Type != "confirm" && item.Type != "bonus" {
		return errors.New("岗位条件类型只支持必须满足、需要确认或加分项")
	}
	content := strings.TrimSpace(item.Content)
	if content == "" {
		return errors.New("岗位条件内容不能为空")
	}
	if len([]rune(content)) > 1000 {
		return errors.New("单条岗位条件不能超过1000字")
	}
	if item.SortOrder < 0 {
		return errors.New("岗位条件排序不能小于0")
	}
	return nil
}

// validateAutoReplyMessage 校验一条待同步的聊天消息。
func validateAutoReplyMessage(item AutoReplyMessage) error {
	if item.Direction != "candidate" && item.Direction != "self" && item.Direction != "system" {
		return errors.New("聊天消息方向不支持")
	}
	if strings.TrimSpace(item.Fingerprint) == "" {
		return errors.New("聊天消息缺少稳定指纹")
	}
	if len([]rune(item.Fingerprint)) > 256 {
		return errors.New("聊天消息指纹过长")
	}
	if strings.TrimSpace(item.MessageType) == "" {
		return errors.New("聊天消息类型不能为空")
	}
	if len([]rune(item.TextContent)) > 100000 {
		return errors.New("单条聊天消息正文过长")
	}
	return validateJSONDocument(item.CardContent, true)
}

// validateResumeAttachment 校验简历附件元数据和允许的文件类型。
func validateResumeAttachment(item StoredResumeAttachment) error {
	if strings.TrimSpace(item.CandidateID) == "" && strings.TrimSpace(item.ConversationID) == "" {
		return errors.New("简历附件需要关联候选人或临时会话")
	}
	if item.SizeBytes < 0 || item.SizeBytes > AutoReplyMaxAttachmentBytes {
		return errors.New("简历附件不能超过20MB")
	}
	if strings.TrimSpace(item.SHA256) == "" || len(item.SHA256) != 64 {
		return errors.New("简历附件缺少有效SHA256")
	}
	if _, err := hex.DecodeString(item.SHA256); err != nil {
		return errors.New("简历附件SHA256格式不正确")
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(item.OriginalName)))
	if ext != ".pdf" && ext != ".doc" && ext != ".docx" && ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
		return errors.New("简历附件只支持PDF、DOC、DOCX、JPG和PNG")
	}
	cleanPath := filepath.Clean(strings.TrimSpace(item.StoragePath))
	if cleanPath == "." || filepath.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return errors.New("简历附件存储路径不安全")
	}
	return nil
}

// validateConfirmationItem 校验候选人确认项。
func validateConfirmationItem(item CandidateConfirmationItem) error {
	if strings.TrimSpace(item.ConversationID) == "" || strings.TrimSpace(item.Content) == "" {
		return errors.New("候选人确认项缺少会话或内容")
	}
	if item.ItemType != "required" && item.ItemType != "confirm" && item.ItemType != "bonus" {
		return errors.New("候选人确认项类型不支持")
	}
	if item.Status != "pending" && item.Status != "matched" && item.Status != "unmatched" && item.Status != "not_applicable" && item.Status != "conflicted" {
		return errors.New("候选人确认项状态不支持")
	}
	if item.SourceType != "position" && item.SourceType != "resume" && item.SourceType != "chat" && item.SourceType != "ai" {
		return errors.New("候选人确认项来源不支持")
	}
	if item.CreatedByKind != "system" && item.CreatedByKind != "ai" && item.CreatedByKind != "user" {
		return errors.New("候选人确认项创建来源不支持")
	}
	return nil
}

// validateJSONDocument 校验审计或卡片字段是合法JSON对象或数组。
func validateJSONDocument(value json.RawMessage, allowEmpty bool) error {
	trimmed := strings.TrimSpace(string(value))
	if trimmed == "" && allowEmpty {
		return nil
	}
	if !json.Valid(value) {
		return errors.New("JSON数据格式不正确")
	}
	if trimmed == "null" {
		return errors.New("JSON数据不能是null")
	}
	return nil
}
