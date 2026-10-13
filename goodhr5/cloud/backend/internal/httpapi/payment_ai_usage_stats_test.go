// 本文件验证支付记录的本月 AI 消费统计、北京时间边界、账本精度和超级管理员权限。
package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAIWalletUsageUnitsBetween 验证月初北京时间边界、全部用户和所有模型的扣费汇总，并排除其他资金变动。
func TestAIWalletUsageUnitsBetween(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, chinaLocation())
	end := start.AddDate(0, 1, 0)
	wallet := NewMemoryAIWalletStore()
	wallet.records = []AIWalletRecord{
		{UserEmail: "first@example.test", ModelID: "qwen3.5-plus", Category: "ai_usage", ChangeUnits: -12, CreatedAt: start.UTC()},
		{UserEmail: "second@example.test", ModelID: "qwen3.8-flash", Category: "ai_usage", ChangeUnits: -23, CreatedAt: end.Add(-time.Nanosecond)},
		{Category: "ai_usage", ChangeUnits: -999, CreatedAt: start.Add(-time.Nanosecond)},
		{Category: "ai_usage", ChangeUnits: -999, CreatedAt: end},
		{Category: "ai_usage", ChangeUnits: 999, CreatedAt: start},
		{Category: "ai_usage", ChangeUnits: 0, CreatedAt: start},
		{Category: "recharge", ChangeUnits: 100000, CreatedAt: start},
		{Category: "signup_bonus", ChangeUnits: 5000, CreatedAt: start},
		{Category: "refund", ChangeUnits: -100000, CreatedAt: start},
		{Category: "admin_adjust", ChangeUnits: -5000, CreatedAt: start},
	}
	if got, err := wallet.UsageUnitsBetween(start, end); err != nil || got != 35 {
		t.Fatalf("usage units = %d, err = %v, want 35", got, err)
	}
	if got, err := wallet.UsageUnitsBetween(end, end.AddDate(0, 1, 0)); err != nil || got != 999 {
		t.Fatalf("next month units = %d, err = %v, want 999", got, err)
	}
	if got, err := wallet.UsageUnitsBetween(start, start); err != nil || got != 0 {
		t.Fatalf("empty interval units = %d, err = %v, want 0", got, err)
	}
	if len(wallet.records) != 10 || len(wallet.balances) != 0 {
		t.Fatal("统计不应修改流水或用户余额")
	}
}

// TestAIWalletUsageUnitsBetweenOverflow 验证无法表示的消费总额会明确报错，避免显示负数或错误金额。
func TestAIWalletUsageUnitsBetweenOverflow(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, chinaLocation())
	for _, changes := range [][]int64{{math.MinInt64}, {-math.MaxInt64, -1}} {
		wallet := NewMemoryAIWalletStore()
		for _, change := range changes {
			wallet.records = append(wallet.records, AIWalletRecord{Category: "ai_usage", ChangeUnits: change, CreatedAt: start})
		}
		if _, err := wallet.UsageUnitsBetween(start, start.Add(time.Hour)); err == nil {
			t.Fatal("超出整数范围应返回错误")
		}
	}
}

// TestAdminPaymentMonthlyAIConsumption 验证现有超管支付接口返回全局月消费，保留四位小数且与订单列表无关。
func TestAdminPaymentMonthlyAIConsumption(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	userToken := loginForTest(t, routes, "monthly-ai@example.com")
	wallet := server.payments.aiWallet.(*MemoryAIWalletStore)
	now := time.Now().In(chinaLocation())
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	wallet.records = append(wallet.records,
		AIWalletRecord{UserEmail: "first@example.test", Category: "ai_usage", ChangeUnits: -1234567, CreatedAt: start},
		AIWalletRecord{UserEmail: "second@example.test", Category: "ai_usage", ChangeUnits: -40260, CreatedAt: start},
		AIWalletRecord{Category: "ai_usage", ChangeUnits: -999999, CreatedAt: start.Add(-time.Nanosecond)},
		AIWalletRecord{Category: "ai_usage", ChangeUnits: -999999, CreatedAt: now.Add(time.Hour)},
		AIWalletRecord{Category: "refund", ChangeUnits: -999999, CreatedAt: start},
	)
	// 流水数量超过支付列表上限，也必须全部参与统计。
	for index := 0; index < 501; index++ {
		wallet.records = append(wallet.records, AIWalletRecord{Category: "ai_usage", ChangeUnits: -1, CreatedAt: start})
	}
	for _, item := range []struct {
		token string
		want  int
	}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}, {adminToken, http.StatusOK}} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/payment/orders?page_size=1&q=nobody", nil)
		if item.token != "" {
			req.Header.Set("Authorization", "Bearer "+item.token)
		}
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != item.want {
			t.Fatalf("status = %d, want %d, body = %s", resp.Code, item.want, resp.Body.String())
		}
		if item.want != http.StatusOK {
			if strings.Contains(resp.Body.String(), "monthly_ai_consumption") {
				t.Fatal("消费统计不应泄露给非超级管理员")
			}
			continue
		}
		var payload struct {
			Orders []PaymentOrder `json:"orders"`
			Stats  struct {
				Month       string `json:"month"`
				Timezone    string `json:"timezone"`
				Consumption string `json:"monthly_ai_consumption"`
				Units       int64  `json:"monthly_ai_consumption_units"`
			} `json:"stats"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Stats.Month != now.Format("2006-01") || payload.Stats.Timezone != chinaTimezoneName || payload.Stats.Units != 1275328 || payload.Stats.Consumption != "127.5328" || len(payload.Orders) != 0 {
			t.Fatalf("unexpected payment statistics: %+v", payload)
		}
	}
}

// monthlyUsageFailureWallet 提供消费统计故障，其他钱包方法仍使用原有实现。
type monthlyUsageFailureWallet struct{ AIWalletStore }

// UsageUnitsBetween 返回合成查询故障，验证接口不能把读取失败显示为零。
func (monthlyUsageFailureWallet) UsageUnitsBetween(time.Time, time.Time) (int64, error) {
	return 0, errors.New("synthetic monthly usage failure")
}

// TestAdminPaymentMonthlyAIConsumptionEmptyAndFailure 验证空账本的零金额以及读取故障和缺失钱包的明确错误。
func TestAdminPaymentMonthlyAIConsumptionEmptyAndFailure(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	token := loginForTest(t, routes, "1224299352@qq.com")
	for _, item := range []struct {
		wallet AIWalletStore
		want   int
	}{{NewMemoryAIWalletStore(), http.StatusOK}, {monthlyUsageFailureWallet{server.payments.aiWallet}, http.StatusInternalServerError}, {nil, http.StatusInternalServerError}} {
		server.payments.aiWallet = item.wallet
		req := httptest.NewRequest(http.MethodGet, "/api/admin/payment/orders", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != item.want {
			t.Fatalf("status = %d, want %d, body = %s", resp.Code, item.want, resp.Body.String())
		}
		if item.want == http.StatusOK && !strings.Contains(resp.Body.String(), `"monthly_ai_consumption":"0.0000"`) {
			t.Fatalf("空账本应显示明确零金额: %s", resp.Body.String())
		}
		if item.want != http.StatusOK && strings.Contains(resp.Body.String(), "monthly_ai_consumption") {
			t.Fatal("查询失败不能返回误导性的零消费统计")
		}
	}
}
