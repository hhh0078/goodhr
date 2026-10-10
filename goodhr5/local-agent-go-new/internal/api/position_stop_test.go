// Package api 文件作用：验证岗位没有本地任务历史时仍能认证停止云端岗位，并准确返回失败且不伪造任务记录。
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/config"
	"goodhr5/local-agent-go-new/internal/flow/lifecycle"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// TestLocalPositionStopWithoutHistory 验证实际兼容路由使用浏览器当前凭证，同步无历史岗位并保留云端认证、归属和写入错误。
func TestLocalPositionStopWithoutHistory(t *testing.T) {
	for _, test := range []struct {
		name        string
		requestBody string
		cloudStatus int
		cloudBody   string
		wantStatus  int
		wantCode    string
		calls       int
		repeats     int
	}{
		{name: "legacy_cloud_success", requestBody: `{"token":"browser-token"}`, cloudStatus: 200, cloudBody: `{"ok":true,"status":"stopped"}`, wantStatus: 200, calls: 2, repeats: 2},
		{name: "missing_token", requestBody: `{}`, wantStatus: 400, wantCode: "TOKEN_REQUIRED", repeats: 1},
		{name: "expired_session", requestBody: `{"token":"browser-token"}`, cloudStatus: 401, cloudBody: `{"error":"session is invalid or expired"}`, wantStatus: 401, wantCode: "POSITION_STOP_FAILED", calls: 1, repeats: 1},
		{name: "another_account_position", requestBody: `{"token":"browser-token"}`, cloudStatus: 404, cloudBody: `{"error":"position not found"}`, wantStatus: 404, wantCode: "POSITION_STOP_FAILED", calls: 1, repeats: 1},
		{name: "cloud_write_failure", requestBody: `{"token":"browser-token"}`, cloudStatus: 500, cloudBody: `{"error":"failed to stop position"}`, wantStatus: 500, wantCode: "POSITION_STOP_FAILED", calls: 1, repeats: 1},
		{name: "unconfirmed_cloud_success", requestBody: `{"token":"browser-token"}`, cloudStatus: 200, cloudBody: `{"ok":true}`, wantStatus: 502, wantCode: "CLOUD_STOP_NOT_CONFIRMED", calls: 1, repeats: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := storage.Open(filepath.Join(t.TempDir(), "agent.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cloudCalls := 0
			cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cloudCalls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/positions/position-no-history/stop" || r.Header.Get("Authorization") != "Bearer browser-token" {
					t.Errorf("停止请求必须使用当前凭证及专用岗位接口：%s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.cloudStatus)
				_, _ = w.Write([]byte(test.cloudBody))
			}))
			defer cloudServer.Close()
			cloudClient := cloud.New(cloudServer.URL)
			runner := lifecycle.New(nil, nil, nil, store, nil, nil, cloudClient, nil, nil)
			server := NewServer(config.Config{Host: "127.0.0.1", Port: 43129}, Dependencies{Runner: runner, Store: store, Cloud: cloudClient})
			for attempt := 0; attempt < test.repeats; attempt++ {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/v1/local/positions/position-no-history/stop", strings.NewReader(test.requestBody))
				request.Header.Set("Content-Type", "application/json")
				server.http.Handler.ServeHTTP(response, request)
				if response.Code != test.wantStatus {
					t.Fatalf("停止状态错误：got=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
				}
				var payload struct {
					OK    bool            `json:"ok"`
					Data  storage.TaskRun `json:"data"`
					Error errorBody       `json:"error"`
				}
				if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if test.wantStatus == http.StatusOK {
					if !payload.OK || payload.Data.PositionID != "position-no-history" || payload.Data.Status != "stopped" || payload.Data.TaskID != "" {
						t.Fatalf("无历史岗位必须确认停止且不伪造任务：%+v", payload)
					}
				} else if payload.OK || payload.Error.Code != test.wantCode || strings.Contains(payload.Error.Message, "sql: no rows") {
					t.Fatalf("停止失败的错误类别不准确：%+v", payload.Error)
				}
			}
			if cloudCalls != test.calls {
				t.Fatalf("云端调用次数不符合授权要求：got=%d want=%d", cloudCalls, test.calls)
			}
			if _, err := store.LatestTaskForPosition(context.Background(), "position-no-history"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("停止接口不应生成本地任务历史：%v", err)
			}
		})
	}
}
