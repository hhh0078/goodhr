// Package cloud 验证自动回复云端客户端的强类型协议、边界限制和隐私摘要。
package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testAutoReplyCredentials = AgentCredentials{Token: "test-token", MachineID: "goodhr-device-v1-test"}

// TestAutoReplySnapshotSendsDeviceAndReadsStrongTypes 验证岗位快照携带设备请求头并完整解析强类型字段。
func TestAutoReplySnapshotSendsDeviceAndReadsStrongTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/auto-reply/agent/positions/position-1/snapshot" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GoodHR-Machine-ID") != "goodhr-device-v1-test" {
			t.Fatalf("unexpected headers: authorization=%q machine=%q", r.Header.Get("Authorization"), r.Header.Get("X-GoodHR-Machine-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ok":true,
			"position":{"id":"position-1","name":"高中数学老师","platform_id":"liepin","status":"running"},
			"config":{"position_id":"position-1","company_profile_id":"company-1","enabled":true,"position_description":"岗位说明","resume_request_message":"你好，能发一份简历吗？","poll_interval_seconds":3,"max_threads_per_checkpoint":3,"version":2,"conditions":[{"id":"condition-1","type":"required","content":"统招本科","dedupe_key":"本科","sort_order":0,"enabled":true}],"updated_at":"2026-08-04T00:00:00Z"},
			"company_profile":{"id":"company-1","name":"GoodHR","address":"德阳","contact":"HR","overview":"公司概况","extra_info":"","updated_at":"2026-08-04T00:00:00Z"},
			"subscription":{"active":true,"member_type":"max","member_name":"Max 全能版","expires_at":"2027-08-04T00:00:00Z","remaining_days":365,"remaining_seconds":31536000,"allow_ai":true,"allow_auto_reply":true,"features":["自动回复"]}
		}`))
	}))
	defer server.Close()

	snapshot, err := New(server.URL).AutoReplySnapshot(context.Background(), testAutoReplyCredentials, "position-1")
	if err != nil {
		t.Fatalf("读取自动回复快照失败：%v", err)
	}
	if snapshot.Position.Name != "高中数学老师" || snapshot.CompanyProfile.ID != "company-1" || len(snapshot.Config.Conditions) != 1 {
		t.Fatalf("自动回复快照解析不完整：%+v", snapshot)
	}
	if !snapshot.Subscription.AllowAutoReply || snapshot.Config.MaxThreadsPerCheckpoint != 3 {
		t.Fatalf("自动回复权限或单轮上限解析不正确：%+v", snapshot)
	}
}

// TestAutoReplySnapshotsReadsAllEnabledPositions 验证同一平台多个已开启岗位会一起下发给本地主流程。
func TestAutoReplySnapshotsReadsAllEnabledPositions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/positions" || r.URL.Query().Get("platform_id") != "liepin" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"positions":[
			{"ok":true,"position":{"id":"position-1","name":"高中数学老师","platform_id":"liepin"},"config":{"position_id":"position-1","company_profile_id":"company-1","enabled":true},"company_profile":{"id":"company-1"},"subscription":{"active":true,"allow_auto_reply":true}},
			{"ok":true,"position":{"id":"position-2","name":"AI应用开发工程师","platform_id":"liepin"},"config":{"position_id":"position-2","company_profile_id":"company-1","enabled":true},"company_profile":{"id":"company-1"},"subscription":{"active":true,"allow_auto_reply":true}}
		]}`))
	}))
	defer server.Close()

	items, err := New(server.URL).AutoReplySnapshots(context.Background(), testAutoReplyCredentials, "liepin")
	if err != nil {
		t.Fatalf("读取多岗位快照失败：%v", err)
	}
	if len(items) != 2 || items[0].Position.Name != "高中数学老师" || items[1].Position.Name != "AI应用开发工程师" {
		t.Fatalf("多岗位快照解析不正确：%+v", items)
	}
}

