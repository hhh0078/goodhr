// 本文件负责测试岗位配置 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPositionLifecycle 验证岗位配置可以创建、列表展示和删除。
func TestPositionLifecycle(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position@example.com")

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/positions",
		bytes.NewBufferString(`{"name":"带货主播","keywords":["直播","带货"],"exclude_keywords":["销售"],"description":"成都岗位","greet_message":"你好","is_and_mode":true}`),
	)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var createPayload struct {
		Position struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Keywords  []string `json:"keywords"`
			IsAndMode bool     `json:"is_and_mode"`
			GreetMsg  string   `json:"greet_message"`
		} `json:"position"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Position.ID == "" || createPayload.Position.Name != "带货主播" {
		t.Fatalf("unexpected position payload: %+v", createPayload.Position)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/positions", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Positions []struct {
			ID string `json:"id"`
		} `json:"positions"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Positions) != 1 {
		t.Fatalf("positions length = %d", len(listPayload.Positions))
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/positions/"+createPayload.Position.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp := httptest.NewRecorder()
	routes.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}
}

// TestPositionSaveChangesPlatform 验证同一岗位可更换平台并保留编号和历史统计，同时修正详情模式。
func TestPositionSaveChangesPlatform(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-platform@example.com"
	token := loginForTest(t, routes, email)
	position, err := server.positions.store.SavePosition(Position{
		UserEmail: email, PlatformID: "boss", Name: "高级软件工程师", IsOpen: true,
		ScannedCount: 7, DailyGreetedCount: 2, SkippedCount: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ platform, mode, want string }{
		{"zhaopin", "ocr", "dom"}, {"51job", "ai", "dom"}, {"boss", "dom", "ai"},
	} {
		body, err := json.Marshal(map[string]any{
			"id": position.ID, "name": position.Name, "platform_id": item.platform,
			"keywords": []string{"Java"}, "common_config": map[string]any{"mode_default": "keyword", "detail_mode": item.mode},
		})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/positions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("更换平台 %s 失败：status=%d body=%s", item.platform, resp.Code, resp.Body.String())
		}
		saved, err := server.positions.store.PositionByID("", email, position.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if saved.PlatformID != item.platform || saved.CommonConfig["detail_mode"] != item.want || saved.ID != position.ID || saved.ScannedCount != 7 || saved.DailyGreetedCount != 2 || saved.SkippedCount != 3 || !saved.IsOpen {
			t.Fatalf("更换平台未保留岗位或未修正模式：%+v", saved)
		}
	}
}

// TestPositionSaveAllowsAIForExpiredMember 验证会员限制暂停后，过期会员仍可保存AI岗位。
func TestPositionSaveAllowsAIForExpiredMember(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-save-member@example.com"
	token := loginForTest(t, routes, email)
	if _, err := server.positions.subscriptions.AdjustSubscriptionDays(email, memberTypeMax, -10); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/positions",
		bytes.NewBufferString(`{"name":"AI岗位","common_config":{"mode_default":"ai"}}`),
	)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

// TestApplyPositionPlatformRules 验证不同平台会修正不支持的详情识别模式。
func TestApplyPositionPlatformRules(t *testing.T) {
	cases := []struct {
		name       string
		platformID string
		mode       string
		want       string
	}{
		{name: "Boss 固定 AI", platformID: "boss", mode: "dom", want: "ai"},
		{name: "猎聘猎头端只支持 DOM", platformID: "hliepin", mode: "ai", want: "dom"},
		{name: "猎聘企业端只支持 DOM", platformID: "liepin", mode: "ocr", want: "dom"},
		{name: "智联只支持 DOM", platformID: "zhaopin", mode: "ai", want: "dom"},
		{name: "前程无忧只支持 DOM", platformID: "51job", mode: "ocr", want: "dom"},
		{name: "其它平台固定 DOM", platformID: "other", mode: "ocr", want: "dom"},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			position := &Position{
				PlatformID:   item.platformID,
				CommonConfig: map[string]any{"detail_mode": item.mode},
			}
			applyPositionPlatformRules(position)
			if position.CommonConfig["detail_mode"] != item.want {
				t.Fatalf("detail_mode = %v, want %s", position.CommonConfig["detail_mode"], item.want)
			}
		})
	}
}

// TestPositionRejectsMissingName 验证岗位配置名称不能为空。
func TestPositionRejectsMissingName(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-missing@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/positions", bytes.NewBufferString(`{"keywords":["直播"]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

// TestFinishPositionRunCountsTodayOnce 验证同一结束状态重复同步时不会重复累加今日打招呼数量。
func TestFinishPositionRunCountsTodayOnce(t *testing.T) {
	store := NewMemoryPositionStore()
	now := time.Date(2026, 7, 30, 9, 0, 0, 0, time.Local)
	store.now = func() time.Time { return now }
	position, err := store.SavePosition(Position{
		UserEmail: "daily@example.com", Name: "数学教师",
		DailyGreetedCount: 5, DailyGreetedDate: now.Format(time.DateOnly),
	})
	if err != nil {
		t.Fatalf("保存岗位失败：%v", err)
	}
	if err = store.FinishPositionRun(position.ID, "completed", 3); err != nil {
		t.Fatalf("保存岗位结束状态失败：%v", err)
	}
	if err = store.FinishPositionRun(position.ID, "completed", 3); err != nil {
		t.Fatalf("重复保存岗位结束状态失败：%v", err)
	}
	actual, err := store.PositionByID("", position.UserEmail, position.ID, false)
	if err != nil {
		t.Fatalf("读取岗位失败：%v", err)
	}
	if actual.DailyGreetedCount != 8 {
		t.Fatalf("岗位今日统计不正确：today=%d", actual.DailyGreetedCount)
	}
}
