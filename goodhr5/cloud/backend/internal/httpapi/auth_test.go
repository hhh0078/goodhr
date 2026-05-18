package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthCodeLogin(t *testing.T) {
	server := NewServer()
	routes := server.Routes()

	sendBody := bytes.NewBufferString(`{"email":"User@Example.com"}`)
	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", sendBody)
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	if sendResp.Code != http.StatusOK {
		t.Fatalf("send code status = %d, body = %s", sendResp.Code, sendResp.Body.String())
	}

	var sendPayload struct {
		DebugCode string `json:"debug_code"`
		Email     string `json:"email"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&sendPayload); err != nil {
		t.Fatal(err)
	}
	if sendPayload.Email != "user@example.com" {
		t.Fatalf("email was not normalized: %q", sendPayload.Email)
	}
	if len(sendPayload.DebugCode) != 4 {
		t.Fatalf("debug code length = %d", len(sendPayload.DebugCode))
	}

	loginBody := bytes.NewBufferString(`{"email":"user@example.com","code":"` + sendPayload.DebugCode + `"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", loginBody)
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}
}

func TestAuthRejectsWrongCode(t *testing.T) {
	server := NewServer()
	routes := server.Routes()

	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"user@example.com"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"email":"user@example.com","code":"0000"}`))
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusUnauthorized)
	}
}
