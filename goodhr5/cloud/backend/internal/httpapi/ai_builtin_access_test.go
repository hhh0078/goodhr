// Package httpapi 本文件验证官方AI域名边界、取消会员后的启动以及自定义接口和0.10元余额拦截。
package httpapi

import (
	"net/http"
	"testing"
)

// TestOfficialAIURLRejectsSpoofing 验证大小写及子域名可用，路径、查询参数、用户名和相似域名不能绕过。
func TestOfficialAIURLRejectsSpoofing(t *testing.T) {
	for _, address := range []string{"https://58it.cn/v1", "https://GOODHR.58IT.CN/api/ai-compatible/v1/chat/completions", "http://ai.58it.cn:8443/v1"} {
		if !isOfficialAIURL(address) {
			t.Errorf("合法地址被拒绝：%s", address)
		}
	}
	for _, address := range []string{"", "58it.cn", "https://other.invalid/58it.cn", "https://other.invalid/?next=58it.cn", "https://58it.cn.other.invalid/v1", "https://fake58it.cn/v1", "https://58it.cn@other.invalid/v1", "https://user:password@goodhr.58it.cn/v1", "ftp://58it.cn/v1"} {
		if isOfficialAIURL(address) {
			t.Errorf("伪装地址通过：%s", address)
		}
	}
}

// TestFixedAIKeepsResumeStorageChoice 验证固定AI模式不会覆盖用户已有的简历入库选择。
func TestFixedAIKeepsResumeStorageChoice(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		position := Position{PlatformID: "boss", CommonConfig: map[string]any{"mode_default": "keyword", "output_structured_resume": enabled}}
		applyPositionPlatformRules(&position)
		if position.CommonConfig["mode_default"] != "ai" || position.CommonConfig["output_structured_resume"] != enabled {
			t.Fatalf("固定模式改写了入库选择：%+v", position.CommonConfig)
		}
	}
}

// TestPositionStartBuiltinAndBalanceBoundary 验证历史关键词岗位也不能绕过内置接口、余额与过期会员新策略。
func TestPositionStartBuiltinAndBalanceBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, address, code string
		units               int64
		want                int
	}{
		{"custom", "https://custom.invalid/v1", "BUILTIN_AI_REQUIRED", 1000, http.StatusForbidden},
		{"spoof", "https://custom.invalid/58it.cn", "BUILTIN_AI_REQUIRED", 1000, http.StatusForbidden},
		{"below", defaultBuiltinAIBaseURL, "AI_BALANCE_INSUFFICIENT", 999, http.StatusPaymentRequired},
		{"exact", defaultBuiltinAIBaseURL, "", 1000, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := mustNewServer(t)
			routes := server.Routes()
			email := "builtin-start-" + tc.name + "@example.com"
			token := loginForTest(t, routes, email)
			bindPositionDeviceForTest(t, routes, token)
			positionID := createPositionWithConfigForTest(t, routes, token, "历史关键词岗位", `{"mode_default":"keyword","detail_mode":"dom"}`)
			if _, err := server.positionExecution.aiConfigs.SaveUserConfig(email, AIConfig{BaseURL: tc.address, Model: defaultBuiltinAIModel, APIKey: "ghai_synthetic", Enabled: true}); err != nil {
				t.Fatal(err)
			}
			balance, err := server.positionExecution.aiWallet.BalanceUnits(email)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = server.positionExecution.aiWallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: tc.units - balance, Category: "test"}); err != nil {
				t.Fatal(err)
			}
			if _, err = server.positionExecution.subscriptions.AdjustSubscriptionDays(email, memberTypeMax, -100); err != nil {
				t.Fatal(err)
			}
			response := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.code != "" && positionStartErrorCodeForTest(t, response) != tc.code {
				t.Fatal("未按预期拦截启动")
			}
			position, err := server.positionExecution.store.PositionByID("", email, positionID, true)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want != http.StatusOK && position.Status == "running" {
				t.Fatal("拒绝启动却已设置运行状态")
			}
		})
	}
}
