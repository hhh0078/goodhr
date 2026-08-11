// Package cloud 验证候选人头像 Base64 优先上传和 multipart 后备协议。
package cloud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUploadAutoReplyAvatarBase64SendsAuthenticatedJSON 验证 Base64 头像请求携带设备鉴权和候选人编号。
func TestUploadAutoReplyAvatarBase64SendsAuthenticatedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/auto-reply/agent/avatars" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GoodHR-Machine-ID") != "goodhr-device-v1-test" {
			t.Fatalf("头像上传鉴权头不完整")
		}
		var payload struct {
			CandidateID string `json:"candidate_id"`
			ImageBase64 string `json:"image_base64"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.CandidateID != "candidate-1" || payload.ImageBase64 != "AQID" {
			t.Fatalf("头像 Base64 请求不正确：%+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "avatar_url": "/api/public/candidate-avatars/" + strings.Repeat("a", 64) + ".png",
		})
	}))
	defer server.Close()

	result, err := New(server.URL).UploadAutoReplyAvatarBase64(context.Background(), testAutoReplyCredentials, "candidate-1", []byte{1, 2, 3})
	if err != nil {
		t.Fatalf("Base64 头像上传失败：%v", err)
	}
	if result.AvatarURL == "" {
		t.Fatal("云端头像地址为空")
	}
}

// TestUploadAutoReplyAvatarFileSendsMultipartFallback 验证文件后备上传仍复用设备鉴权和1MB限制。
func TestUploadAutoReplyAvatarFileSendsMultipartFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(path, []byte("png-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-GoodHR-Machine-ID") != testAutoReplyCredentials.MachineID {
			t.Fatalf("machine header = %q", r.Header.Get("X-GoodHR-Machine-ID"))
		}
		if err := r.ParseMultipartForm(AutoReplyMaxAvatarBytes + 1<<20); err != nil {
			t.Fatal(err)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "png-data" || r.FormValue("candidate_id") != "candidate-1" {
			t.Fatalf("multipart 头像请求不完整")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "avatar_url": "/api/public/candidate-avatars/avatar.png"})
	}))
	defer server.Close()

	if _, err := New(server.URL).UploadAutoReplyAvatarFile(context.Background(), testAutoReplyCredentials, "candidate-1", path); err != nil {
		t.Fatalf("multipart 头像后备上传失败：%v", err)
	}
}
