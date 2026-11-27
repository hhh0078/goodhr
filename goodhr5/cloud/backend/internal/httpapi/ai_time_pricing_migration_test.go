// Package httpapi 本文件验证套餐调价只影响新订单，旧订单、已购期限、999元额度及其他套餐配置均保留。
package httpapi

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAITimePriceMigrationPostgres 在独立测试schema执行真实调价迁移，并核对重复执行和旧权益。
func TestAITimePriceMigrationPostgres(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	aiRechargeApplyMigration(t, db, "0129_ai_time_packages.sql")
	wallet := NewPostgresAIWalletStore(db)
	store := NewPostgresAITimeStore(db)
	wallet.timePackages = store
	f := aiTimeFixture{wallet, store, NewPostgresPaymentStore(db), "price-change@example.test"}
	order := f.makeTimeOrder(t, "old-time-purchase")
	now := time.Now().Truncate(time.Microsecond)
	account, err := store.Grant(order, now)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := f.orders.Create(PaymentOrder{OrderNo: "old-pending-time", OrderType: "ai_time", UserEmail: f.email, PlanID: "monthly", PlanName: "包月套餐", DurationDays: 30, AmountCents: 4000, Status: "pending", PaymentProvider: wechatPayProviderName})
	if err != nil {
		t.Fatal(err)
	}
	var before string
	if err = db.QueryRow(`SELECT config_value::text FROM system_configs WHERE config_key='system.ai_time_plans'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var expected []map[string]any
	if err = json.Unmarshal([]byte(before), &expected); err != nil {
		t.Fatal(err)
	}
	prices := map[string]float64{"monthly": 6900, "quarterly": 17900, "yearly": 54900}
	for _, plan := range expected {
		plan["price_cents"] = prices[plan["id"].(string)]
	}
	expectedJSON, _ := json.Marshal(expected)
	var updated string
	if err = db.QueryRow(`SELECT updated_at::text FROM system_configs WHERE config_key='system.ai_time_plans'`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		aiRechargeApplyMigration(t, db, "0131_ai_time_package_prices.sql")
	}
	var matches bool
	if err = db.QueryRow(`SELECT config_value=$1::jsonb AND updated_at::text=$2 FROM system_configs WHERE config_key='system.ai_time_plans'`, string(expectedJSON), updated).Scan(&matches); err != nil || !matches {
		t.Fatal("套餐的额度、天数或原更新时间被改变", err)
	}
	current, err := store.Current(f.email, time.Now())
	if err != nil || current.ID != account.ID || current.BalanceUnits != account.BalanceUnits || !current.ExpiresAt.Equal(account.ExpiresAt) {
		t.Fatalf("已购权益被调价改变: %+v %+v %v", account, current, err)
	}
	saved, err := f.orders.ByOrderNo(pending.OrderNo)
	if err != nil || saved.AmountCents != 4000 || saved.DurationDays != 30 {
		t.Fatal("待支付订单不能跟随调价", err)
	}
	config := NewPostgresSystemConfigStore(db)
	plans, err := loadAITimePlans(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans {
		if plan.PriceCents != int64(prices[plan.ID]) || plan.BalanceCents != 99900 {
			t.Fatalf("新价格或记账额度错误: %+v", plan)
		}
	}
	// 空套餐数组和其他结构不能因调价变成NULL，其他业务配置也必须保持。
	if _, err = db.Exec(`UPDATE system_configs SET config_value='[]'::jsonb WHERE config_key='system.ai_time_plans'`); err != nil {
		t.Fatal(err)
	}
	aiRechargeApplyMigration(t, db, "0131_ai_time_package_prices.sql")
	if err = db.QueryRow(`SELECT config_value='[]'::jsonb FROM system_configs WHERE config_key='system.ai_time_plans'`).Scan(&matches); err != nil || !matches {
		t.Fatal("空套餐配置处理错误", err)
	}
}
