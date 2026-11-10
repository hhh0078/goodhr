// 本文件用合成数据验证统计鉴权、北京时间边界、日期筛选和读取失败，不访问生产数据。
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// syntheticStatisticsStore 返回合成汇总或合成错误，不提供用户明细。
type syntheticStatisticsStore struct{ err error }

// Summary 实现隔离测试存储并返回精确金额。
func (s syntheticStatisticsStore) Summary(_ context.Context, start, end, now time.Time) (SystemStatistics, error) {
	return SystemStatistics{StartDate: start.Format(time.DateOnly), EndDate: end.AddDate(0, 0, -1).Format(time.DateOnly), AsOf: now, Amounts: map[string]string{"consumption": "0.0001"}}, s.err
}

// TestSystemStatisticsRange 验证北京时间跨零点、默认一天及非法和倒置范围。
func TestSystemStatisticsRange(t *testing.T) {
	now := time.Date(2026, 11, 10, 16, 5, 0, 0, time.UTC)
	for _, test := range []struct {
		query, start, end string
		invalid           bool
	}{
		{"", "2026-11-11", "2026-11-12", false},
		{"?period=yesterday", "2026-11-10", "2026-11-11", false},
		{"?period=last7", "2026-11-05", "2026-11-12", false},
		{"?period=last30", "2026-10-13", "2026-11-12", false},
		{"?period=custom&start_date=2026-10-31&end_date=2026-11-01", "2026-10-31", "2026-11-02", false},
		{"?period=custom&start_date=2026-11-10&end_date=2026-11-09", "", "", true},
		{"?period=custom&start_date=2026-11-10&end_date=2026-11-12", "", "", true},
		{"?period=custom&start_date=2025-01-01&end_date=2026-11-10", "", "", true},
		{"?period=custom&start_date=2026-02-30&end_date=2026-03-01", "", "", true},
		{"?period=other", "", "", true},
	} {
		start, end, err := statisticsRange(httptest.NewRequest(http.MethodGet, "/api/admin/statistics"+test.query, nil), now)
		if (err != nil) != test.invalid {
			t.Fatalf("%s: %v", test.query, err)
		}
		if !test.invalid && (start.Format(time.DateOnly) != test.start || end.Format(time.DateOnly) != test.end) {
			t.Fatalf("%s: got %s..%s", test.query, start, end)
		}
	}
}

// TestSystemStatisticsAuthorization 验证全站统计只允许超管，日期之外的个人筛选和读取失败均明确拒绝。
func TestSystemStatisticsAuthorization(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	admin := loginForTest(t, routes, "1224299352@qq.com")
	user := loginForTest(t, routes, "statistics@example.com")
	service := server.systemStatistics
	service.store = syntheticStatisticsStore{}
	if err := server.auth.store.SaveSession("statistics-extension", Session{Kind: extensionSessionKind, Email: "1224299352@qq.com", ExpiresAt: time.Now().Add(time.Hour)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		token, path string
		status      int
	}{
		{"", "/api/admin/statistics", http.StatusUnauthorized},
		{user, "/api/admin/statistics", http.StatusForbidden},
		{"statistics-extension", "/api/admin/statistics", http.StatusForbidden},
		{admin, "/api/admin/statistics", http.StatusOK},
		{admin, "/api/admin/statistics?email=someone@example.test", http.StatusBadRequest},
		{admin, "/api/admin/statistics?period=today&period=last7", http.StatusBadRequest},
		{admin, "/api/admin/statistics?period=custom", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != test.status {
			t.Fatalf("%s status=%d body=%s", test.path, resp.Code, resp.Body)
		}
		if !strings.Contains(resp.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("statistics response can be cached")
		}
		if resp.Code == http.StatusOK && !strings.Contains(resp.Body.String(), `"consumption":"0.0001"`) {
			t.Fatal("exact money lost")
		}
	}
	for _, store := range []systemStatisticsStore{nil, syntheticStatisticsStore{err: errors.New("synthetic failure")}} {
		service.store = store
		req := httptest.NewRequest(http.MethodGet, "/api/admin/statistics", nil)
		req.Header.Set("Authorization", "Bearer "+admin)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code < 500 || strings.Contains(resp.Body.String(), `"stats"`) {
			t.Fatalf("read failure became zero statistics: %s", resp.Body)
		}
	}
}
