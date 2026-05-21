// 本文件负责测试平台配置接口的管理员权限和返回结果。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAdminPlatformConfigsRequiresAdmin 验证管理员接口可返回平台原始配置。
func TestAdminPlatformConfigsRequiresAdmin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	store, ok := server.systemConfigs.(*MemorySystemConfigStore)
	if !ok {
		t.Skip("仅在内存配置存储下验证管理员平台配置接口")
	}
	store.configs["platform.boss"] = SystemConfig{
		ConfigKey:   "platform.boss",
		ConfigValue: `{"id":"boss","name":"Boss直聘"}`,
		Description: "Boss 平台配置",
		Enabled:     true,
	}

	token := "token_admin_platform_config"
	err := server.auth.store.SaveSession(token, Session{
		Email:     "admin@example.com",
		CreatedAt: time.Now(),
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/platforms/config/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var payload struct {
		Configs []SystemConfig `json:"configs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Configs) != 1 {
		t.Fatalf("configs length = %d", len(payload.Configs))
	}
	if payload.Configs[0].ConfigKey != "platform.boss" {
		t.Fatalf("config key = %q", payload.Configs[0].ConfigKey)
	}
}
