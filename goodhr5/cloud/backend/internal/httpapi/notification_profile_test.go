// 本文件负责测试用户邮件通知画像 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNotificationProfile 验证登录用户可以读取和保存邮件通知画像。
func TestNotificationProfile(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "notice-profile@example.com")

	getReq := httptest.NewRequest(http.MethodGet, "/api/config/notification-profile", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp := httptest.NewRecorder()
	routes.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResp.Code, getResp.Body.String())
	}

	saveBody := bytes.NewBufferString(`{"completed":true,"user_type":"hr","gender":"female","platforms":["BOSS直聘","BOSS直聘","猎聘"],"os":"Mac","browser":"Chrome"}`)
	saveReq := httptest.NewRequest(http.MethodPut, "/api/config/notification-profile", saveBody)
	saveReq.Header.Set("Authorization", "Bearer "+token)
	saveResp := httptest.NewRecorder()
	routes.ServeHTTP(saveResp, saveReq)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", saveResp.Code, saveResp.Body.String())
	}
	var payload struct {
		Profile NotificationProfile `json:"profile"`
	}
	if err := json.NewDecoder(saveResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Profile.UserType != "hr" || len(payload.Profile.Platforms) != 2 {
		t.Fatalf("unexpected profile: %+v", payload.Profile)
	}
}
