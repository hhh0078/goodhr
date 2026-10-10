// 本文件验证站内 AI 模型首项默认、旧配置修复、外部模型自由输入和中转计费一致性。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// saveBuiltinAIConfigForTest 保存内置 AI 测试配置，同时保留账号登录需要的其他公共配置。
func saveBuiltinAIConfigForTest(t *testing.T, store SystemConfigStore, config builtinAIConfig) {
	t.Helper()
	item, err := store.Get("system.app_config")
	if err != nil {
		t.Fatal(err)
	}
	var app map[string]any
	if err := json.Unmarshal([]byte(item.ConfigValue), &app); err != nil {
		t.Fatal(err)
	}
	app["builtin_ai"] = config
	value, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	item.ConfigValue = string(value)
	if err := store.Save(item); err != nil {
		t.Fatal(err)
	}
}

// builtinAIPolicyConfigForTest 提供默认字段故意指向第二项的模型配置，验证数组顺序优先。
func builtinAIPolicyConfigForTest() builtinAIConfig {
	return builtinAIConfig{
		PublicBaseURL:   "https://goodhr.example.com/api/ai-compatible/v1/chat/completions",
		UpstreamBaseURL: "https://upstream.example.com/v1/chat/completions",
		UpstreamAPIKey:  "upstream-secret",
		DefaultModel:    "second-model",
		Models: []builtinAIModel{
			{ID: "first-model", Name: "第一个模型", Description: "非常推荐", InputPricePer1MCents: 200, OutputPricePer1MCents: 540},
			{ID: "second-model", Name: "第二个模型", InputPricePer1MCents: 1, OutputPricePer1MCents: 1},
		},
	}
}

// TestBuiltinAIConfigUsesFirstValidModel 验证清理空 ID、空白和重复 ID 后仍按首项决定默认模型。
func TestBuiltinAIConfigUsesFirstValidModel(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	store := NewMemorySystemConfigStore()
	config := builtinAIPolicyConfigForTest()
	config.Models = []builtinAIModel{
		{ID: " "},
		{ID: " first-model ", Name: " ", Description: " 推荐 ", InputPricePer1MCents: 200, OutputPricePer1MCents: 540},
		{ID: "first-model", Name: "重复项", InputPricePer1MCents: 900},
		{ID: "second-model"},
	}
	saveBuiltinAIConfigForTest(t, store, config)
	got, err := loadBuiltinAIConfig(store)
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultModel != "first-model" || len(got.Models) != 2 {
		t.Fatalf("normalized models = %+v, default = %q", got.Models, got.DefaultModel)
	}
	if got.Models[0].Name != "first-model" || got.Models[0].Description != "推荐" || got.Models[0].InputPricePer1MCents != 200 {
		t.Fatalf("first model metadata = %+v", got.Models[0])
	}
}

// TestBuiltinAIConfigEmptyModelsFallback 验证空数组和全部空 ID 有可用兜底，不会访问空数组。
func TestBuiltinAIConfigEmptyModelsFallback(t *testing.T) {
	for _, test := range []struct {
		name   string
		models []builtinAIModel
	}{
		{name: "nil"},
		{name: "empty", models: []builtinAIModel{}},
		{name: "blank", models: []builtinAIModel{{ID: ""}, {ID: " "}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemorySystemConfigStore()
			config := builtinAIPolicyConfigForTest()
			config.Models = test.models
			saveBuiltinAIConfigForTest(t, store, config)
			got, err := loadBuiltinAIConfig(store)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Models) != 1 || got.Models[0].ID != "second-model" || got.DefaultModel != got.Models[0].ID {
				t.Fatalf("fallback config = %+v", got)
			}
		})
	}
}

