// 本文件验证关注赠送配置的动态读取、超管保存校验和旧注册及本地程序字段保留，不连接生产服务。
package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestWechatFollowRewardDaysValidation 验证缺失字段默认两天且只接受一至三百六十五的整数。
func TestWechatFollowRewardDaysValidation(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    int
		invalid bool
	}{
		{raw: `{}`, want: 2},
		{raw: `{"trial_days":7,"local_agent":[{"version":"keep"}]}`, want: 2},
		{raw: `{"wechat_follow_reward_days":1}`, want: 1},
		{raw: `{"wechat_follow_reward_days":2}`, want: 2},
		{raw: `{"wechat_follow_reward_days":7}`, want: 7},
		{raw: `{"wechat_follow_reward_days":365}`, want: 365},
		{raw: `{"wechat_follow_reward_days":0}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":-1}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":366}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":2.5}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":"2"}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":null}`, invalid: true},
		{raw: `{"wechat_follow_reward_days":true}`, invalid: true},
		{raw: `[]`, invalid: true},
		{raw: `null`, invalid: true},
		{raw: `{`, invalid: true},
	} {
		t.Run(test.raw, func(t *testing.T) {
			got, err := parseWechatFollowRewardDays(test.raw)
			if (err != nil) != test.invalid || (!test.invalid && got != test.want) {
				t.Fatalf("配置解析结果=%d, 错误=%v, 期望=%d, 是否无效=%v", got, err, test.want, test.invalid)
			}
		})
	}
}

// TestWechatFollowRewardAdminSave 验证更新关注天数会立即反映到用户状态且不改变注册、更新或未知字段。
func TestWechatFollowRewardAdminSave(t *testing.T) {
	server, official := newWechatOfficialTestServer(t, "safe")
	adminToken := wechatTestSession(t, server, "wechat-admin@example.com")
	userToken := wechatTestSession(t, server, "reward-config-user@example.com")
	raw := `{"trial_days":7,"wechat_follow_reward_days":7,"local_agent":[{"version":"6.2.10.1","url_win":"https://example.com/agent.zip"}],"runtime_components":{"node":{"version":"22"}},"future_field":{"keep":true}}`
	body, err := json.Marshal(map[string]string{"config_value": raw})
	if err != nil {
		t.Fatal(err)
	}
	response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/system.onboarding_config", adminToken, string(body))
	if response.Code != http.StatusOK {
		t.Fatalf("超管保存失败：%d %s", response.Code, response.Body.String())
	}
	saved, err := server.systemConfigs.Get("system.onboarding_config")
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfigValue != raw {
		t.Fatalf("保存修改了无关字段：%s", saved.ConfigValue)
	}
	days, err := loadWechatFollowRewardDays(server.systemConfigs)
	if err != nil || days != 7 {
		t.Fatalf("动态加载=%d, 错误=%v", days, err)
	}
	response = wechatTestRequest(server.Routes(), http.MethodGet, "/api/wechat-official/status", userToken, "")
	var status map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || status["reward_days"] != float64(7) {
		t.Fatalf("用户状态未读取新天数：%d %s", response.Code, response.Body.String())
	}
	response = wechatTestRequest(server.Routes(), http.MethodGet, "/api/admin/system/configs/", adminToken, "")
	var payload struct {
		Configs []SystemConfig `json:"configs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, config := range payload.Configs {
		if config.ConfigKey != wechatOfficialConfigKey {
			continue
		}
		found = true
		var fields map[string]any
		if err := json.Unmarshal([]byte(config.ConfigValue), &fields); err != nil {
			t.Fatal(err)
		}
		if fields["reward_days"] != float64(7) || fields["encoding_aes_key"] != "" {
			t.Fatalf("超管展示未同步天数或泄露密钥：%v", fields["reward_days"])
		}
	}
	if !found {
		t.Fatal("超管配置遗漏公众号")
	}
	// 保存公众号自身信息也必须采用统一配置的天数，不能退回旧的固定奖励。
	officialBody, err := json.Marshal(official)
	if err != nil {
		t.Fatal(err)
	}
	body, err = json.Marshal(map[string]string{"config_value": string(officialBody)})
	if err != nil {
		t.Fatal(err)
	}
	response = wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/"+wechatOfficialConfigKey, adminToken, string(body))
	var updated struct {
		Config SystemConfig `json:"config"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(updated.Config.ConfigValue), &fields); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || fields["reward_days"] != float64(7) {
		t.Fatal("公众号保存响应未使用动态天数")
	}
}

// TestWechatFollowRewardInvalidSavePreservesConfig 验证无效天数和非超管请求不会覆盖任何旧配置。
func TestWechatFollowRewardInvalidSavePreservesConfig(t *testing.T) {
	server, _ := newWechatOfficialTestServer(t, "safe")
	adminToken := wechatTestSession(t, server, "wechat-admin@example.com")
	userToken := wechatTestSession(t, server, "reward-config-user@example.com")
	before, err := server.systemConfigs.Get("system.onboarding_config")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"wechat_follow_reward_days":0}`, `{"wechat_follow_reward_days":366}`, `{"wechat_follow_reward_days":2.5}`, `{"wechat_follow_reward_days":null}`, `{"wechat_follow_reward_days":"2"}`, `[]`} {
		body, err := json.Marshal(map[string]string{"config_value": raw})
		if err != nil {
			t.Fatal(err)
		}
		response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/system.onboarding_config", adminToken, string(body))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("无效配置被保存：%s => %d", raw, response.Code)
		}
		after, err := server.systemConfigs.Get("system.onboarding_config")
		if err != nil || after != before {
			t.Fatal("无效请求覆盖旧配置")
		}
	}
	response := wechatTestRequest(server.Routes(), http.MethodPut, "/api/admin/system/configs/system.onboarding_config", userToken, `{"config_value":"{\"wechat_follow_reward_days\":7}"}`)
	if response.Code != http.StatusForbidden {
		t.Fatal("普通用户可以调整赠送天数")
	}
}
