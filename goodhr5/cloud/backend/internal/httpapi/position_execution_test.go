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

	resp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting"}`)
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
	positionID := createPositionWithConfigForTest(t, routes, token, "会员岗位", `{"detail_mode":"ai"}`)
	if _, err := server.positionExecution.subscriptions.AdjustSubscriptionDays(email, defaultMemberType, -10); err != nil {
		t.Fatal(err)
	}

	resp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+positionID+"/start", `{"task_type":"greeting"}`)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, resp); code != "SUBSCRIPTION_REQUIRED" {
		t.Fatalf("error code = %s", code)
	}
}

// TestPositionStartAllowsOnlyOneRunningPosition 验证同一账号的启动检查与 running 更新在一次申请中完成。
func TestPositionStartAllowsOnlyOneRunningPosition(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "position-conflict@example.com")
	firstID := createPositionWithConfigForTest(t, routes, token, "第一个岗位", `{"mode_default":"keyword"}`)
	secondID := createPositionWithConfigForTest(t, routes, token, "第二个岗位", `{"mode_default":"keyword"}`)

	firstResp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+firstID+"/start", `{"task_type":"greeting"}`)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first start status = %d, body = %s", firstResp.Code, firstResp.Body.String())
	}
	secondResp := postPositionExecutionForTest(t, routes, token, "/api/positions/"+secondID+"/start", `{"task_type":"greeting"}`)
	if secondResp.Code != http.StatusConflict {
		t.Fatalf("second start status = %d, body = %s", secondResp.Code, secondResp.Body.String())
	}
	if code := positionStartErrorCodeForTest(t, secondResp); code != "POSITION_TASK_CONFLICT" {
		t.Fatalf("error code = %s", code)
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
		`{"scanned_count":50,"greeted_count":50,"skipped_count":9,"failed_count":0}`,
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
	if notice.TodayGreetedCount != 50 || notice.RunGreetedCount != 12 || notice.RunSkippedCount != 3 {
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
