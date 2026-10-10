// 文件作用：验证用户管理的全库 AI 余额与成功充值统计、整数精度、负值、搜索隔离及访问权限，全部使用内存合成数据。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdminUserFinancialStats 验证所有当前用户余额减去每人 0.50 元，充值只累计成功 AI 订单且不受 500 条列表上限影响。
func TestAdminUserFinancialStats(t *testing.T) {
	subscriptions, wallet, payments := NewMemorySubscriptionStore(), NewMemoryAIWalletStore(), NewMemoryPaymentStore()
	for index, units := range []int64{10001, 4999, 0, -1256} {
		email := fmt.Sprintf("finance-%d@example.test", index)
		if _, err := subscriptions.UserSubscription(email); err != nil {
			t.Fatal(err)
		}
		if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: email, ChangeUnits: units}); err != nil {
			t.Fatal(err)
		}
	}
	// 没有当前用户记录的钱包不属于用户表余额汇总。
	if _, err := wallet.AdjustBalance(AIWalletRecord{UserEmail: "orphan@example.test", ChangeUnits: 999999}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 501; index++ {
		if _, err := payments.Create(PaymentOrder{OrderNo: fmt.Sprintf("paid-%d", index), OrderType: "ai_balance", Status: "paid", AmountCents: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for _, order := range []PaymentOrder{
		{OrderNo: "paid-discount", OrderType: "ai_balance", Status: "paid", AmountCents: 123, OriginalAmountCents: 9000, DiscountAmountCents: 8877},
		{OrderNo: "paid-refunded", OrderType: "ai_balance", Status: "paid", AmountCents: 321},
		{OrderNo: "membership", OrderType: "subscription", Status: "paid", AmountCents: 900000},
		{OrderNo: "pending", OrderType: "ai_balance", Status: "pending", AmountCents: 900000},
		{OrderNo: "cancelled", OrderType: "ai_balance", Status: "cancelled", AmountCents: 900000},
		{OrderNo: "failed", OrderType: "ai_balance", Status: "failed", AmountCents: 900000},
		{OrderNo: "expired", OrderType: "ai_balance", Status: "expired", AmountCents: 900000},
	} {
		if _, err := payments.Create(order); err != nil {
			t.Fatal(err)
		}
	}
	// 用户要求累计成功充值订单的原支付金额，退款记录不改变订单的成功状态或本统计口径。
	if _, err := payments.CreateRefund(PaymentRefund{RefundNo: "refund-1", OrderNo: "paid-refunded", AmountCents: 100, Status: "success"}); err != nil {
		t.Fatal(err)
	}
	stats, err := NewMemoryAdminUserStore(subscriptions, wallet, payments).Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalUserCount != 4 || stats.TotalAIBalanceUnits != 13744 || stats.SignupGiftDeductionUnits != 20000 || stats.RemainingAIBalanceUnits != -6256 || stats.AIRechargeAmountCents != 945 {
		t.Fatalf("incorrect global financial totals: %+v", stats)
	}
	if stats.TotalAIBalance != "1.3744" || stats.SignupGiftDeduction != "2.0000" || stats.RemainingAIBalance != "-0.6256" || stats.AIRechargeAmount != "9.45" {
		t.Fatalf("incorrect exact monetary labels: %+v", stats)
	}
	if got, err := wallet.BalanceUnits("finance-0@example.test"); err != nil || got != 10001 {
		t.Fatalf("statistics changed an actual wallet: balance=%d err=%v", got, err)
	}
}

// TestAdminUserFinancialStatsEmpty 验证空库或未初始化内存用户存储仍返回明确的零金额字符串。
func TestAdminUserFinancialStatsEmpty(t *testing.T) {
	for _, store := range []*MemoryAdminUserStore{nil, {}, NewMemoryAdminUserStore(NewMemorySubscriptionStore(), NewMemoryAIWalletStore(), NewMemoryPaymentStore())} {
		stats, err := store.Stats()
		if err != nil || stats.TotalUserCount != 0 || stats.RemainingAIBalance != "0.0000" || stats.AIRechargeAmount != "0.00" {
			t.Fatalf("unexpected empty financial totals: %+v err=%v", stats, err)
		}
	}
}

// TestAdminUserFinancialAmountPrecision 验证超出浮点安全整数范围的分金额和负零整数部分仍精确格式化。
func TestAdminUserFinancialAmountPrecision(t *testing.T) {
	for _, test := range []struct {
		cents int64
		want  string
	}{{0, "0.00"}, {1, "0.01"}, {-1, "-0.01"}, {9007199254740993, "90071992547409.93"}, {math.MaxInt64, "92233720368547758.07"}, {math.MinInt64, "-92233720368547758.08"}} {
		if got := adminStatsCentsToYuanString(test.cents); got != test.want {
			t.Fatalf("cents=%d got=%s want=%s", test.cents, got, test.want)
		}
	}
}

// financialFailureWallet 只在读取余额时返回合成故障，用于验证金额查询不能将失败伪装成零。
type financialFailureWallet struct{ AIWalletStore }

// BalanceUnits 返回合成钱包读取故障。
func (financialFailureWallet) BalanceUnits(string) (int64, error) {
	return 0, errors.New("synthetic wallet read failure")
}

// financialFailurePayments 只在读取订单时返回合成故障。
type financialFailurePayments struct{ PaymentStore }

// ListAll 返回合成订单读取故障。
func (financialFailurePayments) ListAll() ([]PaymentOrder, error) {
	return nil, errors.New("synthetic payment read failure")
}

// TestAdminUserFinancialStatsReadFailure 验证钱包和充值读取失败时返回错误，不展示误导性的零统计。
func TestAdminUserFinancialStatsReadFailure(t *testing.T) {
	subscriptions := NewMemorySubscriptionStore()
	if _, err := subscriptions.UserSubscription("read-failure@example.test"); err != nil {
		t.Fatal(err)
	}
	for _, store := range []*MemoryAdminUserStore{
		NewMemoryAdminUserStore(subscriptions, financialFailureWallet{}, NewMemoryPaymentStore()),
		NewMemoryAdminUserStore(subscriptions, NewMemoryAIWalletStore(), financialFailurePayments{}),
	} {
		if _, err := store.Stats(); err == nil {
			t.Fatal("expected financial statistics read failure")
		}
	}
}

// TestAdminUserFinancialStatsAPI 验证金额统计复用超管接口、与分页搜索无关，并且不会暴露到官网公开统计。
func TestAdminUserFinancialStatsAPI(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	adminToken := loginForTest(t, routes, "1224299352@qq.com")
	userToken := loginForTest(t, routes, "finance-api@example.com")
	loginForTest(t, routes, "finance-other@example.com")
	if _, err := server.adminUsers.aiWallet.AdjustBalance(AIWalletRecord{UserEmail: "finance-api@example.com", ChangeUnits: 12345}); err != nil {
		t.Fatal(err)
	}
	store := server.adminUsers.users.(*MemoryAdminUserStore)
	if _, err := store.payments.Create(PaymentOrder{OrderNo: "api-paid", OrderType: "ai_balance", Status: "paid", AmountCents: 789}); err != nil {
		t.Fatal(err)
	}
	want, err := store.Stats()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/users", "/api/admin/users?q=finance-api&page=1&page_size=1", "/api/admin/users?q=nobody-matches&page=99&page_size=1"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("admin statistics status=%d", resp.Code)
		}
		var payload struct {
			Stats AdminUserStats `json:"stats"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Stats != want {
			t.Fatalf("search/pagination changed global statistics: got=%+v want=%+v", payload.Stats, want)
		}
	}
	for _, test := range []struct {
		token string
		code  int
	}{{"", http.StatusUnauthorized}, {userToken, http.StatusForbidden}} {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		resp := httptest.NewRecorder()
		routes.ServeHTTP(resp, req)
		if resp.Code != test.code {
			t.Fatalf("restricted statistics status=%d want=%d", resp.Code, test.code)
		}
	}
	resp := httptest.NewRecorder()
	routes.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/public/stats/today", nil))
	var public map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"total_ai_balance", "remaining_ai_balance", "ai_recharge_amount", "total_ai_balance_units", "ai_recharge_amount_cents"} {
		if _, exists := public[key]; exists {
			t.Fatalf("public statistics exposed private field %s", key)
		}
	}
}
