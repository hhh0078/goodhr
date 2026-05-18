// 本文件负责测试云端平台账号映射 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPlatformAccountLifecycle 验证平台账号映射可以创建、查询和删除。
func TestPlatformAccountLifecycle(t *testing.T) {
	server := NewServer()
	routes := server.Routes()
	token := loginForTest(t, routes, "platform@example.com")

	// 调用创建接口，保存 Boss 平台的一个本地 profile 映射。
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/platform-accounts/create",
		bytes.NewBufferString(`{"platform_id":"boss","display_name":"Boss 主账号","local_profile_id":"boss_main"}`),
	)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var createPayload struct {
		Account struct {
			ID             string `json:"id"`
			PlatformID     string `json:"platform_id"`
			LocalProfileID string `json:"local_profile_id"`
		} `json:"account"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if createPayload.Account.ID == "" {
		t.Fatal("platform account id is empty")
	}

	// 调用列表接口，并按平台过滤，供任务创建页面选择账号。
	listReq := httptest.NewRequest(http.MethodGet, "/api/platform-accounts?platform_id=boss", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Accounts) != 1 {
		t.Fatalf("accounts length = %d", len(listPayload.Accounts))
	}

	// 调用删除接口，移除云端映射；本地 profile 文件不在云端删除。
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/platform-accounts/"+createPayload.Account.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp := httptest.NewRecorder()
	routes.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}
}

// TestPlatformAccountRejectsDuplicate 验证同平台同 profile 不会重复创建。
func TestPlatformAccountRejectsDuplicate(t *testing.T) {
	server := NewServer()
	routes := server.Routes()
	token := loginForTest(t, routes, "duplicate@example.com")

	body := `{"platform_id":"boss","display_name":"Boss 主账号","local_profile_id":"boss_main"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/platform-accounts/create", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)

		if i == 0 && resp.Code != http.StatusOK {
			t.Fatalf("first create status = %d, body = %s", resp.Code, resp.Body.String())
		}
		if i == 1 && resp.Code != http.StatusConflict {
			t.Fatalf("second create status = %d, want %d", resp.Code, http.StatusConflict)
		}
	}
}
