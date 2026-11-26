// Package httpapi 文件作用：验证公共配置与充值入口共享记录的原子保存，防止旧JSON草稿覆盖备用AI，保留旧客户端未改备用的兼容路径。
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// appFallbackCASFixture 创建真实超管路由和包含备用密钥及未来字段的公共配置，不调用上游。
func appFallbackCASFixture(t *testing.T) (*Server, string) {
	t.Helper()
	t.Setenv("GOODHR_SUPER_ADMINS", "app-cas-admin@example.com")
	server := mustNewServer(t)
	token := loginForTest(t, server.Routes(), "app-cas-admin@example.com")
	cfg := builtinAIPolicyConfigForTest()
	cfg.Fallback = &BuiltinAIFallbackConfig{Enabled: true, BaseURL: "https://backup.example.com/v1", APIKey: "fixture-backup-secret", Model: "backup-model"}
	saveBuiltinAIConfigForTest(t, server.systemConfigs, cfg)
	return server, token
}

// appFallbackCASCall 通过正式公共配置保存路由发送原始配置及可选CAS基线。
func appFallbackCASCall(t *testing.T, server *Server, token, value string, expected *string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{"config_value": value}
	if expected != nil {
		body["expected_config_value"] = *expected
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/system/configs/system.app_config", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Routes().ServeHTTP(w, req)
	return w
}

// appFallbackCASEdit 保留公共配置其它字段，仅由测试明确修改选定字段。
func appFallbackCASEdit(t *testing.T, value string, edit func(map[string]any)) string {
	t.Helper()
	var app map[string]any
	if json.Unmarshal([]byte(value), &app) != nil {
		t.Fatal("bad fixture")
	}
	edit(app)
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestAppConfigCASRejectsStaleAndPreservesKey 验证新公共草稿原子保存、密钥留空保留、未来字段不丢，以及独立备用更新不会被旧草稿覆盖。
func TestAppConfigCASRejectsStaleAndPreservesKey(t *testing.T) {
	server, token := appFallbackCASFixture(t)
	before, _ := server.systemConfigs.Get("system.app_config")
	next := appFallbackCASEdit(t, before.ConfigValue, func(app map[string]any) {
		app["local_agent_version"] = "public-edit"
		fallback := app["builtin_ai"].(map[string]any)["fallback"].(map[string]any)
		fallback["api_key"] = " "
		fallback["model"] = " next-backup "
		fallback["future_option"] = map[string]any{"keep": true}
	})
	response := appFallbackCASCall(t, server, token, next, &before.ConfigValue)
	if response.Code != 200 {
		t.Fatalf("fresh CAS status=%d body=%s", response.Code, response.Body.String())
	}
	stored, _ := server.systemConfigs.Get("system.app_config")
	var app map[string]any
	_ = json.Unmarshal([]byte(stored.ConfigValue), &app)
	fallback := app["builtin_ai"].(map[string]any)["fallback"].(map[string]any)
	if fallback["api_key"] != "fixture-backup-secret" || fallback["model"] != "next-backup" || fallback["future_option"].(map[string]any)["keep"] != true || app["local_agent_version"] != "public-edit" {
		t.Fatal("公共保存丢密钥、未来字段或公共改动")
	}
	_, view := fallbackConfigCall(t, server, token, http.MethodGet, "")
	dedicated, _ := json.Marshal(map[string]any{"enabled": true, "base_url": "https://backup.example.com/v1", "model": "dedicated-new", "api_key": "", "revision": view["revision"]})
	updated, _ := fallbackConfigCall(t, server, token, http.MethodPut, string(dedicated))
	if updated.Code != 200 {
		t.Fatal("dedicated fixture update failed")
	}
	stale := appFallbackCASCall(t, server, token, stored.ConfigValue, &stored.ConfigValue)
	if stale.Code != 409 {
		t.Fatalf("stale CAS status=%d", stale.Code)
	}
	loaded, _ := server.aiWallet.loadBuiltinAIConfig()
	if loaded.Fallback.Model != "dedicated-new" || loaded.Fallback.APIKey != "fixture-backup-secret" {
		t.Fatal("旧公共草稿覆盖了专用备用更新")
	}
}

// TestAppConfigLegacyKeepsUnchangedFallback 验证无CAS旧调用仅在备用语义未变时兼容，删除或修改备用均需重新加载。
func TestAppConfigLegacyKeepsUnchangedFallback(t *testing.T) {
	server, token := appFallbackCASFixture(t)
	before, _ := server.systemConfigs.Get("system.app_config")
	next := appFallbackCASEdit(t, before.ConfigValue, func(app map[string]any) { app["legacy_public_setting"] = "changed" })
	if w := appFallbackCASCall(t, server, token, next, nil); w.Code != 200 {
		t.Fatalf("legacy unrelated status=%d", w.Code)
	}
	current, _ := server.systemConfigs.Get("system.app_config")
	for _, edit := range []func(map[string]any){func(a map[string]any) { delete(a["builtin_ai"].(map[string]any), "fallback") }, func(a map[string]any) {
		a["builtin_ai"].(map[string]any)["fallback"].(map[string]any)["model"] = "stale-model"
	}} {
		raw := appFallbackCASEdit(t, current.ConfigValue, edit)
		if w := appFallbackCASCall(t, server, token, raw, nil); w.Code != 409 {
			t.Fatalf("legacy fallback change status=%d", w.Code)
		}
	}
	blank := appFallbackCASEdit(t, current.ConfigValue, func(a map[string]any) { a["builtin_ai"].(map[string]any)["fallback"].(map[string]any)["api_key"] = "" })
	if w := appFallbackCASCall(t, server, token, blank, nil); w.Code != 200 {
		t.Fatalf("legacy blank key preservation status=%d", w.Code)
	}
	loaded, _ := server.aiWallet.loadBuiltinAIConfig()
	if loaded.Fallback.APIKey != "fixture-backup-secret" {
		t.Fatal("空密钥被保存为清空")
	}
}

// TestAppConfigManualFallbackValidation 验证公共JSON手写备用采用同一校验，坏备用原样未动时不影响旧公共设置。
func TestAppConfigManualFallbackValidation(t *testing.T) {
	server, token := appFallbackCASFixture(t)
	for _, edit := range []func(map[string]any){func(a map[string]any) {
		a["builtin_ai"].(map[string]any)["fallback"].(map[string]any)["base_url"] = "https://user:pass@bad.example.com"
	}, func(a map[string]any) { a["builtin_ai"].(map[string]any)["fallback"].(map[string]any)["model"] = "" }, func(a map[string]any) { a["builtin_ai"].(map[string]any)["fallback"] = "wrong-type" }} {
		before, _ := server.systemConfigs.Get("system.app_config")
		raw := appFallbackCASEdit(t, before.ConfigValue, edit)
		if w := appFallbackCASCall(t, server, token, raw, &before.ConfigValue); w.Code != 400 {
			t.Fatalf("invalid fallback status=%d", w.Code)
		}
		after, _ := server.systemConfigs.Get("system.app_config")
		if after.ConfigValue != before.ConfigValue {
			t.Fatal("失败保存改了现有配置")
		}
	}
	old, _ := server.systemConfigs.Get("system.app_config")
	old.ConfigValue = appFallbackCASEdit(t, old.ConfigValue, func(a map[string]any) {
		a["builtin_ai"].(map[string]any)["fallback"] = map[string]any{"enabled": "bad-legacy-type", "api_key": "fixture-backup-secret"}
	})
	if err := server.systemConfigs.Save(old); err != nil {
		t.Fatal(err)
	}
	unrelated := appFallbackCASEdit(t, old.ConfigValue, func(a map[string]any) { a["unrelated"] = "ok" })
	if w := appFallbackCASCall(t, server, token, unrelated, &old.ConfigValue); w.Code != 200 {
		t.Fatalf("unchanged legacy fallback blocked unrelated config status=%d", w.Code)
	}
}

// appFallbackRaceStore 在请求读取后、CAS前模拟另一位管理员更新备用，验证旧兼容路径也不能发生竞态覆盖。
type appFallbackRaceStore struct {
	SystemConfigStore
	change    func()
	saveCalls int
}

// Save 记录不应出现的无条件写入，真实数据仍交由现有存储。
func (s *appFallbackRaceStore) Save(cfg SystemConfig) error {
	s.saveCalls++
	return s.SystemConfigStore.Save(cfg)
}

// SaveIfUnchanged 在原子比较前注入一次真实竞争更新，不改变现有存储的CAS实现。
func (s *appFallbackRaceStore) SaveIfUnchanged(cfg SystemConfig, expected string) (bool, error) {
	if s.change != nil {
		change := s.change
		s.change = nil
		change()
	}
	return s.SystemConfigStore.SaveIfUnchanged(cfg, expected)
}

// TestAppConfigLegacyReadWriteRaceUsesCAS 验证缺少客户端基线时也以服务端读取快照保护读写之间的备用更新。
func TestAppConfigLegacyReadWriteRaceUsesCAS(t *testing.T) {
	server, token := appFallbackCASFixture(t)
	base := server.systemConfigs
	before, _ := base.Get("system.app_config")
	raw := appFallbackCASEdit(t, before.ConfigValue, func(a map[string]any) { a["unrelated"] = "legacy-edit" })
	race := &appFallbackRaceStore{SystemConfigStore: base}
	race.change = func() {
		current, _ := base.Get("system.app_config")
		current.ConfigValue = appFallbackCASEdit(t, current.ConfigValue, func(a map[string]any) {
			a["builtin_ai"].(map[string]any)["fallback"].(map[string]any)["model"] = "concurrent-new"
		})
		if err := base.Save(current); err != nil {
			t.Fatal(err)
		}
	}
	server.systemConfigs = race
	response := appFallbackCASCall(t, server, token, raw, nil)
	if response.Code != 409 || race.saveCalls != 0 {
		t.Fatalf("read/write race status=%d unconditional=%d", response.Code, race.saveCalls)
	}
	stored, _ := base.Get("system.app_config")
	if !strings.Contains(stored.ConfigValue, "concurrent-new") {
		t.Fatal("并发备用更新被覆盖")
	}
}
