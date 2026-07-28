// Package cloud 验证个人运行配置和云端登录状态错误的强类型解析。
package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// TestPreferencesReadsStrongTypes 验证个人运行配置会完整进入强类型结构。
func TestPreferencesReadsStrongTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config":{"detail_open_probability":66,"scroll_delay_min":2,"scroll_delay_max":5,"request_unused":1,"rest_times_min":1,"rest_times_max":2}}`))
	}))
	defer server.Close()

	preferences, err := New(server.URL).Preferences(context.Background(), "token")
	if err != nil {
		t.Fatalf("读取个人配置失败：%v", err)
	}
	if preferences.DetailOpenProbability != 66 || preferences.ScrollDelayMin != 2 || preferences.ScrollDelayMax != 5 {
		t.Fatalf("个人配置解析不正确：%+v", preferences)
	}
	if preferences.RestTimesMin != 1 || preferences.RestTimesMax != 2 {
		t.Fatalf("休息次数解析不正确：%+v", preferences)
	}
}

// TestValidateSessionReturnsAuthExpiredCode 验证 401 可以被运行中登录检查稳定识别。
func TestValidateSessionReturnsAuthExpiredCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"session is invalid or expired"}`))
	}))
	defer server.Close()

	_, err := New(server.URL).ValidateSession(context.Background(), "expired-token")
	if err == nil || !IsAuthExpired(err) {
		t.Fatalf("401 没有识别为登录失效：%v", err)
	}
}

// TestSyncCompletedSummaryRetriesNotice 验证完成邮件未确认时最多重试三次。
func TestSyncCompletedSummaryRetriesNotice(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		noticeSent := requests == 3
		_, _ = w.Write([]byte(`{"success":true,"notice_sent":` + strconv.FormatBool(noticeSent) + `}`))
	}))
	defer server.Close()

	err := New(server.URL).SyncCompletedSummary(context.Background(), "token", TaskSummary{PositionID: "position-1"})
	if err != nil {
		t.Fatalf("同步完成状态失败：%v", err)
	}
	if requests != 3 {
		t.Fatalf("完成邮件应在第三次确认，实际请求 %d 次", requests)
	}
}

// TestDecodeLegacyPlatformConfig 验证云端当前平台 JSON 能转换为新版 URL、选择器和行为配置。
func TestDecodeLegacyPlatformConfig(t *testing.T) {
	raw := json.RawMessage(`{
		"id":"zhaopin",
		"name":"智联招聘",
		"auth":{
			"entry_url":"https://rd6.zhaopin.com/app/recommend",
			"login_url_prefixes":["https://passport.zhaopin.com"]
		},
		"card":{
			"item":{"parent_classes":[["[role=group]"]],"target_classes":[[".candidate-card"]]},
			"scroll":{"target_classes":[["body"]]},
			"fields":[{"name":{"target_classes":[[".candidate-name"]]}}]
		},
		"actions":{"greetBtn":{"target_classes":[[".greet-button"]]}},
		"detail":{
			"openTarget":{"target_classes":[[".candidate-card"]]},
			"content":{"target_classes":[[".resume-detail"]]}
		},
		"position":{
			"switchBtn":{"target_classes":[[".position-switch"]]},
			"item":{"target_classes":[[".position-item"]]}
		},
		"behavior":{"needsDetailPage":true,"supportsPaging":false}
	}`)
	cfg, err := decodePlatformConfig(raw, "zhaopin")
	if err != nil {
		t.Fatalf("转换云端平台配置失败：%v", err)
	}
	if cfg.EntryURL != "https://rd6.zhaopin.com/app/recommend" ||
		cfg.LoginURL != "https://passport.zhaopin.com" {
		t.Fatalf("平台页面地址转换不完整：%+v", cfg)
	}
	if len(cfg.Selectors["candidate.item"].Parents) != 1 ||
		len(cfg.Selectors["candidate.greet"].Target.Selectors) != 1 ||
		len(cfg.CandidateFields["name"].Target.Selectors) != 1 {
		t.Fatalf("平台选择器转换不完整：%+v", cfg)
	}
	if !cfg.Behavior.DirectPositionSelection ||
		!cfg.Behavior.SelectFirstPositionResult ||
		!cfg.Behavior.NeedsDetail {
		t.Fatalf("智联平台行为转换不完整：%+v", cfg.Behavior)
	}
}
