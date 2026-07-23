// 本文件负责测试岗位运行完成状态同步和邮件提醒结果。
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		`{"status":"completed"}`,
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
	if notice.ScannedCount != 50 || notice.GreetedCount != 50 || notice.SkippedCount != 9 || notice.FailedCount != 0 {
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
