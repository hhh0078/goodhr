// 本文件回归验证 WorkBuddy 首次安装引导、各系统下载包选择和无岗位时的下一步。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWorkBuddyDownloads 验证正式发布字段、旧配置、无效地址和缺失配置都能安全处理。
func TestWorkBuddyDownloads(t *testing.T) {
	for _, test := range []struct {
		name, config, windows, macos string
	}{
		{
			name:    "各系统使用首条有效发布，旧版本不能覆盖",
			config:  `{"local_agent":[null,{"url_win":"javascript:alert(1)","url_mac":17},{"version":"6.3.4.11","url_win":" https://example.com/windows-latest.zip "},{"version":"6.3.4.11","url_mac":"https://example.com/mac-latest.pkg"},{"version":"6.3.4.10","url_win":"https://example.com/windows-old.zip","url_mac":"https://example.com/mac-old.pkg"}]}`,
			windows: "https://example.com/windows-latest.zip", macos: "https://example.com/mac-latest.pkg",
		},
		{
			name:    "兼容旧版平台字段并防止地址串到另一系统",
			config:  `{"local_agent":[{"platform":"WIN","url":"https://example.com/windows.zip"},{"platform":"Darwin","url":"/downloads/mac.pkg"},{"platform":"mac","url":"https://example.com/old-mac.pkg"}]}`,
			windows: "https://example.com/windows.zip", macos: "/downloads/mac.pkg",
		},
		{name: "空发布记录", config: `{"local_agent":[]}`},
		{name: "配置损坏", config: `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			configs := NewMemorySystemConfigStore()
			if err := configs.Save(SystemConfig{ConfigKey: "system.onboarding_config", ConfigValue: test.config, Enabled: true}); err != nil {
				t.Fatal(err)
			}
			service := &WorkBuddyMCPService{systemConfigs: configs}
			downloads := service.localAgentDownloads()
			if downloads["windows"] != test.windows || downloads["macos"] != test.macos {
				t.Fatalf("downloads = %#v", downloads)
			}
		})
	}
	if downloads := (&WorkBuddyMCPService{}).localAgentDownloads(); downloads["windows"] != "" || downloads["macos"] != "" {
		t.Fatalf("missing store downloads = %#v", downloads)
	}
}

// TestWorkBuddyCheckSetup 验证未安装、历史绑定离线、在线三种场景的真实 MCP 返回结构和引导。
func TestWorkBuddyCheckSetup(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.test")
	const email = "first-use@example.test"
	for _, test := range []struct {
		name          string
		bound, online bool
	}{
		{name: "新用户尚未绑定"},
		{name: "历史绑定不等于当前已安装", bound: true},
		{name: "在线但尚未绑定不能就绪", online: true},
		{name: "已绑定且命令通道在线", bound: true, online: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			agents := NewMemoryAgentStore()
			if test.bound {
				if _, err := agents.SaveBinding(AgentBinding{UserEmail: email, MachineID: "test-machine", AgentVersion: "6.3.4.11"}); err != nil {
					t.Fatal(err)
				}
			}
			hub := NewAgentWSHub(nil)
			if test.online {
				hub.clients[email] = NewAgentWSClient(email, nil, hub)
			}
			service := &WorkBuddyMCPService{agents: agents, agentWS: hub, systemConfigs: NewMemorySystemConfigStore()}
			result := callWorkBuddyToolForTest(t, service, email, "check_setup")
			local := result["local_program"].(map[string]any)
			if local["bound"] != test.bound || local["online"] != test.online || local["ready"] != (test.bound && test.online) {
				t.Fatalf("local_program = %#v", local)
			}
			if result["console_url"] != "https://goodhr.example.test/admin/positions" || len(result["setup_steps"].([]any)) == 0 {
				t.Fatalf("missing first-use guidance = %#v", result)
			}
			if !test.bound || !test.online {
				if result["download_page"] != "https://goodhr.example.test/download" || result["downloads"].(map[string]any)["macos"] == "" {
					t.Fatalf("missing usable installation entry = %#v", result)
				}
				if !strings.Contains(result["next_step"].(string), "相同的账号") {
					t.Fatalf("missing account guidance = %#v", result)
				}
			}
			if test.bound && !test.online && !strings.Contains(result["next_step"].(string), "已卸载") {
				t.Fatalf("historical binding wrongly treated as installed = %#v", result)
			}
		})
	}
}

// TestWorkBuddyQuotaWithoutPositions 验证零岗位账号可以获得控制台入口，而不是陷入无岗位可选的死路。
func TestWorkBuddyQuotaWithoutPositions(t *testing.T) {
	t.Setenv("GOODHR_PUBLIC_BASE_URL", "https://goodhr.example.test")
	service := &WorkBuddyMCPService{positions: NewMemoryPositionStore(), subscriptions: NewMemorySubscriptionStore()}
	result := callWorkBuddyToolForTest(t, service, "new-user@example.test", "get_quota")
	if result["position_count"] != float64(0) || len(result["positions"].([]any)) != 0 {
		t.Fatalf("unexpected positions = %#v", result)
	}
	if result["console_url"] != "https://goodhr.example.test/admin/positions" || !strings.Contains(result["next_step"].(string), "创建并开启岗位") {
		t.Fatalf("missing position setup guidance = %#v", result)
	}
}

// callWorkBuddyToolForTest 使用连接器专用会话执行真实 MCP 工具请求并解析文本结果。
func callWorkBuddyToolForTest(t *testing.T, service *WorkBuddyMCPService, email, tool string) map[string]any {
	t.Helper()
	store := NewMemoryAuthStore()
	if err := store.SaveSession("test-connector-token", Session{Email: email, Kind: workbuddySessionKind}, time.Hour); err != nil {
		t.Fatal(err)
	}
	service.auth = &AuthService{store: store}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer test-connector-token")
	response := httptest.NewRecorder()
	service.ServeMCP(response, request)
	var payload struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload.Result.IsError || len(payload.Result.Content) != 1 {
		t.Fatalf("MCP %s response = %d %s", tool, response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(payload.Result.Content[0].Text), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
