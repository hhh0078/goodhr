// Package cloudapi 负责测试 Go 本地程序访问云端接口的能力。
package cloudapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchPlatformConfig 验证公开平台配置读取和 JSON 字符串解码。
func TestFetchPlatformConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/platforms/config/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"configs": []map[string]any{
				{
					"config_key":   "platform.boss",
					"config_value": `{"name":"Boss直聘","selectors":{"card":".job-card"}}`,
				},
			},
		})
	}))
	defer server.Close()

	client := New(server.URL)
	config, err := client.FetchPlatformConfig(t.Context(), "boss")
	if err != nil {
		t.Fatal(err)
	}
	if config["id"] != "boss" || config["name"] != "Boss直聘" {
		t.Fatalf("config = %+v", config)
	}
}

// TestFetchSubscription 验证会员状态读取会携带登录令牌。
func TestFetchSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"subscription": map[string]any{
				"active":      true,
				"member_type": "plus",
			},
		})
	}))
	defer server.Close()

	client := New(server.URL)
	subscription, err := client.FetchSubscription(t.Context(), "token-1")
	if err != nil {
		t.Fatal(err)
	}
	if subscription["active"] != true || subscription["member_type"] != "plus" {
		t.Fatalf("subscription = %+v", subscription)
	}
}

// TestFetchPlatformConfigError 验证常见英文错误会转成中文。
func TestFetchPlatformConfigError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "failed to load system configs"})
	}))
	defer server.Close()

	client := New(server.URL)
	_, err := client.FetchPlatformConfig(t.Context(), "boss")
	if err == nil || err.Error() != "读取平台配置失败" {
		t.Fatalf("err = %v", err)
	}
}
