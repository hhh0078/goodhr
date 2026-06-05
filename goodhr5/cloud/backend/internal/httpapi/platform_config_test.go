// 本文件负责测试平台配置接口的管理员权限和返回结果。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPublicPlatformConfigsWithoutLogin 验证未登录时也可以读取平台配置。
func TestPublicPlatformConfigsWithoutLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	store, ok := server.systemConfigs.(*MemorySystemConfigStore)
	if !ok {
		t.Skip("仅在内存配置存储下验证公开平台配置接口")
	}
	store.configs["platform.boss"] = SystemConfig{
		ConfigKey:   "platform.boss",
		ConfigValue: `{"id":"boss","name":"Boss直聘"}`,
		Description: "Boss 平台配置",
		Enabled:     true,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platforms/config/", nil)
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
	if len(payload.Configs) != 1 || payload.Configs[0].ConfigKey != "platform.boss" {
		t.Fatalf("unexpected configs: %+v", payload.Configs)
	}
}

// TestAdminPlatformConfigsRequiresSuperAdmin 验证超管接口可返回平台原始配置。
func TestAdminPlatformConfigsRequiresSuperAdmin(t *testing.T) {
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
		Email:     "1224299352@qq.com",
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

// TestAdminPlatformConfigsRejectsTenantAdmin 验证租户管理员不是超管时不能读取平台原始配置。
func TestAdminPlatformConfigsRejectsTenantAdmin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	token := "token_tenant_admin_platform_config"
	err := server.auth.store.SaveSession(token, Session{
		Email:     "tenant-admin@example.com",
		CreatedAt: time.Now(),
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/platforms/config/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body = %s", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestAdminPlatformConfigsUpdate(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	store, ok := server.systemConfigs.(*MemorySystemConfigStore)
	if !ok {
		t.Skip("仅在内存配置存储下验证管理员平台配置更新接口")
	}
	store.configs["platform.boss"] = SystemConfig{
		ConfigKey:   "platform.boss",
		ConfigValue: `{"id":"boss","name":"Boss直聘"}`,
		Description: "Boss 平台配置",
		Enabled:     true,
	}

	token := "token_admin_platform_config_update"
	err := server.auth.store.SaveSession(token, Session{
		Email:     "1224299352@qq.com",
		CreatedAt: time.Now(),
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	body := bytes.NewBufferString(`{"config_value":"{\"id\":\"boss\",\"name\":\"Boss直聘\",\"domain\":\"zhipin.com\"}"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/platforms/config/platform.boss", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	saved, err := store.Get("platform.boss")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfigValue != `{"id":"boss","name":"Boss直聘","domain":"zhipin.com"}` {
		t.Fatalf("config value = %s", saved.ConfigValue)
	}
}
