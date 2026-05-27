// 本文件负责测试邀请奖励和会员激活码接口。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestInvitationBindOnLogin 验证邀请链接注册后会记录邀请关系。
func TestInvitationBindOnLogin(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	inviterToken := loginForTest(t, routes, "inviter@example.com")

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/invitations/summary", nil)
	summaryReq.Header.Set("Authorization", "Bearer "+inviterToken)
	summaryResp := httptest.NewRecorder()
	routes.ServeHTTP(summaryResp, summaryReq)
	if summaryResp.Code != http.StatusOK {
		t.Fatalf("summary status = %d, body = %s", summaryResp.Code, summaryResp.Body.String())
	}
	var summaryPayload struct {
		InviteID string `json:"invite_id"`
	}
	if err := json.NewDecoder(summaryResp.Body).Decode(&summaryPayload); err != nil {
		t.Fatal(err)
	}
	if summaryPayload.InviteID == "" {
		t.Fatal("invite id is empty")
	}

	code := sendLoginCodeForTest(t, routes, "invitee@example.com")
	loginReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/login",
		bytes.NewBufferString(`{"email":"invitee@example.com","code":"`+code+`","inviter_id":"`+summaryPayload.InviteID+`"}`),
	)
	loginResp := httptest.NewRecorder()
	routes.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("invitee login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/invitations/summary", nil)
	listReq.Header.Set("Authorization", "Bearer "+inviterToken)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		Invitees []struct {
			Email string `json:"email"`
		} `json:"invitees"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Invitees) != 1 || listPayload.Invitees[0].Email != "invitee@example.com" {
		t.Fatalf("unexpected invitees: %+v", listPayload.Invitees)
	}
}

// TestActivationCodeCreateAndRedeem 验证超管生成激活码后普通用户可以兑换。
func TestActivationCodeCreateAndRedeem(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	userToken := loginForTest(t, routes, "activation-user@example.com")

	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/activation-codes", bytes.NewBufferString(`{"days":7,"remark":"测试","count":2}`))
	createReq.Header.Set("Authorization", "Bearer "+adminToken)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create code status = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Codes []struct {
			Code string `json:"code"`
			Days int    `json:"days"`
		} `json:"codes"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	if len(createPayload.Codes) != 2 || createPayload.Codes[0].Days != 7 {
		t.Fatalf("unexpected codes: %+v", createPayload.Codes)
	}

	redeemReq := httptest.NewRequest(http.MethodPost, "/api/activation-codes/redeem", bytes.NewBufferString(`{"code":"`+createPayload.Codes[0].Code+`"}`))
	redeemReq.Header.Set("Authorization", "Bearer "+userToken)
	redeemResp := httptest.NewRecorder()
	routes.ServeHTTP(redeemResp, redeemReq)
	if redeemResp.Code != http.StatusOK {
		t.Fatalf("redeem status = %d, body = %s", redeemResp.Code, redeemResp.Body.String())
	}
}

// sendLoginCodeForTest 发送验证码并返回开发模式验证码。
func sendLoginCodeForTest(t *testing.T, routes http.Handler, email string) string {
	t.Helper()
	sendReq := httptest.NewRequest(http.MethodPost, "/api/auth/send-code", bytes.NewBufferString(`{"email":"`+email+`"}`))
	sendResp := httptest.NewRecorder()
	routes.ServeHTTP(sendResp, sendReq)
	if sendResp.Code != http.StatusOK {
		t.Fatalf("send code status = %d, body = %s", sendResp.Code, sendResp.Body.String())
	}
	var payload struct {
		DebugCode string `json:"debug_code"`
	}
	if err := json.NewDecoder(sendResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.DebugCode
}
