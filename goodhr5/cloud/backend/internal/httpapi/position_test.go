// 本文件负责测试岗位配置 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
