// 本文件负责测试云端 Agent 机器绑定 API。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAgentBindAndCurrent 验证登录后可以绑定并查询当前机器。
func TestAgentBindAndCurrent(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "agent@example.com")

	bindReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-test","agent_version":"0.1.0","local_port":95271}`),
	)
	bindReq.Header.Set("Authorization", "Bearer "+token)
	bindResp := httptest.NewRecorder()
	routes.ServeHTTP(bindResp, bindReq)

	if bindResp.Code != http.StatusOK {
		t.Fatalf("bind status = %d, body = %s", bindResp.Code, bindResp.Body.String())
	}

	currentReq := httptest.NewRequest(http.MethodGet, "/api/agents/current", nil)
	currentReq.Header.Set("Authorization", "Bearer "+token)
	currentResp := httptest.NewRecorder()
	routes.ServeHTTP(currentResp, currentReq)

	if currentResp.Code != http.StatusOK {
		t.Fatalf("current status = %d, body = %s", currentResp.Code, currentResp.Body.String())
	}

	var payload struct {
		Agent struct {
			MachineID string `json:"machine_id"`
		} `json:"agent"`
	}
	if err := json.NewDecoder(currentResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Agent.MachineID != "sha256-test" {
		t.Fatalf("machine_id = %q", payload.Agent.MachineID)
	}
}

// TestAgentBindRejectsAnotherMachine 验证同一账号不能绑定第二台电脑。
func TestAgentBindRejectsAnotherMachine(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "agent-conflict@example.com")

	firstReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-first","agent_version":"5.0.0","local_port":95271}`),
	)
	firstReq.Header.Set("Authorization", "Bearer "+token)
	firstResp := httptest.NewRecorder()
	routes.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first bind status = %d, body = %s", firstResp.Code, firstResp.Body.String())
	}

	secondReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-second","agent_version":"5.0.0","local_port":95272}`),
	)
	secondReq.Header.Set("Authorization", "Bearer "+token)
	secondResp := httptest.NewRecorder()
	routes.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusConflict {
		t.Fatalf("second bind status = %d, want %d, body = %s", secondResp.Code, http.StatusConflict, secondResp.Body.String())
	}
}

// TestAgentBindRejectsAnonymous 验证未登录请求不能绑定机器。
func TestAgentBindRejectsAnonymous(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/agents/bind", bytes.NewBufferString(`{"machine_id":"sha256-test"}`))
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("bind status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

// loginForTest 调用验证码登录接口，并返回可用于后续接口测试的 token。
func loginForTest(t *testing.T, routes http.Handler, email string) string {
	t.Helper()

	// 调用发送验证码接口，获取开发模式下返回的 debug_code。
	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"`+email+`"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)
	if sendResp.Code != http.StatusOK {
		t.Fatalf("send code status = %d, body = %s", sendResp.Code, sendResp.Body.String())
	}

	var sendPayload struct {
		DebugCode string `json:"debug_code"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&sendPayload); err != nil {
		t.Fatal(err)
	}

	// 调用登录接口，使用验证码换取当前测试会话 token。
	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		bytes.NewBufferString(`{"email":"`+email+`","code":"`+sendPayload.DebugCode+`"}`),
	)
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	var loginPayload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginPayload); err != nil {
		t.Fatal(err)
	}
	return loginPayload.AccessToken
}
