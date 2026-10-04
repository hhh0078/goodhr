// Package httpapi 本文件负责验证自动回复手机号、条件、消息和简历附件的存储边界。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNormalizeCandidatePhoneKeepsInternationalDigits 验证国际号码和分隔符会稳定转换为纯数字身份。
func TestNormalizeCandidatePhoneKeepsInternationalDigits(t *testing.T) {
	got := normalizeCandidatePhone("+86 (176) 0708-0935")
	if got != "17607080935" {
		t.Fatalf("normalizeCandidatePhone() = %q", got)
	}
	if international := normalizeCandidatePhone("+1 (202) 555-0123"); international != "12025550123" {
		t.Fatalf("normalizeCandidatePhone(international) = %q", international)
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
	tooLarge := valid
	tooLarge.SizeBytes = AutoReplyMaxAttachmentBytes + 1
	if err := validateResumeAttachment(tooLarge); err == nil || !strings.Contains(err.Error(), "20MB") {
		t.Fatalf("validateResumeAttachment(tooLarge) err = %v", err)
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

// TestValidateAutoReplyCandidateBirthYM 验证精确月份和估算年份不会互相混用或编造月份。
func TestValidateAutoReplyCandidateBirthYM(t *testing.T) {
	tests := []struct {
		name      string
		birthYM   string
		precision string
		wantError bool
	}{
		{name: "精确月份", birthYM: "1995-08", precision: "month"},
		{name: "估算年份", birthYM: "1995", precision: "year_estimated"},
		{name: "估算年份不能带月份", birthYM: "1995-08", precision: "year_estimated", wantError: true},
		{name: "月份不能为零", birthYM: "1995-00", precision: "month", wantError: true},
		{name: "月份不能超过十二", birthYM: "1995-13", precision: "month", wantError: true},
		{name: "出生时间必须声明精度", birthYM: "1995", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAutoReplyCandidateRequest(autoReplyCandidateRequest{
				PositionID: "position-1", PlatformID: "liepin", Phone: "17607080935",
				BirthYM: test.birthYM, BirthYMPrecision: test.precision,
			})
			if (err != nil) != test.wantError {
				t.Fatalf("validateAutoReplyCandidateRequest() err = %v, wantError=%t", err, test.wantError)
			}
		})
	}
}

// TestDetectResumeMIMERejectsDisguisedFiles 验证只改扩展名的伪装附件不会进入简历目录。
func TestDetectResumeMIMERejectsDisguisedFiles(t *testing.T) {
	if _, valid := detectResumeMIME(".pdf", []byte("这不是PDF")); valid {
		t.Fatal("detectResumeMIME() accepted a disguised PDF")
	}
	if _, valid := detectResumeMIME(".png", []byte("PK\x03\x04")); valid {
		t.Fatal("detectResumeMIME() accepted a disguised PNG")
	}
	if mimeType, valid := detectResumeMIME(".pdf", []byte("%PDF-1.4")); !valid || mimeType != "application/pdf" {
		t.Fatalf("detectResumeMIME() mime=%q valid=%t", mimeType, valid)
	}
}

// TestWriteAutoReplyStoreErrorClassifiesSafeAndInternalErrors 验证参数错误可读，数据库错误只返回追踪编号。
func TestWriteAutoReplyStoreErrorClassifiesSafeAndInternalErrors(t *testing.T) {
	validationResponse := httptest.NewRecorder()
	writeAutoReplyStoreError(validationResponse, newAutoReplyValidationError("岗位条件不能为空"), "保存失败")
	if validationResponse.Code != http.StatusBadRequest || !strings.Contains(validationResponse.Body.String(), "岗位条件不能为空") {
		t.Fatalf("validation status=%d body=%s", validationResponse.Code, validationResponse.Body.String())
	}

	internalResponse := httptest.NewRecorder()
	writeAutoReplyStoreError(internalResponse, errors.New("pq: password=secret database unavailable"), "数据暂时没保存成功")
	body := internalResponse.Body.String()
	if internalResponse.Code != http.StatusInternalServerError || strings.Contains(body, "password=secret") || !strings.Contains(body, "error_id") {
		t.Fatalf("internal status=%d body=%s", internalResponse.Code, body)
	}
}
