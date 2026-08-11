// Package httpapi 验证候选人头像的安全重编码、自托管保存和公开读取。
package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type publicRecommendationAvatarStore struct {
	*MemoryCandidateStore
	publiclyReferenced bool
}

// CandidateAvatarPubliclyReferenced 返回测试指定的旧公开推荐快照引用状态。
func (s *publicRecommendationAvatarStore) CandidateAvatarPubliclyReferenced(_ context.Context, _ string) (bool, error) {
	return s.publiclyReferenced, nil
}

// TestSaveCandidateAvatarImageReencodesAndServesOwnURL 验证头像保存后数据库只持有自有路径且公开接口安全返回 PNG。
func TestSaveCandidateAvatarImageReencodesAndServesOwnURL(t *testing.T) {
	store := NewMemoryCandidateStore()
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{CandidateID: "candidate-1", TenantID: "tenant-1", CandidateName: "邓云川"}); err != nil {
		t.Fatal(err)
	}
	service := &AutoReplyService{candidates: store, resumeDir: t.TempDir()}
	avatarURL, err := service.saveCandidateAvatarImage(context.Background(), "tenant-1", "candidate-1", candidateAvatarPNG(t, 24, 24))
	if err != nil {
		t.Fatalf("保存自托管头像失败：%v", err)
	}
	if !strings.HasPrefix(avatarURL, candidateAvatarPublicPrefix) || store.profiles["candidate-1"].AvatarURL != avatarURL {
		t.Fatalf("头像地址没有写入候选人：%q", avatarURL)
	}
	request := httptest.NewRequest(http.MethodGet, avatarURL, nil)
	response := httptest.NewRecorder()
	service.PublicCandidateAvatar(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("公开头像响应不正确：status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
	cacheControl := response.Header().Get("Cache-Control")
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || cacheControl != "no-store" || strings.Contains(cacheControl, "immutable") {
		t.Fatalf("公开头像安全缓存头不完整：%v", response.Header())
	}
}

// TestNormalizeCandidateAvatarImageRejectsInvalidTypeAndOversizedDimensions 验证伪造图片和超大尺寸图片都会被拒绝。
func TestNormalizeCandidateAvatarImageRejectsInvalidTypeAndOversizedDimensions(t *testing.T) {
	normalized, err := normalizeCandidateAvatarImage(candidateAvatarJPEG(t, 32, 32))
	if err != nil {
		t.Fatalf("有效 JPEG 头像被拒绝：%v", err)
	}
	if _, format, decodeErr := image.DecodeConfig(bytes.NewReader(normalized)); decodeErr != nil || format != "png" {
		t.Fatalf("JPEG 没有重新编码成 PNG：format=%q err=%v", format, decodeErr)
	}
	if _, err := normalizeCandidateAvatarImage([]byte("not-image")); err == nil {
		t.Fatal("伪造头像没有被拒绝")
	}
	if _, err := normalizeCandidateAvatarImage(candidateAvatarPNG(t, autoReplyMaxAvatarDimension+1, 1)); err == nil {
		t.Fatal("超大尺寸头像没有被拒绝")
	}
}

// TestReadAutoReplyAvatarUploadLimitsBase64JSON 验证 Base64 上传协议可以读取有效图片且请求体仍受统一2MB限制。
func TestReadAutoReplyAvatarUploadLimitsBase64JSON(t *testing.T) {
	imageData := candidateAvatarPNG(t, 16, 16)
	payload := `{"candidate_id":"candidate-1","image_base64":"` + base64.StdEncoding.EncodeToString(imageData) + `"}`
	request := httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/avatars", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	candidateID, received, err := readAutoReplyAvatarUpload(response, request)
	if err != nil || candidateID != "candidate-1" || !bytes.Equal(received, imageData) {
		t.Fatalf("有效 Base64 头像没读成功：candidate=%q bytes=%d err=%v", candidateID, len(received), err)
	}

	oversized := `{"candidate_id":"candidate-1","image_base64":"` + strings.Repeat("A", autoReplyJSONBodyLimit+1) + `"}`
	request = httptest.NewRequest(http.MethodPost, "/api/auto-reply/agent/avatars", strings.NewReader(oversized))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	if _, _, err = readAutoReplyAvatarUpload(response, request); err == nil {
		t.Fatal("超过统一 JSON 请求体限制的 Base64 头像没有被拒绝")
	}
}

// TestCandidateAvatarPathRejectsTraversal 验证公开头像路径不能使用目录穿越或非随机 token。
func TestCandidateAvatarPathRejectsTraversal(t *testing.T) {
	service := &AutoReplyService{resumeDir: t.TempDir()}
	for _, avatarURL := range []string{
		candidateAvatarPublicPrefix + "../secret.png",
		candidateAvatarPublicPrefix + "short.png",
		"https://image.example.com/avatar.png",
	} {
		if _, err := service.candidateAvatarPathFromURL(avatarURL); err == nil {
			t.Fatalf("不安全头像路径没有被拒绝：%s", avatarURL)
		}
	}
}

// TestSaveCandidateAvatarRemovesUnreferencedPreviousFile 验证换头像后会清理已经没有任何引用的旧自托管文件。
func TestSaveCandidateAvatarRemovesUnreferencedPreviousFile(t *testing.T) {
	resumeDir := t.TempDir()
	oldURL := candidateAvatarPublicPrefix + strings.Repeat("c", 64) + ".png"
	oldPath, err := candidateAvatarStoragePath(resumeDir, oldURL)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(oldPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(oldPath, candidateAvatarPNG(t, 12, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	memory := NewMemoryCandidateStore()
	if _, err = memory.SaveCandidateProfile(CandidateProfileInput{CandidateID: "candidate-history", TenantID: "tenant-1", CandidateName: "历史候选人", AvatarURL: oldURL}); err != nil {
		t.Fatal(err)
	}
	service := &AutoReplyService{candidates: memory, resumeDir: resumeDir}
	if _, err = service.saveCandidateAvatarImage(context.Background(), "tenant-1", "candidate-history", candidateAvatarPNG(t, 16, 16)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("没有引用的旧头像没有清理：%v", err)
	}
}

// TestSaveCandidateAvatarKeepsPublicRecommendationFile 验证公开推荐仍引用旧头像时继续保留文件。
func TestSaveCandidateAvatarKeepsPublicRecommendationFile(t *testing.T) {
	resumeDir := t.TempDir()
	oldURL := candidateAvatarPublicPrefix + strings.Repeat("c", 64) + ".png"
	oldPath, err := candidateAvatarStoragePath(resumeDir, oldURL)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(oldPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(oldPath, candidateAvatarPNG(t, 12, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	memory := NewMemoryCandidateStore()
	if _, err = memory.SaveCandidateProfile(CandidateProfileInput{CandidateID: "candidate-history", TenantID: "tenant-1", CandidateName: "历史候选人", AvatarURL: oldURL}); err != nil {
		t.Fatal(err)
	}
	store := &publicRecommendationAvatarStore{MemoryCandidateStore: memory, publiclyReferenced: true}
	service := &AutoReplyService{candidates: store, resumeDir: resumeDir}
	if _, err = service.saveCandidateAvatarImage(context.Background(), "tenant-1", "candidate-history", candidateAvatarPNG(t, 16, 16)); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(oldPath); err != nil {
		t.Fatalf("公开推荐仍引用的旧头像被删除：%v", err)
	}
}

// TestMemoryCandidateAvatarTenantIsolation 验证内存回退的头像更新不能跨团队或绕过团队校验。
func TestMemoryCandidateAvatarTenantIsolation(t *testing.T) {
	store := NewMemoryCandidateStore()
	oldURL := candidateAvatarPublicPrefix + strings.Repeat("a", 64) + ".png"
	newURL := candidateAvatarPublicPrefix + strings.Repeat("b", 64) + ".png"
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-tenant-a", TenantID: "tenant-a", CandidateName: "团队甲候选人", AvatarURL: oldURL,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-tenant-a", CandidateName: "团队甲候选人", AvatarURL: oldURL,
	}); err == nil {
		t.Fatal("缺少团队的候选人更新不应成功")
	}

	for _, tenantID := range []string{"tenant-b", ""} {
		if _, err := store.UpdateCandidateAvatar(context.Background(), tenantID, "candidate-tenant-a", newURL); !errors.Is(err, ErrNotFound) {
			t.Fatalf("团队 %q 不应能更新团队甲头像：%v", tenantID, err)
		}
	}
	if store.profiles["candidate-tenant-a"].AvatarURL != oldURL {
		t.Fatalf("失败更新不应修改原头像：%q", store.profiles["candidate-tenant-a"].AvatarURL)
	}
	if referenced, err := store.CandidateAvatarPubliclyReferenced(context.Background(), newURL); err != nil || referenced {
		t.Fatalf("失败更新不应产生新的公开头像引用：referenced=%v err=%v", referenced, err)
	}

	previous, err := store.UpdateCandidateAvatar(context.Background(), "tenant-a", "candidate-tenant-a", newURL)
	if err != nil || previous != oldURL {
		t.Fatalf("所属团队更新头像失败：previous=%q err=%v", previous, err)
	}
	if referenced, err := store.CandidateAvatarPubliclyReferenced(context.Background(), newURL); err != nil || !referenced {
		t.Fatalf("所属团队更新后的头像应继续支持全局公开引用检查：referenced=%v err=%v", referenced, err)
	}
}

// TestPublicCandidateAvatarServesOldPublicRecommendationSnapshot 验证旧公开推荐快照仍可读取其保留的历史头像。
func TestPublicCandidateAvatarServesOldPublicRecommendationSnapshot(t *testing.T) {
	resumeDir := t.TempDir()
	avatarURL := candidateAvatarPublicPrefix + strings.Repeat("f", 64) + ".png"
	path, err := candidateAvatarStoragePath(resumeDir, avatarURL)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, candidateAvatarPNG(t, 12, 12), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &publicRecommendationAvatarStore{MemoryCandidateStore: NewMemoryCandidateStore(), publiclyReferenced: true}
	service := &AutoReplyService{candidates: store, resumeDir: resumeDir}
	request := httptest.NewRequest(http.MethodGet, avatarURL, nil)
	response := httptest.NewRecorder()
	service.PublicCandidateAvatar(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("旧公开推荐头像无法读取：status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}

// TestCandidateAvatarAvailableDetectsMissingVolumeFile 验证数据库有路径但持久化文件丢失时会触发下轮补传。
func TestCandidateAvatarAvailableDetectsMissingVolumeFile(t *testing.T) {
	service := &AutoReplyService{resumeDir: t.TempDir()}
	avatarURL := candidateAvatarPublicPrefix + strings.Repeat("d", 64) + ".png"
	if service.candidateAvatarAvailable(avatarURL) {
		t.Fatal("不存在的头像文件不应被当成可用")
	}
	path, err := candidateAvatarStoragePath(service.resumeDir, avatarURL)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, candidateAvatarPNG(t, 8, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	if !service.candidateAvatarAvailable(avatarURL) {
		t.Fatal("已经落盘的自托管头像应被识别为可用")
	}
}

// TestPublicCandidateAvatarRejectsOrphanedFile 验证数据库删除后即使文件清理失败，旧公开地址也不能继续访问。
func TestPublicCandidateAvatarRejectsOrphanedFile(t *testing.T) {
	resumeDir := t.TempDir()
	avatarURL := candidateAvatarPublicPrefix + strings.Repeat("e", 64) + ".png"
	path, err := candidateAvatarStoragePath(resumeDir, avatarURL)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, candidateAvatarPNG(t, 8, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &AutoReplyService{candidates: NewMemoryCandidateStore(), resumeDir: resumeDir}
	request := httptest.NewRequest(http.MethodGet, avatarURL, nil)
	response := httptest.NewRecorder()
	service.PublicCandidateAvatar(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("孤儿头像仍能公开访问：status=%d", response.Code)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("访问孤儿头像后没有顺手清理文件：%v", err)
	}
}

// candidateAvatarPNG 生成云端头像测试使用的 PNG 图片。
func candidateAvatarPNG(t *testing.T, width int, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.RGBA{R: 40, G: 100, B: 60, A: 255})
	var output bytes.Buffer
	if err := png.Encode(&output, picture); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// candidateAvatarJPEG 生成云端头像测试使用的 JPEG 图片。
func candidateAvatarJPEG(t *testing.T, width int, height int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	picture.Set(0, 0, color.RGBA{R: 120, G: 80, B: 40, A: 255})
	var output bytes.Buffer
	if err := jpeg.Encode(&output, picture, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
