// 本文件负责测试岗位运行完成状态同步和邮件提醒结果。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

const positionTestMachineID = "goodhr-device-v1-position-test"

// bindPositionDeviceForTest 为岗位启动测试绑定一台稳定设备。
func bindPositionDeviceForTest(t *testing.T, routes http.Handler, token string) {
	t.Helper()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/agents/bind",
		bytes.NewBufferString(`{"machine_id":"`+positionTestMachineID+`","agent_version":"6","local_port":43129}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bind device status = %d, body = %s", response.Code, response.Body.String())
	}
}

// postPositionExecutionForTest 调用岗位运行子接口并返回响应。
func postPositionExecutionForTest(t *testing.T, routes http.Handler, token, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	return resp
}

// createPositionWithConfigForTest 创建带指定公共配置的测试岗位。
func createPositionWithConfigForTest(t *testing.T, routes http.Handler, token, name, commonConfig string) string {
	t.Helper()
	body := `{"name":` + strconv.Quote(name) + `,"keywords":["招聘"],"common_config":` + commonConfig + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/positions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("create position status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Position struct {
			ID string `json:"id"`
		} `json:"position"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Position.ID
}

// positionStartErrorCodeForTest 读取岗位启动失败响应里的稳定错误码。
func positionStartErrorCodeForTest(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Error.Code
}

// TestPositionStartRejectsInsufficientAIBalance 验证 AI 岗位余额不足时不会写入运行状态。
func TestPositionStartRejectsInsufficientAIBalance(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-ai-balance@example.com"
	token := loginForTest(t, routes, email)
	bindPositionDeviceForTest(t, routes, token)
	positionID := createPositionWithConfigForTest(t, routes, token, "AI 岗位", `{"mode_default":"ai"}`)
	wallet := server.positionExecution.aiWallet
	balance, err := wallet.BalanceUnits(email)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wallet.AdjustBalance(AIWalletRecord{
		UserEmail: email, ChangeUnits: -balance, Category: "test", Reason: "测试清空余额",
	}); err != nil {
		t.Fatal(err)
	}

	resp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if resp.Code != http.StatusPaymentRequired {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, resp); code != "AI_BALANCE_INSUFFICIENT" {
		t.Fatalf("error code = %s", code)
	}
}

// TestPositionStartRejectsExpiredSubscription 验证 AI 岗位会员到期时不会启动。
func TestPositionStartRejectsExpiredSubscription(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-subscription@example.com"
	token := loginForTest(t, routes, email)
	bindPositionDeviceForTest(t, routes, token)
	positionID := createPositionWithConfigForTest(t, routes, token, "会员岗位", `{"detail_mode":"ai"}`)
	if _, err := server.positionExecution.subscriptions.AdjustSubscriptionDays(email, defaultMemberType, -10); err != nil {
		t.Fatal(err)
	}

	resp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, resp); code != "SUBSCRIPTION_REQUIRED" {
		t.Fatalf("error code = %s", code)
	}
}

// TestPositionStartRequiresMaxForAutoReply 验证 Plus 有效时仍不能启动自动回复。
func TestPositionStartRequiresMaxForAutoReply(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "position-auto-reply@example.com"
	token := loginForTest(t, routes, email)
	bindPositionDeviceForTest(t, routes, token)
	positionID := createPositionWithConfigForTest(t, routes, token, "自动回复岗位", `{"mode_default":"keyword"}`)
	if _, err := server.positionExecution.subscriptions.AdjustSubscriptionDays(email, memberTypePlus, 30); err != nil {
		t.Fatal(err)
	}

	resp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"auto_reply","machine_id":"`+positionTestMachineID+`"}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, resp); code != "AUTO_REPLY_MAX_REQUIRED" {
		t.Fatalf("error code = %s", code)
	}
}

// TestPositionStartAllowsOnlyOneRunningPosition 验证同一账号的启动检查与 running 更新在一次申请中完成。
func TestPositionStartAllowsOnlyOneRunningPosition(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-conflict@example.com")
	bindPositionDeviceForTest(t, routes, token)
	firstID := createPositionWithConfigForTest(t, routes, token, "第一个岗位", `{"mode_default":"keyword"}`)
	secondID := createPositionWithConfigForTest(t, routes, token, "第二个岗位", `{"mode_default":"keyword"}`)

	firstResp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+firstID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first start status = %d, body = %s", firstResp.Code, firstResp.Body.String())
	}
	secondResp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+secondID+"/start", `{"task_type":"greeting","machine_id":"`+positionTestMachineID+`"}`)
	if secondResp.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, body = %s", secondResp.Code, secondResp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, secondResp); code != "POSITION_TASK_CONFLICT" {
		t.Fatalf("error code = %s", code)
	}
}

// TestPositionStartRequiresBoundStableDevice 验证没有稳定设备绑定时云端不会允许岗位启动。
func TestPositionStartRequiresBoundStableDevice(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-device-required@example.com")
	positionID := createPositionWithConfigForTest(t, routes, token, "设备校验岗位", `{"mode_default":"keyword"}`)
	response := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting"}`)
	if response.Code != http.StatusForbidden || positionStartErrorCodeForTest(t, response) != "DEVICE_BINDING_REQUIRED" {
		t.Fatalf("start status = %d, body = %s", response.Code, response.Body.String())
	}
}

