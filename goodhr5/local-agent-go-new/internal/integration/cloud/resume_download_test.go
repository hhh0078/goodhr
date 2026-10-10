// Package cloud 文件作用：验证下载设置必须明确确认、云端重复与消息字段合同，不调用正式账号。
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDownloadSettingsRequireExplicitBoolean 验证未知设置不会被默认开启，关闭和默认开启的明确响应都可解析。
func TestDownloadSettingsRequireExplicitBoolean(t *testing.T) {
	for _, item := range []struct {
		body           string
		valid, enabled bool
	}{{`{"ok":true}`, false, false}, {`{"ok":true,"settings":{}}`, false, false}, {`{"ok":true,"settings":{"auto_import_downloads":true}}`, true, true}, {`{"ok":true,"settings":{"auto_import_downloads":false}}`, true, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/resume-library/settings" || r.Header.Get("Authorization") != "Bearer web-token" {
				t.Error("错误接口或授权")
			}
			_, _ = w.Write([]byte(item.body))
		}))
		result, err := New(server.URL).ResumeDownloadSettings(context.Background(), "web-token")
		server.Close()
		if (err == nil) != item.valid || result.AutoImportDownloads != item.enabled {
			t.Fatalf("设置=%+v err=%v", result, err)
		}
	}
}

// TestResumeDownloadDuplicateAndMessageContract 验证隐藏重复不能自动入库，通知仅使用当前账号且字段为body。
func TestResumeDownloadDuplicateAndMessageContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/candidates/duplicates" {
			_, _ = w.Write([]byte(`{"ok":true,"matches":[],"hidden_match":true}`))
			return
		}
		if r.URL.Path != "/api/messages/push" {
			t.Fatal("错误接口")
		}
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("错误JSON")
		}
		if string(body["body"]) != `"草稿仍在本机"` || body["content"] != nil || body["email"] != nil || body["user_id"] != nil || string(body["idempotency_key"]) != `"stable-message"` {
			t.Fatalf("错误消息合同=%s", body)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	client := New(server.URL)
	duplicate, err := client.LibraryCandidateHasDuplicate(context.Background(), "web-token", LibraryCandidate{})
	if err != nil || !duplicate {
		t.Fatal("隐藏重复未拦截")
	}
	if err := client.PushResumeMessage(context.Background(), "web-token", "待核对", "草稿仍在本机", "/admin/resumes", "stable-message"); err != nil {
		t.Fatal(err)
	}
}