// TestUserAIConfigStoreRepairsOnlyCurrentSiteModels 验证站内旧模型被保存修复，外部模型及其他参数保持原值。
func TestUserAIConfigStoreRepairsOnlyCurrentSiteModels(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com:8443")
	systemConfigs := NewMemorySystemConfigStore()
	saveBuiltinAIConfigForTest(t, systemConfigs, builtinAIPolicyConfigForTest())
	for _, test := range []struct {
		name    string
		baseURL string
		model   string
		want    string
	}{
		{name: "site_old", baseURL: "https://GOODHR.EXAMPLE.COM/api/ai", model: "removed-model", want: "first-model"},
		{name: "site_empty", baseURL: "https://goodhr.example.com/api/ai", want: "first-model"},
		{name: "site_valid", baseURL: "https://goodhr.example.com/api/ai", model: "second-model", want: "second-model"},
		{name: "site_trimmed", baseURL: "https://goodhr.example.com/api/ai", model: " second-model ", want: "second-model"},
		{name: "contains_site", baseURL: "https://proxy.example.com/goodhr.example.com/api/ai", model: "custom-model", want: "first-model"},
		{name: "external", baseURL: "https://custom.example.com/v1", model: "custom-model", want: "custom-model"},
		{name: "external_empty", baseURL: "https://custom.example.com/v1", want: ""},
		{name: "unset_url", model: "custom-model", want: "custom-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := NewMemoryAIConfigStore()
			config := AIConfig{BaseURL: test.baseURL, Model: test.model, APIKey: "existing-key", Temperature: 0.7, PromptTemplate: "existing prompt", Enabled: true}
			if _, err := raw.SaveUserConfig("policy@example.com", config); err != nil {
				t.Fatal(err)
			}
			store := newModelRestrictedAIConfigStore(raw, systemConfigs)
			got, err := store.UserConfig("policy@example.com")
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := raw.UserConfig("policy@example.com")
			if err != nil {
				t.Fatal(err)
			}
			if got.Model != test.want || persisted.Model != test.want {
				t.Fatalf("returned model = %q, saved model = %q, want %q", got.Model, persisted.Model, test.want)
			}
			if persisted.BaseURL != config.BaseURL || persisted.APIKey != config.APIKey || persisted.Temperature != config.Temperature || persisted.PromptTemplate != config.PromptTemplate || persisted.Enabled != config.Enabled {
				t.Fatalf("repair changed other fields: %+v", persisted)
			}
			config.Model = "new-custom-model"
			saved, err := store.SaveUserConfig("policy@example.com", config)
			if err != nil {
				t.Fatal(err)
			}
			wantSaved := "new-custom-model"
			if isCurrentSiteAIURL(test.baseURL) {
				wantSaved = "first-model"
			}
			if saved.Model != wantSaved {
				t.Fatalf("saved model = %q, want %q", saved.Model, wantSaved)
			}
		})
	}
}

// TestBuiltinAIUserInitializationAndSwitchUseFirstModel 验证初始化和切回内置均选首项，保留现有外部配置和赠送幂等性。
func TestBuiltinAIUserInitializationAndSwitchUseFirstModel(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	systemConfigs := NewMemorySystemConfigStore()
	saveBuiltinAIConfigForTest(t, systemConfigs, builtinAIPolicyConfigForTest())
	raw := NewMemoryAIConfigStore()
	store := newModelRestrictedAIConfigStore(raw, systemConfigs)
	wallet := NewMemoryAIWalletStore()
	service := NewAIWalletService(nil, wallet, store, systemConfigs)
	email := "init@example.com"
	if err := service.EnsureUserDefaultAI(email); err != nil {
		t.Fatal(err)
	}
	config, err := raw.UserConfig(email)
	if err != nil || config.Model != "first-model" {
		t.Fatalf("initial config = %+v, err = %v", config, err)
	}
	balance, _ := wallet.BalanceUnits(email)
	if err := service.EnsureUserDefaultAI(email); err != nil {
		t.Fatal(err)
	}
	if got, _ := wallet.BalanceUnits(email); got != balance {
		t.Fatalf("repeat initialization changed balance: %d -> %d", balance, got)
	}
	config.BaseURL, config.Model, config.APIKey = "https://custom.example.com/v1", "external-model", "external-key"
	if _, err := store.SaveUserConfig(email, config); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureUserDefaultAI(email); err != nil {
		t.Fatal(err)
	}
	if got, _ := raw.UserConfig(email); got.Model != "external-model" {
		t.Fatalf("existing external model changed: %+v", got)
	}
	config, err = service.ConfigureUserBuiltinAI(email)
	if err != nil || config.Model != "first-model" {
		t.Fatalf("switched config = %+v, err = %v", config, err)
	}
	key := config.APIKey
	config, err = service.ConfigureUserBuiltinAI(email)
	if err != nil || config.APIKey != key {
		t.Fatalf("repeat switch lost builtin key: %+v, err = %v", config, err)
	}
	if got, _ := wallet.BalanceUnits(email); got != balance {
		t.Fatalf("switch changed balance: %d -> %d", balance, got)
	}
}