// TestPositionStartAllowsAnyDeviceOwnedByAccount 验证同一账号较早绑定的电脑也可以启动岗位。
func TestPositionStartAllowsAnyDeviceOwnedByAccount(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-multi-device@example.com")
	for _, machineID := range []string{"goodhr-device-v1-earlier", "goodhr-device-v1-latest"} {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/agents/bind",
			bytes.NewBufferString(`{"machine_id":"`+machineID+`","agent_version":"6","local_port":43129}`),
		)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("bind %s status = %d, body = %s", machineID, response.Code, response.Body.String())
		}
	}
	positionID := createPositionWithConfigForTest(t, routes, token, "多设备岗位", `{"mode_default":"keyword"}`)
	response := postPositionExecutionForTest(
		t, routes, token, "/api/positions/"+positionID+"/start",
		`{"task_type":"greeting","machine_id":"goodhr-device-v1-earlier"}`,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", response.Code, response.Body.String())
	}
}

// TestSyncCompletedStatusSendsNoticeWithCurrentCounts 验证完成状态会发送包含最新统计的邮件，并返回明确的发送结果。
func TestSyncCompletedStatusSendsNoticeWithCurrentCounts(t *testing.T) {
	server := mustNewServer(t)
	mailer := &recordingMailer{}
	server.positionExecution.mailer = mailer
	routes := server.Routes()
	token := loginForTest(t, routes, "position-completed@example.com")
	positionID := createPositionForTest(t, routes, token)

	countsResp := postPositionExecutionForTest(
		t,
		routes,
		token,
		"/api/positions/"+positionID+"/counts",
		`{"scanned_count":50,"skipped_count":9,"failed_count":0}`,
	)
	if countsResp.Code != http.StatusOK {
		t.Fatalf("counts status = %d, body = %s", countsResp.Code, countsResp.Body.String())
	}

	completedResp := postPositionExecutionForTest(
		t,
		routes,
		token,
		"/api/positions/"+positionID+"/status",
		`{"status":"completed","run_greeted_count":12,"run_skipped_count":3}`,
	)
	if completedResp.Code != http.StatusOK {
		t.Fatalf("completed status = %d, body = %s", completedResp.Code, completedResp.Body.String())
	}
	var completedPayload struct {
		NoticeSent bool `json:"notice_sent"`
	}
	if err := json.NewDecoder(completedResp.Body).Decode(&completedPayload); err != nil {
		t.Fatal(err)
	}
	if !completedPayload.NoticeSent {
		t.Fatal("notice_sent = false, want true")
	}
	if len(mailer.positionNotices) != 1 {
		t.Fatalf("position notices = %d, want 1", len(mailer.positionNotices))
	}
	notice := mailer.positionNotices[0].notice
	if notice.TodayGreetedCount != 12 || notice.RunGreetedCount != 12 || notice.RunSkippedCount != 3 {
		t.Fatalf("notice counts = %+v", notice)
	}

	repeatedResp := postPositionExecutionForTest(
		t,
		routes,
		token,
		"/api/positions/"+positionID+"/status",
		`{"status":"completed"}`,
	)
	if repeatedResp.Code != http.StatusOK {
		t.Fatalf("repeated completed status = %d, body = %s", repeatedResp.Code, repeatedResp.Body.String())
	}
	if len(mailer.positionNotices) != 1 {
		t.Fatalf("repeated position notices = %d, want 1", len(mailer.positionNotices))
	}
}

// TestSyncCompletedStatusCanRetryAfterMailFailure 验证邮件失败时不会提前写入完成状态，重试后可以正常发送。
func TestSyncCompletedStatusCanRetryAfterMailFailure(t *testing.T) {
	server := mustNewServer(t)
	mailer := &recordingMailer{positionStatusErr: errors.New("smtp unavailable")}
	server.positionExecution.mailer = mailer
	routes := server.Routes()
	token := loginForTest(t, routes, "position-completed-retry@example.com")
	positionID := createPositionForTest(t, routes, token)

	failedResp := postPositionExecutionForTest(
		t,
		routes,
		token,
		"/api/positions/"+positionID+"/status",
		`{"status":"completed"}`,
	)
	if failedResp.Code != http.StatusBadGateway {
		t.Fatalf("failed completed status = %d, body = %s", failedResp.Code, failedResp.Body.String())
	}

	mailer.positionStatusErr = nil
	retryResp := postPositionExecutionForTest(
		t,
		routes,
		token,
		"/api/positions/"+positionID+"/status",
		`{"status":"completed"}`,
	)
	if retryResp.Code != http.StatusOK {
		t.Fatalf("retry completed status = %d, body = %s", retryResp.Code, retryResp.Body.String())
	}
	if len(mailer.positionNotices) != 1 {
		t.Fatalf("position notices after retry = %d, want 1", len(mailer.positionNotices))
	}
}