// TestAutoReplyClientKeepsStructuredErrorID 验证云端内部错误编号会进入稳定 APIError。
func TestAutoReplyClientKeepsStructuredErrorID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"AUTO_REPLY_STORAGE_FAILED","message":"聊天记录暂时没读出来","error_id":"error-123"}}`))
	}))
	defer server.Close()

	_, err := New(server.URL).AutoReplyMessages(context.Background(), testAutoReplyCredentials, "conversation-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want APIError", err)
	}
	if apiErr.Code != "AUTO_REPLY_STORAGE_FAILED" || apiErr.ErrorID != "error-123" || !strings.Contains(apiErr.Error(), "错误编号：error-123") {
		t.Fatalf("unexpected api error: %+v / %s", apiErr, apiErr.Error())
	}
}

// TestSyncAutoReplyMessagesRejectsOver5000BeforeRequest 验证首次聊天超过5000条会在本地直接拦截。
func TestSyncAutoReplyMessagesRejectsOver5000BeforeRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	messages := make([]AutoReplyMessage, AutoReplyMaxHistoryMessages+1)
	_, err := New(server.URL).SyncAutoReplyMessages(context.Background(), testAutoReplyCredentials, AutoReplyMessageSyncRequest{
		ConversationID: "conversation-1",
		Messages:       messages,
	})
	if err == nil || !strings.Contains(err.Error(), "5000") {
		t.Fatalf("超过5000条消息没有被拦截：%v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("本地拦截后不应请求云端，实际请求 %d 次", requests.Load())
	}
}

// TestAutoReplyClientHonorsCancellationAndTimeout 验证自动回复请求会响应主动取消和截止时间。
func TestAutoReplyClientHonorsCancellationAndTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	t.Run("主动取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := New(server.URL).AutoReplyMessages(ctx, testAutoReplyCredentials, "conversation-1")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("主动取消没有传递到调用方：%v", err)
		}
	})

	t.Run("截止时间", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := New(server.URL).AutoReplyMessages(ctx, testAutoReplyCredentials, "conversation-1")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("请求超时没有传递到调用方：%v", err)
		}
	})
}

// TestUploadAutoReplyAttachmentAccepts20MBAndChecksHash 验证恰好20MB可以上传且云端哈希必须一致。
func TestUploadAutoReplyAttachmentAccepts20MBAndChecksHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.pdf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte("%PDF-")); err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(AutoReplyMaxAttachmentBytes); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GoodHR-Machine-ID") != testAutoReplyCredentials.MachineID {
			t.Fatalf("machine header = %q", r.Header.Get("X-GoodHR-Machine-ID"))
		}
		if err := r.ParseMultipartForm(AutoReplyMaxAttachmentBytes + 1<<20); err != nil {
			t.Fatalf("解析 multipart 失败：%v", err)
		}
		uploaded, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer uploaded.Close()
		hash := sha256.New()
		size, err := io.Copy(hash, uploaded)
		if err != nil {
			t.Fatal(err)
		}
		if size != AutoReplyMaxAttachmentBytes || r.FormValue("conversation_id") != "conversation-1" {
			t.Fatalf("upload size=%d conversation=%q", size, r.FormValue("conversation_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"attachment": map[string]any{
				"id": "attachment-1", "conversation_id": "conversation-1", "original_name": "resume.pdf",
				"sha256": hex.EncodeToString(hash.Sum(nil)), "mime_type": "application/pdf", "size_bytes": size,
				"created_at": "2026-08-04T00:00:00Z",
			},
		})
	}))
	defer server.Close()

	attachment, err := New(server.URL).UploadAutoReplyAttachment(context.Background(), testAutoReplyCredentials, AutoReplyAttachmentUpload{
		FilePath: path, ConversationID: "conversation-1", PlatformID: "liepin",
	})
	if err != nil {
		t.Fatalf("上传20MB附件失败：%v", err)
	}
	if attachment.ID != "attachment-1" || attachment.SizeBytes != AutoReplyMaxAttachmentBytes {
		t.Fatalf("attachment = %+v", attachment)
	}
}

// TestUploadAutoReplyAttachmentRejectsTooLargeAndHashMismatch 验证超限文件不请求云端且哈希不一致会报错。
func TestUploadAutoReplyAttachmentRejectsTooLargeAndHashMismatch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		size, _ := io.Copy(io.Discard, file)
		_ = file.Close()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"attachment": map[string]any{"id": "attachment-1", "sha256": strings.Repeat("0", 64), "size_bytes": size, "created_at": "2026-08-04T00:00:00Z"},
		})
	}))
	defer server.Close()

	tooLargePath := filepath.Join(t.TempDir(), "too-large.pdf")
	tooLarge, err := os.Create(tooLargePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = tooLarge.Truncate(AutoReplyMaxAttachmentBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = tooLarge.Close()
	_, err = New(server.URL).UploadAutoReplyAttachment(context.Background(), testAutoReplyCredentials, AutoReplyAttachmentUpload{
		FilePath: tooLargePath, ConversationID: "conversation-1",
	})
	if err == nil || !strings.Contains(err.Error(), "20MB") || requests.Load() != 0 {
		t.Fatalf("超限附件没有在本地拦截：err=%v requests=%d", err, requests.Load())
	}

	path := filepath.Join(t.TempDir(), "resume.pdf")
	if err := os.WriteFile(path, []byte("%PDF-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = New(server.URL).UploadAutoReplyAttachment(context.Background(), testAutoReplyCredentials, AutoReplyAttachmentUpload{
		FilePath: path, ConversationID: "conversation-1",
	})
	if err == nil || !strings.Contains(err.Error(), "校验没有通过") {
		t.Fatalf("云端哈希不一致没有被识别：%v", err)
	}
}

// TestAutoReplyAttachmentErrorDoesNotLeakPath 验证文件错误不会泄露本地完整路径。
func TestAutoReplyAttachmentErrorDoesNotLeakPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "candidate-17607080935.pdf")
	_, err := New("http://127.0.0.1").UploadAutoReplyAttachment(context.Background(), testAutoReplyCredentials, AutoReplyAttachmentUpload{
		FilePath: path, ConversationID: "conversation-1",
	})
	if err == nil {
		t.Fatal("不存在的附件应该返回错误")
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), "17607080935") {
		t.Fatalf("附件错误泄露了完整路径：%v", err)
	}
}

// TestAutoReplySafeSummaryDoesNotLeakSensitiveText 验证普通日志摘要不包含手机号、邮箱、聊天正文或附件路径。
func TestAutoReplySafeSummaryDoesNotLeakSensitiveText(t *testing.T) {
	input := AutoReplyPrivacyInput{
		ConversationID: "private-conversation-id", PlatformCandidateID: "platform-candidate-1",
		Phone: "17607080935", Email: "candidate@example.com", LatestMessage: "薪资是多少",
		AttachmentPath: "/private/resumes/17607080935.pdf", MessageCount: 12, AttachmentCount: 1,
	}
	text := AutoReplySafeSummary(input).String()
	for _, secret := range []string{input.ConversationID, input.PlatformCandidateID, input.Phone, input.Email, input.LatestMessage, input.AttachmentPath} {
		if strings.Contains(text, secret) {
			t.Fatalf("安全摘要泄露敏感信息 %q：%s", secret, text)
		}
	}
	if !strings.Contains(text, "消息数=12") || !strings.Contains(text, "附件数=1") {
		t.Fatalf("安全摘要缺少可用统计：%s", text)
	}
	if bytes.Contains([]byte(text), []byte("private")) {
		t.Fatalf("安全摘要包含原始会话标识：%s", text)
	}
}