// TestAIConfigHTTPUsesCurrentSiteModelPolicy 验证真实路由的注册默认、旧模型读取修复、保存和测试都使用同一规则。
func TestAIConfigHTTPUsesCurrentSiteModelPolicy(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	server := mustNewServer(t)
	saveBuiltinAIConfigForTest(t, server.systemConfigs, builtinAIPolicyConfigForTest())
	routes := server.Routes()
	email := "policy-http@example.com"
	token := loginForTest(t, routes, email)
	config, err := server.ai.store.UserConfig(email)
	if err != nil || config.Model != "first-model" {
		t.Fatalf("registered config = %+v, err = %v", config, err)
	}
	raw := server.ai.store.(*modelRestrictedAIConfigStore).store
	for _, path := range []string{"/api/config/user-ai", "/api/config/effective-ai"} {
		config.Model = "removed-model"
		if _, err := raw.SaveUserConfig(email, config); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		persisted, err := raw.UserConfig(email)
		if resp.Code != http.StatusOK || err != nil || persisted.Model != "first-model" || !strings.Contains(resp.Body.String(), `"model":"first-model"`) {
			t.Fatalf("%s status = %d, body = %s, persisted = %+v, err = %v", path, resp.Code, resp.Body.String(), persisted, err)
		}
	}
	for _, test := range []struct {
		baseURL string
		want    string
	}{
		{baseURL: "https://GOODHR.EXAMPLE.COM/api/ai", want: "first-model"},
		{baseURL: "https://custom.example.com/v1", want: "custom-model"},
	} {
		body, _ := json.Marshal(aiConfigRequest{BaseURL: test.baseURL, Model: "custom-model", APIKey: "test-key", Enabled: true})
		req := httptest.NewRequest(http.MethodPut, "/api/config/user-ai", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"model":"`+test.want+`"`) {
			t.Fatalf("save status = %d, body = %s", resp.Code, resp.Body.String())
		}
		server.ai.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload.Model != test.want {
				t.Fatalf("test request model = %q, want %q, err = %v", payload.Model, test.want, err)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"成功"}}]}`))}, nil
		})}
		req = httptest.NewRequest(http.MethodPost, "/api/config/test-ai", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp = httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("test status = %d, body = %s", resp.Code, resp.Body.String())
		}
	}
}

