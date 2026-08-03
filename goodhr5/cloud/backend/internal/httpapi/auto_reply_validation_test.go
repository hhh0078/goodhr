// 本文件负责验证自动回复手机号、条件、消息和简历附件的存储边界。
package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestNormalizeCandidatePhoneKeepsInternationalDigits 验证国际号码和分隔符会稳定转换为纯数字身份。
func TestNormalizeCandidatePhoneKeepsInternationalDigits(t *testing.T) {
	got := normalizeCandidatePhone("+86 (176) 0708-0935")
	if got != "8617607080935" {
		t.Fatalf("normalizeCandidatePhone() = %q", got)
	}
}

// TestValidatePositionAutoReplyConfigRequiresCompany 验证开启自动回复时必须选择团队公司档案。
func TestValidatePositionAutoReplyConfigRequiresCompany(t *testing.T) {
	item := PositionAutoReplyConfig{PositionID: "position-1", TenantID: "tenant-1", Enabled: true}
	applyAutoReplyConfigDefaults(&item)
	if err := validatePositionAutoReplyConfig(item); err == nil || !strings.Contains(err.Error(), "公司档案") {
		t.Fatalf("validatePositionAutoReplyConfig() err = %v", err)
	}
}

// TestValidatePositionAutoReplyConfigRejectsDuplicateConditions 验证忽略空格和标点后相同的条件不会重复保存。
func TestValidatePositionAutoReplyConfigRejectsDuplicateConditions(t *testing.T) {
	item := PositionAutoReplyConfig{
		PositionID: "position-1", TenantID: "tenant-1", CompanyProfileID: "company-1", Enabled: true,
		ResumeRequestMessage: AutoReplyDefaultResumeRequestMessage, PollIntervalSeconds: 5, MaxThreadsPerCheckpoint: 3,
		Conditions: []PositionReplyCondition{
			{Type: "required", Content: "必须统招本科", Enabled: true},
			{Type: "confirm", Content: "必须 统招本科。", Enabled: true},
		},
	}
	if err := validatePositionAutoReplyConfig(item); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("validatePositionAutoReplyConfig() err = %v", err)
	}
}

// TestValidateAutoReplyMessageRequiresFingerprint 验证没有平台消息ID时仍必须提供稳定指纹。
func TestValidateAutoReplyMessageRequiresFingerprint(t *testing.T) {
	err := validateAutoReplyMessage(AutoReplyMessage{Direction: "candidate", MessageType: "text", CardContent: json.RawMessage(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "指纹") {
		t.Fatalf("validateAutoReplyMessage() err = %v", err)
	}
}

// TestValidateResumeAttachmentBoundaries 验证简历附件类型、大小、哈希和相对路径边界。
func TestValidateResumeAttachmentBoundaries(t *testing.T) {
	valid := StoredResumeAttachment{
		TenantID: "tenant-1", ConversationID: "conversation-1", OriginalName: "张三..简历.pdf",
		StoragePath: "resumes/tenant-1/file.pdf", SHA256: strings.Repeat("a", 64), SizeBytes: AutoReplyMaxAttachmentBytes,
	}
	if err := validateResumeAttachment(valid); err != nil {
		t.Fatalf("validateResumeAttachment(valid) err = %v", err)
	}
	invalidPath := valid
	invalidPath.StoragePath = "../outside.pdf"
	if err := validateResumeAttachment(invalidPath); err == nil {
		t.Fatal("validateResumeAttachment() accepted parent path")
	}
	invalidType := valid
	invalidType.OriginalName = "resume.exe"
	if err := validateResumeAttachment(invalidType); err == nil {
		t.Fatal("validateResumeAttachment() accepted executable")
	}
	invalidHash := valid
	invalidHash.SHA256 = strings.Repeat("z", 64)
	if err := validateResumeAttachment(invalidHash); err == nil {
		t.Fatal("validateResumeAttachment() accepted invalid hash")
	}
}
