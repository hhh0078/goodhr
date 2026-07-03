// 本文件负责测试超级管理员用户管理和会员天数调整接口。
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdminUserManagementAdjustsSubscription 验证超管可以查看用户并调整会员天数。
func TestAdminUserManagementAdjustsSubscription(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	userToken := loginForTest(t, routes, "managed-user@example.com")
	adminToken := loginForTest(t, routes, "1224299352@qq.com")

	statusReq := httptest.NewRequest(http.MethodGet, "/api/subscription/status", nil)
	statusReq.Header.Set("Authorization", "Bearer "+userToken)
	statusResp := httptest.NewRecorder()
	routes.ServeHTTP(statusResp, statusReq)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", statusResp.Code, statusResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminToken)
	listResp := httptest.NewRecorder()
	routes.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list code = %d, body = %s", listResp.Code, listResp.Body.String())
	}

	var listPayload struct {
		Users []struct {
			Email string `json:"email"`
			Flow  struct {
				CurrentStep string `json:"current_step"`
				Completed   bool   `json:"completed"`
			} `json:"flow"`
		} `json:"users"`
		Total    int `json:"total"`
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Stats    struct {
			TodayRegisteredCount int `json:"today_registered_count"`
			AgentBindingCount    int `json:"agent_binding_count"`
		} `json:"stats"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Users) == 0 {
		t.Fatal("admin users list is empty")
	}
	if listPayload.Total == 0 || listPayload.Page != 1 || listPayload.PageSize == 0 {
		t.Fatalf("unexpected pagination payload: %+v", listPayload)
	}
	if listPayload.Users[0].Flow.CurrentStep == "" {
		t.Fatalf("missing user flow: %+v", listPayload.Users[0])
	}
	if listPayload.Stats.TodayRegisteredCount == 0 {
		t.Fatalf("today registered count = %d", listPayload.Stats.TodayRegisteredCount)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/admin/users?q=managed-user&page=1&page_size=1", nil)
	searchReq.Header.Set("Authorization", "Bearer "+adminToken)
	searchResp := httptest.NewRecorder()
	routes.ServeHTTP(searchResp, searchReq)
	if searchResp.Code != http.StatusOK {
		t.Fatalf("search code = %d, body = %s", searchResp.Code, searchResp.Body.String())
	}
	var searchPayload struct {
		Users []struct {
			Email string `json:"email"`
		} `json:"users"`
		Total    int `json:"total"`
		PageSize int `json:"page_size"`
	}
	if err := json.NewDecoder(searchResp.Body).Decode(&searchPayload); err != nil {
		t.Fatal(err)
	}
	if searchPayload.Total != 1 || searchPayload.PageSize != 1 || searchPayload.Users[0].Email != "managed-user@example.com" {
		t.Fatalf("unexpected search payload: %+v", searchPayload)
	}

	adjustReq := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewBufferString(`{"email":"managed-user@example.com","days":5,"reason":"测试补偿"}`))
	adjustReq.Header.Set("Authorization", "Bearer "+adminToken)
	adjustResp := httptest.NewRecorder()
	routes.ServeHTTP(adjustResp, adjustReq)
	if adjustResp.Code != http.StatusOK {
		t.Fatalf("adjust code = %d, body = %s", adjustResp.Code, adjustResp.Body.String())
	}

	var adjustPayload struct {
		Subscription struct {
			MemberType string `json:"member_type"`
		} `json:"subscription"`
	}
	if err := json.NewDecoder(adjustResp.Body).Decode(&adjustPayload); err != nil {
		t.Fatal(err)
	}
	if adjustPayload.Subscription.MemberType != defaultMemberType {
		t.Fatalf("member type = %q", adjustPayload.Subscription.MemberType)
	}

	negativeReq := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewBufferString(`{"email":"managed-user@example.com","days":-1,"reason":"测试扣减"}`))
	negativeReq.Header.Set("Authorization", "Bearer "+adminToken)
	negativeResp := httptest.NewRecorder()
	routes.ServeHTTP(negativeResp, negativeReq)
	if negativeResp.Code != http.StatusBadRequest {
		t.Fatalf("negative adjust code = %d, body = %s", negativeResp.Code, negativeResp.Body.String())
	}
}

// TestPublicTodayStats 验证官网公开统计接口无需登录即可读取。
func TestPublicTodayStats(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "public-stats@example.com")
	positionID := createPositionForTest(t, routes, token)

	bindReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-public","agent_version":"5.0.0","local_port":55271}`),
	)
	bindReq.Header.Set("Authorization", "Bearer "+token)
	bindResp := httptest.NewRecorder()
	routes.ServeHTTP(bindResp, bindReq)
	if bindResp.Code != http.StatusOK {
		t.Fatalf("bind code = %d, body = %s", bindResp.Code, bindResp.Body.String())
	}

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks",
		bytes.NewBufferString(`{"platform_id":"boss","platform_account_id":"platform_account_1","position_id":"`+positionID+`","mode":"keyword","match_limit":20}`),
	)
	createReq.Header.Set("Authorization", "Bearer "+token)
	createResp := httptest.NewRecorder()
	routes.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create task code = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var createPayload struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&createPayload); err != nil {
		t.Fatal(err)
	}
	processedReq := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks/"+createPayload.Task.ID+"/processed-resumes",
		bytes.NewBufferString(`{"count":12}`),
	)
	processedReq.Header.Set("Authorization", "Bearer "+token)
	processedResp := httptest.NewRecorder()
	routes.ServeHTTP(processedResp, processedReq)
	if processedResp.Code != http.StatusOK {
		t.Fatalf("processed code = %d, body = %s", processedResp.Code, processedResp.Body.String())
	}

	statsReq := httptest.NewRequest(http.MethodGet, "/api/public/stats/today", nil)
	statsResp := httptest.NewRecorder()
	routes.ServeHTTP(statsResp, statsReq)
	if statsResp.Code != http.StatusOK {
		t.Fatalf("stats code = %d, body = %s", statsResp.Code, statsResp.Body.String())
	}
	var payload struct {
		ProcessedResumeCount int `json:"processed_resume_count"`
		TodayRegisteredCount int `json:"today_registered_count"`
		AgentBindingCount    int `json:"agent_binding_count"`
	}
	if err := json.NewDecoder(statsResp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ProcessedResumeCount != 12 || payload.TodayRegisteredCount == 0 || payload.AgentBindingCount == 0 {
		t.Fatalf("unexpected public stats: %+v", payload)
	}
}

