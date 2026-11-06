// Package auto_reply 验证候选人头像截图上传和外部地址安全边界。
package auto_reply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/flow/shared"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

type candidateAvatarBrowserStub struct {
	autoReplyBrowserStub
	imageData []byte
	target    *contract.SelectorSpec
	request   contract.ScreenshotRequest
}

// FindAll 模拟最后一条候选人头像的真实序号，避免默认取历史第一条。
func (b *candidateAvatarBrowserStub) FindAll(_ context.Context, request contract.ElementFindAllRequest) ([]contract.FindAllItem, error) {
	if !request.FromEnd || request.MaxItems != 1 || !request.ProbeOnly {
		return nil, fmt.Errorf("头像未使用标准尾窗")
	}
	return []contract.FindAllItem{{Index: 88, TotalMatches: 89}}, nil
}

// Screenshot 保存测试头像并记录公共流程传入的平台选择器。
func (b *candidateAvatarBrowserStub) Screenshot(_ context.Context, request contract.ScreenshotRequest) (contract.ScreenshotResult, error) {
	b.target = request.Target
	b.request = request
	path := filepath.Join(request.Directory, request.Filename)
	if err := os.WriteFile(path, b.imageData, 0o600); err != nil {
		return contract.ScreenshotResult{}, err
	}
	return contract.ScreenshotResult{Path: path, Filename: request.Filename, Size: int64(len(b.imageData))}, nil
}

// TestSyncCandidateAvatarUsesConfiguredElementScreenshot 验证头像优先使用当前会话元素截图和 Base64 上传。
func TestSyncCandidateAvatarUsesConfiguredElementScreenshot(t *testing.T) {
	imageData := testCandidateAvatarPNG(t, 16, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auto-reply/agent/avatars" || r.Header.Get("X-GoodHR-Machine-ID") != "goodhr-device-v1-test" {
			t.Fatalf("头像云端请求不正确：%s", r.URL.Path)
		}
		var payload struct {
			CandidateID string `json:"candidate_id"`
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.CandidateID != "candidate-1" || payload.ImageBase64 == "" {
			t.Fatalf("头像请求不完整：%+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "avatar_url": "/api/public/candidate-avatars/avatar.png"})
	}))
	defer server.Close()

	browser := &candidateAvatarBrowserStub{imageData: imageData}
	flow := &Flow{Browser: browser, Cloud: cloud.New(server.URL)}
	prepared := shared.PreparedTask{
		MachineID: "goodhr-device-v1-test",
		Request:   shared.StartRequest{Token: "test-token"},
		Platform: model.Config{ID: "liepin", Name: "猎聘企业", Behavior: model.Behavior{AutoReplyAvatarContainerKey: "message.history_scroll"}, Selectors: map[string]contract.SelectorSpec{
			currentAutoReplyAvatarSelectorKey: {Description: "当前候选人头像"},
			"message.history_scroll":          {Description: "聊天可见区"},
		}},
	}
	runtime := &autoReplyRuntimeStub{}
	if err := flow.syncCandidateAvatar(context.Background(), prepared, model.AutoReplyConversationSnapshot{}, "candidate-1", runtime); err != nil {
		t.Fatalf("候选人头像同步失败：%v", err)
	}
	if browser.target == nil || browser.target.Description != "当前候选人头像" {
		t.Fatalf("没有使用平台当前头像选择器：%+v", browser.target)
	}
	if !browser.request.ViewportOnly || browser.request.ClipContainer == nil || browser.request.ClipContainer.Description != "聊天可见区" || browser.target.Target.Index == nil || *browser.target.Target.Index != 88 || runtime.openReadCount != 2 {
		t.Fatalf("头像没有使用末条、不滚动截图和前后身份复核：%+v reads=%d", browser.request, runtime.openReadCount)
	}
}

// TestAvatarUnknownConversationDoesNotCapture 验证无法证明当前聊天归属时不截图，并继续执行已有HTTPS地址安全校验。
func TestAvatarUnknownConversationDoesNotCapture(t *testing.T) {
	browser := &candidateAvatarBrowserStub{}
	f := &Flow{Browser: browser}
	err := f.syncCandidateAvatar(context.Background(), shared.PreparedTask{}, model.AutoReplyConversationSnapshot{AvatarURL: "http://127.0.0.1/avatar.png"}, "candidate", &autoReplyRuntimeStub{openChatClosed: true})
	if err == nil || browser.target != nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("身份不明仍截图或绕过下载保护：target=%+v err=%v", browser.target, err)
	}
}

