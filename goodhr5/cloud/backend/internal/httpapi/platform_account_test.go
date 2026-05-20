// 本文件负责测试云端平台账号映射 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPlatformAccountLifecycle 验证平台账号兼容接口会从 cookie 表查询和删除记录。
func TestPlatformAccountLifecycle(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "platform@example.com"
	token := loginForTest(t, routes, email)

	cookieID := createCookieRecordForTest(t, server, email, "boss", "Boss 主账号")

	// 调用列表接口，并按平台过滤，供任务创建页面选择 cookie 账号。
	listReq := httptest.NewRequest(http.MethodGet, "/api/platform-accounts?platform_id=boss", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Accounts []struct {
			ID           string `json:"id"`
			DisplayName  string `json:"display_name"`
			CookieStatus string `json:"cookie_status"`
			LocalProfile string `json:"local_profile_id"`
		} `json:"accounts"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Accounts) != 1 {
		t.Fatalf("accounts length = %d", len(listPayload.Accounts))
	}
	if listPayload.Accounts[0].ID != cookieID || listPayload.Accounts[0].CookieStatus != "available" {
		t.Fatalf("unexpected account payload: %+v", listPayload.Accounts[0])
	}

	// 调用删除接口，删除对应 cookie 账号记录。
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/platform-accounts/"+cookieID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp := httptest.NewRecorder()
	routes.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleteResp.Code, deleteResp.Body.String())
	}
}

// TestPlatformAccountCreateRemoved 验证独立平台账号创建入口已经关闭。
func TestPlatformAccountCreateRemoved(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "duplicate@example.com")

	req := httptest.NewRequest(http.MethodPost, "/api/platform-accounts/create", bytes.NewBufferString(`{"platform_id":"boss"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

// createCookieRecordForTest 直接写入一条测试 cookie 账号记录，并返回 cookie ID。
func createCookieRecordForTest(t *testing.T, server *Server, email string, platformID string, displayName string) string {
	t.Helper()

	tenant, err := server.tenants.store.GetOrCreateTenant(email)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := server.cookies.store.Create(CookieRecord{
		TenantID:      tenant.ID,
		UserID:        email,
		PlatformID:    platformID,
		DisplayName:   displayName,
		CookieType:    "json",
		Status:        "available",
		EncryptedData: []byte("encrypted"),
		EncryptedKeys: map[string]string{"agent": "key"},
		SizeBytes:     9,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rec.ID
}