// TestAdminUserManagementUnbindsAgent 验证超管可以解除用户本地程序绑定。
func TestAdminUserManagementUnbindsAgent(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	userToken := loginForTest(t, routes, "agent-unbind@example.com")
	adminToken := loginForTest(t, routes, "1224299352@qq.com")

	bindReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-old","agent_version":"5.0.0","local_port":55271}`),
	)
	bindReq.Header.Set("Authorization", "Bearer "+userToken)
	bindResp := httptest.NewRecorder()
	routes.ServeHTTP(bindResp, bindReq)
	if bindResp.Code != http.StatusOK {
		t.Fatalf("bind code = %d, body = %s", bindResp.Code, bindResp.Body.String())
	}

	unbindReq := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/users/unbind-agent",
		bytes.NewBufferString(`{"email":"agent-unbind@example.com"}`),
	)
	unbindReq.Header.Set("Authorization", "Bearer "+adminToken)
	unbindResp := httptest.NewRecorder()
	routes.ServeHTTP(unbindResp, unbindReq)
	if unbindResp.Code != http.StatusOK {
		t.Fatalf("unbind code = %d, body = %s", unbindResp.Code, unbindResp.Body.String())
	}

	nextBindReq := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"sha256-new","agent_version":"5.0.0","local_port":55272}`),
	)
	nextBindReq.Header.Set("Authorization", "Bearer "+userToken)
	nextBindResp := httptest.NewRecorder()
	routes.ServeHTTP(nextBindResp, nextBindReq)
	if nextBindResp.Code != http.StatusOK {
		t.Fatalf("next bind code = %d, body = %s", nextBindResp.Code, nextBindResp.Body.String())
	}
}

// TestAdminUserManagementRejectsNormalUser 验证普通用户不能访问用户管理接口。
func TestAdminUserManagementRejectsNormalUser(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "normal-admin-users@example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("admin users status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}