// TestBuiltinAICompatibleChatFallsBackAndChargesSameModel 验证未知和缺失模型按首项请求及扣费，合法模型仍按自身计费。
func TestBuiltinAICompatibleChatFallsBackAndChargesSameModel(t *testing.T) {
	for _, test := range []struct {
		name      string
		model     string
		stream    bool
		wantModel string
		wantCost  int64
	}{
		{name: "unknown", model: "removed-model", wantModel: "first-model", wantCost: 74000},
		{name: "missing", wantModel: "first-model", wantCost: 74000},
		{name: "valid", model: "second-model", wantModel: "second-model", wantCost: 200},
		{name: "unknown_stream", model: "removed-model", stream: true, wantModel: "first-model", wantCost: 74000},
		{name: "missing_stream", stream: true, wantModel: "first-model", wantCost: 74000},
		{name: "valid_stream", model: "second-model", stream: true, wantModel: "second-model", wantCost: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			systemConfigs := NewMemorySystemConfigStore()
			saveBuiltinAIConfigForTest(t, systemConfigs, builtinAIPolicyConfigForTest())
			wallet := NewMemoryAIWalletStore()
			email, key := "proxy@example.com", "proxy-key"
			wallet.BindAIKey(key, email)
			const initialBalance int64 = 1000000
			if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: initialBalance, Category: "recharge"}); err != nil {
				t.Fatal(err)
			}
			service := NewAIWalletService(nil, wallet, nil, systemConfigs)
			called := false
			service.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				called = true
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload.Model != test.wantModel {
					t.Fatalf("upstream model = %q, want %q, err = %v", payload.Model, test.wantModel, err)
				}
				responseBody := `{"choices":[{"message":{"content":"成功"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`
				if test.stream {
					responseBody = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000000,\"completion_tokens\":1000000}}\n\ndata: [DONE]\n\n"
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}, nil
			})}
			payload := map[string]any{"messages": []map[string]string{{"role": "user", "content": "测试"}}, "stream": test.stream}
			if test.model != "" {
				payload["model"] = test.model
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/api/ai-compatible/v1/chat/completions", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+key)
			resp := httptest.NewRecorder()
			service.CompatibleChat(resp, req)
			if !called || resp.Code != http.StatusOK {
				t.Fatalf("called = %v, status = %d, body = %s", called, resp.Code, resp.Body.String())
			}
			records, _, err := wallet.ListRecords(email, 10, 0)
			if err != nil || len(records) != 2 || records[0].ModelID != test.wantModel || records[0].ChangeUnits != -test.wantCost {
				t.Fatalf("usage records = %+v, err = %v", records, err)
			}
			if balance, _ := wallet.BalanceUnits(email); balance != initialBalance-test.wantCost {
				t.Fatalf("balance = %d, want %d", balance, initialBalance-test.wantCost)
			}
		})
	}
}

// delayedAIConfigStoreForTest 在第一次读取快照后暂停，便于确定性复现 GET 与 PUT 交错。
type delayedAIConfigStoreForTest struct {
	AIConfigStore
	readStarted chan struct{}
	resumeRead  chan struct{}
	once        sync.Once
}

// UserConfig 返回暂停前读到的快照，修复操作由真实存储的条件更新执行。
func (s *delayedAIConfigStoreForTest) UserConfig(email string) (AIConfig, error) {
	config, err := s.AIConfigStore.UserConfig(email)
	s.once.Do(func() {
		close(s.readStarted)
		<-s.resumeRead
	})
	return config, err
}

