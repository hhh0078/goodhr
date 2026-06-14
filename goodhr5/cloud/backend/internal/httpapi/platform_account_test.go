// 本文件负责测试云端平台账号映射 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPlatformAccountLifecycle 验证平台账号信息接口支持创建、查询和删除。
func TestPlatformAccountLifecycle(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "platform@example.com"
	token := loginForTest(t, routes, email)

	createReq := httptest.NewRequest(http.MethodPost, "/api/platform-accounts/create", bytes.NewBufferString(`{"platform_id":"boss","display_name":"Boss 主账号","local_profile_id":"boss_main"}`))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Account struct {
			ID             string `json:"id"`
			DisplayName    string `json:"display_name"`
			LocalProfileID string `json:"local_profile_id"`
		} `json:"account"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Account.ID == "" || createPayload.Account.LocalProfileID != "boss_main" {
		t.Fatalf("unexpected create account payload: %+v", createPayload.Account)
	}

	// 调用列表接口，并按平台过滤，供任务创建页面选择平台账号。
	listReq := httptest.NewRequest(http.MethodGet, "/api/platform-accounts?platform_id=boss", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Accounts []struct {
			ID             string `json:"id"`
			DisplayName    string `json:"display_name"`
			LocalProfileID string `json:"local_profile_id"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Accounts) != 1 {
		t.Fatalf("accounts length = %d", len(listPayload.Accounts))
	}
	if listPayload.Accounts[0].ID != createPayload.Account.ID || listPayload.Accounts[0].LocalProfileID != "boss_main" {
		t.Fatalf("unexpected account payload: %+v", listPayload.Accounts[0])
	}

	// 调用删除接口，删除对应平台账号信息记录。
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/platform-accounts/"+createPayload.Account.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp := httptest.NewRecorder()
	routes.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}
}

// TestPlatformAccountCreateDuplicate 验证同平台同本地 profile 不能重复创建。
func TestPlatformAccountCreateDuplicate(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "duplicate@example.com")

	for index := 0; index < 2; index++ {
		req := httptest.NewRequest(http.MethodPost, "/api/platform-accounts/create", bytes.NewBufferString(`{"platform_id":"boss","display_name":"Boss 主账号","local_profile_id":"boss_main"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if index == 0 && resp.Code != http.StatusOK {
			t.Fatalf("first create status = %d, body = %s", resp.Code, resp.Body.String())
		}
		if index == 1 && resp.Code != http.StatusConflict {
			t.Fatalf("second create status = %d, want %d, body = %s", resp.Code, http.StatusConflict, resp.Body.String())
		}
	}
}