// TestUploadCandidateAvatarDoesNotFallbackWhenResponseIsUnknown 验证 Base64 可能已成功但响应损坏时不会再上传第二份文件。
func TestUploadCandidateAvatarDoesNotFallbackWhenResponseIsUnknown(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":`))
	}))
	defer server.Close()
	imageData := testCandidateAvatarPNG(t, 16, 16)
	filePath := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(filePath, imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	flow := &Flow{Cloud: cloud.New(server.URL)}
	err := flow.uploadCandidateAvatar(context.Background(), cloud.AgentCredentials{
		Token: "token", MachineID: "machine",
	}, "candidate-1", imageData, filePath)
	if err == nil || requests != 1 {
		t.Fatalf("响应未知时不应执行 multipart 后备：requests=%d err=%v", requests, err)
	}
}

// TestUploadCandidateAvatarFallsBackAfterExplicitClientError 验证云端明确拒绝 Base64 协议时仍可使用 multipart 后备。
func TestUploadCandidateAvatarFallsBackAfterExplicitClientError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code": "AVATAR_CONTENT_TYPE_INVALID", "message": "请改用文件上传",
			}})
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("后备请求不是 multipart：%s", r.Header.Get("Content-Type"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "avatar_url": "/api/public/candidate-avatars/avatar.png"})
	}))
	defer server.Close()
	imageData := testCandidateAvatarPNG(t, 16, 16)
	filePath := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(filePath, imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	flow := &Flow{Cloud: cloud.New(server.URL)}
	if err := flow.uploadCandidateAvatar(context.Background(), cloud.AgentCredentials{
		Token: "token", MachineID: "machine",
	}, "candidate-1", imageData, filePath); err != nil {
		t.Fatalf("明确协议错误后 multipart 后备失败：%v", err)
	}
	if requests != 2 {
		t.Fatalf("后备上传请求次数不正确：%d", requests)
	}
}

// TestCandidateAvatarSecurityRejectsPrivateAndReservedIPs 验证头像安全下载拒绝本机、私网和文档保留网段。
func TestCandidateAvatarSecurityRejectsPrivateAndReservedIPs(t *testing.T) {
	for _, rawURL := range []string{"http://example.com/avatar.png", "https://localhost/avatar.png", "https://127.0.0.1/avatar.png", "https://10.0.0.1/avatar.png"} {
		if _, err := parseSafeCandidateAvatarURL(rawURL); err == nil {
			t.Fatalf("不安全头像地址没有被拒绝：%s", rawURL)
		}
	}
	if !isPublicCandidateAvatarIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("公开 IP 被错误拒绝")
	}
	if isPublicCandidateAvatarIP(net.ParseIP("203.0.113.1")) {
		t.Fatal("文档保留 IP 没有被拒绝")
	}
}

// TestIsSelfHostedCandidateAvatarRequiresStrictToken 验证只有64位小写随机 token 才会跳过重复头像上传。
func TestIsSelfHostedCandidateAvatarRequiresStrictToken(t *testing.T) {
	valid := "/api/public/candidate-avatars/" + strings.Repeat("a", 64) + ".png"
	if !isSelfHostedCandidateAvatar(valid) {
		t.Fatal("合法自托管头像没有被识别")
	}
	for _, invalid := range []string{
		"/api/public/candidate-avatars/short.png",
		"/api/public/candidate-avatars/" + strings.Repeat("A", 64) + ".png",
		"https://image.example.com/avatar.png",
	} {
		if isSelfHostedCandidateAvatar(invalid) {
			t.Fatalf("不合法头像被误认为已自托管：%s", invalid)
		}
	}
}

// TestValidateDownloadedCandidateAvatarChecksRealImage 验证下载后只接受真实且尺寸合理的 PNG/JPEG。
func TestValidateDownloadedCandidateAvatarChecksRealImage(t *testing.T) {
	if err := validateDownloadedCandidateAvatar(testCandidateAvatarPNG(t, 32, 32)); err != nil {
		t.Fatalf("有效头像被拒绝：%v", err)
	}
	if err := validateDownloadedCandidateAvatar([]byte("not-image")); err == nil {
		t.Fatal("伪造图片没有被拒绝")
	}
}

// testCandidateAvatarPNG 生成指定尺寸的无元数据 PNG 测试图片。
func testCandidateAvatarPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.RGBA{R: 20, G: 120, B: 80, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, picture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