// TestAIConfigReadRepairDoesNotOverwriteConcurrentSave 验证 GET 读取旧快照后，PUT 的新地址、模型和 Key 不会被修复覆盖。
func TestAIConfigReadRepairDoesNotOverwriteConcurrentSave(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	server := mustNewServer(t)
	saveBuiltinAIConfigForTest(t, server.systemConfigs, builtinAIPolicyConfigForTest())
	routes := server.Routes()
	email := "concurrent-policy@example.com"
	token := loginForTest(t, routes, email)
	policy := server.ai.store.(*modelRestrictedAIConfigStore)
	raw := policy.store
	old, err := raw.UserConfig(email)
	if err != nil {
		t.Fatal(err)
	}
	old.Model = "removed-model"
	if _, err := raw.SaveUserConfig(email, old); err != nil {
		t.Fatal(err)
	}
	delayed := &delayedAIConfigStoreForTest{AIConfigStore: raw, readStarted: make(chan struct{}), resumeRead: make(chan struct{})}
	policy.store = delayed
	getResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/config/user-ai", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		getResponse <- resp
	}()
	select {
	case <-delayed.readStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("GET did not reach captured snapshot")
	}
	next := aiConfigRequest{BaseURL: "https://custom.example.com/v1", Model: "external-model", APIKey: "new-key", Temperature: 0.6, PromptTemplate: "new prompt", Enabled: false}
	body, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config/user-ai", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	putResponse := httptest.NewRecorder()
	routes.ServeHTTP(putResponse, req)
	close(delayed.resumeRead)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", putResponse.Code, putResponse.Body.String())
	}
	select {
	case resp := <-getResponse:
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"model":"external-model"`) || !strings.Contains(resp.Body.String(), `"api_key":"new-key"`) {
			t.Fatalf("GET status = %d, body = %s", resp.Code, resp.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GET did not finish after concurrent PUT")
	}
	got, err := raw.UserConfig(email)
	if err != nil || got.BaseURL != next.BaseURL || got.Model != next.Model || got.APIKey != next.APIKey || got.Temperature != next.Temperature || got.PromptTemplate != next.PromptTemplate || got.Enabled != next.Enabled {
		t.Fatalf("concurrent save was overwritten: %+v, err = %v", got, err)
	}
}

// TestMemoryAIConfigRepairChecksSnapshotEvenWithSameTimestamp 验证更新时间相同时仍比对全部旧字段，不会错改新配置模型。
func TestMemoryAIConfigRepairChecksSnapshotEvenWithSameTimestamp(t *testing.T) {
	raw := NewMemoryAIConfigStore()
	fixedTime := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	raw.now = func() time.Time { return fixedTime }
	previous, err := raw.SaveUserConfig("snapshot@example.com", AIConfig{BaseURL: "https://goodhr.example.com/api/ai", Model: "old-model", APIKey: "old-key", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	next := previous
	next.BaseURL, next.APIKey = "https://custom.example.com/v1", "new-key"
	if _, err := raw.SaveUserConfig("snapshot@example.com", next); err != nil {
		t.Fatal(err)
	}
	got, err := raw.RepairUserModel("snapshot@example.com", previous, "first-model")
	if err != nil || got != next {
		t.Fatalf("repair ignored changed snapshot: %+v, err = %v", got, err)
	}
}

// failingPolicySystemConfigStoreForTest 模拟系统配置读取失败，其余能力复用真实内存存储。
type failingPolicySystemConfigStoreForTest struct {
	SystemConfigStore
	err      error
	appValue string
}

// Get 按测试场景返回配置存储错误或损坏的公共配置。
func (s *failingPolicySystemConfigStoreForTest) Get(key string) (SystemConfig, error) {
	if key == "system.app_config" {
		if s.err != nil {
			return SystemConfig{}, s.err
		}
		if s.appValue != "" {
			return SystemConfig{ConfigKey: key, ConfigValue: s.appValue, Enabled: true}, nil
		}
	}
	return s.SystemConfigStore.Get(key)
}

// TestAIConfigPolicyFailuresDoNotSaveGuessedModels 验证存储错误和损坏 JSON 会阻止站内修复与保存，同时允许外部模型保存。
func TestAIConfigPolicyFailuresDoNotSaveGuessedModels(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	for _, test := range []struct {
		name     string
		err      error
		appValue string
	}{
		{name: "storage", err: errors.New("temporary config connection failure")},
		{name: "broken_json", appValue: `{"builtin_ai":`},
		{name: "broken_models", appValue: `{"builtin_ai":{"models":"invalid"}}`},
		{name: "null_app", appValue: `null`},
		{name: "null_builtin", appValue: `{"builtin_ai":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			systemConfigs := &failingPolicySystemConfigStoreForTest{SystemConfigStore: NewMemorySystemConfigStore(), err: test.err, appValue: test.appValue}
			raw := NewMemoryAIConfigStore()
			previous, err := raw.SaveUserConfig("failure@example.com", AIConfig{BaseURL: "https://goodhr.example.com/api/ai", Model: "configured-model", APIKey: "existing-key", Temperature: 0.6, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			store := newModelRestrictedAIConfigStore(raw, systemConfigs)
			if _, err := store.UserConfig("failure@example.com"); !errors.Is(err, ErrBuiltinAIConfigUnavailable) {
				t.Fatalf("read error = %v", err)
			}
			next := previous
			next.Model, next.APIKey = "requested-model", "new-key"
			if _, err := store.SaveUserConfig("failure@example.com", next); !errors.Is(err, ErrBuiltinAIConfigUnavailable) {
				t.Fatalf("save error = %v", err)
			}
			got, err := raw.UserConfig("failure@example.com")
			if err != nil || got != previous {
				t.Fatalf("failure overwrote existing config: %+v, err = %v", got, err)
			}
			next.BaseURL = "https://external.example.com/v1"
			if got, err := store.SaveUserConfig("failure@example.com", next); err != nil || got.Model != next.Model {
				t.Fatalf("external save unexpectedly blocked: %+v, err = %v", got, err)
			}
			wallet := NewMemoryAIWalletStore()
			service := NewAIWalletService(nil, wallet, store, systemConfigs)
			if err := service.EnsureUserDefaultAI("new-user@example.com"); !errors.Is(err, ErrBuiltinAIConfigUnavailable) {
				t.Fatalf("initialization error = %v", err)
			}
			if _, err := raw.UserConfig("new-user@example.com"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed initialization persisted guessed config: %v", err)
			}
			if _, err := service.ConfigureUserBuiltinAI("failure@example.com"); !errors.Is(err, ErrBuiltinAIConfigUnavailable) {
				t.Fatalf("switch error = %v", err)
			}
		})
	}
}

// TestAIConfigHTTPReturnsTemporaryErrorWhenModelsCannotLoad 验证摘要、配置读取保存和测试都返回临时错误且不改旧配置。
func TestAIConfigHTTPReturnsTemporaryErrorWhenModelsCannotLoad(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.com")
	server := mustNewServer(t)
	saveBuiltinAIConfigForTest(t, server.systemConfigs, builtinAIPolicyConfigForTest())
	routes := server.Routes()
	email := "failure-http@example.com"
	token := loginForTest(t, routes, email)
	policy := server.ai.store.(*modelRestrictedAIConfigStore)
	raw := policy.store
	previous, err := raw.UserConfig(email)
	if err != nil {
		t.Fatal(err)
	}
	failing := &failingPolicySystemConfigStoreForTest{SystemConfigStore: server.systemConfigs, err: errors.New("temporary configuration failure")}
	policy.systemConfigs, server.ai.systemConfigs, server.aiWallet.systemConfigs = failing, failing, failing
	called := false
	server.ai.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("AI should not be called with unknown model policy")
	})}
	for _, test := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/ai-wallet"},
		{method: http.MethodGet, path: "/api/config/user-ai"},
		{method: http.MethodGet, path: "/api/config/effective-ai"},
		{method: http.MethodPut, path: "/api/config/user-ai", body: `{"base_url":"https://goodhr.example.com/api/ai","model":"new-model","api_key":"new-key","enabled":true}`},
		{method: http.MethodPut, path: "/api/config/user-ai", body: `{"base_url":"https://external.example.com/v1","model":"external-model","api_key":"","enabled":true}`},
		{method: http.MethodPost, path: "/api/config/test-ai", body: `{"base_url":"https://goodhr.example.com/api/ai","model":"new-model","api_key":"new-key"}`},
		{method: http.MethodPost, path: "/api/ai-wallet/use-builtin"},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s status = %d, body = %s", test.method, test.path, resp.Code, resp.Body.String())
		}
	}
	got, err := raw.UserConfig(email)
	if called || err != nil || got != previous {
		t.Fatalf("temporary failure called AI or changed config: called=%v, config=%+v, err=%v", called, got, err)
	}
}

// TestBuiltinAIConfigMissingConfigurationStillFallsBack 验证明确缺失配置与缺失内置项仍属于合法默认场景。
func TestBuiltinAIConfigMissingConfigurationStillFallsBack(t *testing.T) {
	for _, store := range []SystemConfigStore{
		&failingPolicySystemConfigStoreForTest{SystemConfigStore: NewMemorySystemConfigStore(), err: ErrConfigNotFound},
		&failingPolicySystemConfigStoreForTest{SystemConfigStore: NewMemorySystemConfigStore(), appValue: `{}`},
	} {
		got, err := loadBuiltinAIConfig(store)
		if err != nil || len(got.Models) != 1 || got.Models[0].ID != defaultBuiltinAIModel || got.DefaultModel != got.Models[0].ID {
			t.Fatalf("missing config fallback = %+v, err = %v", got, err)
		}
	}
}
