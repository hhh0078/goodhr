// Package httpapi 本文件验证时间套餐扣费、资金隔离、续费幂等、跨到期请求及注册配置的真实业务边界。
package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// aiTimeFixture 复用内存和 PostgreSQL 钱包运行相同资金规则，订单通过原有存储创建。
type aiTimeFixture struct {
	wallet AIWalletStore
	store  AITimeStore
	orders PaymentStore
	email  string
}

// makeTimeOrder 建立已支付订单和购买时额度快照，不调用真实支付渠道。
func (f aiTimeFixture) makeTimeOrder(t *testing.T, id string) PaymentOrder {
	t.Helper()
	order, err := f.orders.Create(PaymentOrder{OrderNo: id, OrderType: "ai_time", UserEmail: f.email, PlanID: "monthly", PlanName: "包月套餐", DurationDays: 30, AmountCents: 4000, Status: "paid", PaymentProvider: wechatPayProviderName})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.SaveTerms(order, aiTimeTerms{BalanceUnits: 9990000}); err != nil {
		t.Fatal(err)
	}
	return order
}

// runTimeFundingSuite 验证相同资金规则在真实事务和内存实现中一致。
func runTimeFundingSuite(t *testing.T, f aiTimeFixture) {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 1500000, Category: "admin_adjust"}); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.Current(f.email, now)
	if err != nil || current.ID != "" {
		t.Fatalf("旧余额不得自动转换: %+v %v", current, err)
	}
	order := f.makeTimeOrder(t, "time-first")
	current, err = f.store.Grant(order, now)
	if err != nil || current.BalanceUnits != 9990000 {
		t.Fatalf("grant: %+v %v", current, err)
	}
	funding, err := captureAIUsageFunding(f.wallet, f.email)
	if err != nil || funding.AccountID != current.ID {
		t.Fatal("未固定请求资金账期", err)
	}
	r := AIWalletRecord{UserEmail: f.email, ChangeUnits: -50000, Category: "ai_usage", PromptTokens: 100, ModelID: "test"}
	funding.bind(&r)
	if _, err = f.wallet.AdjustBalance(r); err != nil {
		t.Fatal(err)
	}
	if got, _ := balanceForFunding(f.wallet, f.email, aiUsageFunding{}); got != 1500000 {
		t.Fatalf("套餐消费碰了按量余额: %d", got)
	}
	if got, _ := f.wallet.BalanceUnits(f.email); got != 9940000 {
		t.Fatalf("未扣套餐额度: %d", got)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.store.Grant(order, time.Now()); errors <- e }()
	}
	wg.Wait()
	close(errors)
	for e := range errors {
		if e != nil {
			t.Fatal(e)
		}
	}
	current, _ = f.store.Current(f.email, now)
	if current.BalanceUnits != 9940000 || !current.ExpiresAt.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("重复回调改变权益: %+v", current)
	}
	if _, err = f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 200000, Category: "admin_adjust"}); err != nil {
		t.Fatal(err)
	}
	renew := f.makeTimeOrder(t, "time-renew")
	current, err = f.store.Grant(renew, now)
	if err != nil || current.BalanceUnits != 10140000 || !current.ExpiresAt.Equal(now.Add(60*24*time.Hour)) {
		t.Fatalf("续费减少高额度或期限错误: %+v %v", current, err)
	}
	if _, err = f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: -1000000, Category: "ai_usage"}); err != nil {
		t.Fatal(err)
	}
	current, err = f.store.Grant(f.makeTimeOrder(t, "time-renew-2"), now)
	if err != nil || current.BalanceUnits != 9990000 {
		t.Fatalf("续费未补足: %+v %v", current, err)
	}
	// 按量充值仍入原钱包，有效期内不替代套餐额度。
	if _, err = f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 20000, Category: "signup_gift"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := balanceForFunding(f.wallet, f.email, aiUsageFunding{}); got != 1520000 {
		t.Fatalf("按量充值错误: %d", got)
	}
	future := now.Add(100 * 24 * time.Hour)
	current, err = f.store.Current(f.email, future)
	if err != nil || current.BalanceUnits != 0 {
		t.Fatalf("到期未回收: %+v %v", current, err)
	}
	// 新账期建立后，之前已发起的请求只能结算到原账期。
	newAccount, err := f.store.Grant(f.makeTimeOrder(t, "time-new-period"), future)
	if err != nil || newAccount.ID == funding.AccountID {
		t.Fatal("过期重购必须新建账期", err)
	}
	if _, err = f.store.Adjust(r, funding.AccountID, future); err != nil {
		t.Fatal(err)
	}
	old, err := f.store.Account(f.email, funding.AccountID, future)
	if err != nil || old.BalanceUnits != 0 {
		t.Fatalf("过期请求应记账后回收: %+v %v", old, err)
	}
	newAccount, _ = f.store.Account(f.email, newAccount.ID, future)
	if newAccount.BalanceUnits != 9990000 {
		t.Fatal("旧请求扣了新套餐")
	}
	if got, _ := balanceForFunding(f.wallet, f.email, aiUsageFunding{}); got != 1520000 {
		t.Fatal("到期回收扣了按量本金")
	}
	usage, err := f.wallet.UsageUnitsBetween(now.Add(-time.Minute), future.Add(time.Hour))
	if err != nil || usage != 1100000 {
		t.Fatalf("消费统计需包含实际AI用量而非套餐发放: %d %v", usage, err)
	}
	if _, err = f.store.Adjust(AIWalletRecord{UserEmail: "other@example.test", ChangeUnits: -1, Category: "ai_usage"}, newAccount.ID, future); err == nil {
		t.Fatal("跨用户扣费必须拒绝")
	}
}

// TestAITimeFundingMemory 验证内存资金和账期边界。
func TestAITimeFundingMemory(t *testing.T) {
	wallet := NewMemoryAIWalletStore()
	store := NewMemoryAITimeStore(wallet)
	wallet.timePackages = store
	runTimeFundingSuite(t, aiTimeFixture{wallet, store, NewMemoryPaymentStore(), "time@example.test"})
}

// TestAITimeFundingPostgres 在显式隔离的回环测试库执行真实迁移与并发事务。
func TestAITimeFundingPostgres(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	aiRechargeApplyMigration(t, db, "0100_ai_usage_daily_retention.sql")
	aiRechargeApplyMigration(t, db, "0129_ai_time_packages.sql")
	wallet := NewPostgresAIWalletStore(db)
	store := NewPostgresAITimeStore(db)
	wallet.timePackages = store
	runTimeFundingSuite(t, aiTimeFixture{wallet, store, NewPostgresPaymentStore(db), "time@example.test"})
}

// TestAITimePaymentSnapshotAndRetry 验证付款金额、不可变额度快照与支付回调重放。
func TestAITimePaymentSnapshotAndRetry(t *testing.T) {
	server := mustNewServer(t)
	routes := server.Routes()
	email := "time-payment@example.com"
	token := loginForTest(t, routes, email)
	provider := &fakeWechatPaymentProvider{}
	server.payments.providers = map[string]PaymentProvider{provider.Name(): provider}
	cfg := SystemConfig{ConfigKey: aiTimePlansKey, ConfigValue: `[{"id":"month","name":"包月","duration_days":30,"price_cents":4000,"balance_cents":99900,"enabled":true}]`, Enabled: true}
	if err := server.systemConfigs.Save(cfg); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/payment/time-plan", bytes.NewBufferString(`{"plan_id":"month","price_cents":1,"balance_cents":99999999}`))
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Order PaymentOrder `json:"order"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cfg.ConfigValue = `[]`
	_ = server.systemConfigs.Save(cfg)
	transaction := PaymentProviderTransactionResult{OrderNo: result.Order.OrderNo, TradeNo: "time-payment", AmountCents: 1, Paid: true}
	if err := server.payments.completeProviderTransaction(provider.Name(), transaction); err == nil {
		t.Fatal("错误金额不可发放权益")
	}
	transaction.AmountCents = 4000
	for i := 0; i < 3; i++ {
		if err := server.payments.completeProviderTransaction(provider.Name(), transaction); err != nil {
			t.Fatal(err)
		}
	}
	account, err := timeStoreOf(server.payments.aiWallet).Current(email, time.Now())
	if err != nil || account.BalanceUnits != 9990000 {
		t.Fatalf("付款须用下单快照: %+v %v", account, err)
	}
}

// TestRegistrationZeroDaysPostgres 验证真实用户默认值读取系统配置，已有账号期限不被重写。
func TestRegistrationZeroDaysPostgres(t *testing.T) {
	db := aiRechargePostgresTestDB(t, false)
	_, err := db.Exec(`ALTER TABLE users ADD COLUMN subscription jsonb NOT NULL DEFAULT jsonb_build_object('member_type','max','expires_at',now()+interval '3 days'); INSERT INTO system_configs(config_key,config_value) VALUES('system.onboarding_config','{"trial_days":7,"wechat_follow_reward_days":2,"keep":"yes"}'); INSERT INTO users(email) VALUES('old@example.test')`)
	if err != nil {
		t.Fatal(err)
	}
	var old string
	_ = db.QueryRow(`SELECT subscription::text FROM users WHERE email='old@example.test'`).Scan(&old)
	aiRechargeApplyMigration(t, db, "0130_zero_registration_trial.sql")
	_, err = db.Exec(`INSERT INTO users(email) VALUES('new@example.test')`)
	if err != nil {
		t.Fatal(err)
	}
	var active bool
	if err = db.QueryRow(`SELECT (subscription->>'expires_at')::timestamptz>now() FROM users WHERE email='new@example.test'`).Scan(&active); err != nil || active {
		t.Fatalf("新注册仍在赠送: %v %v", active, err)
	}
	var after string
	_ = db.QueryRow(`SELECT subscription::text FROM users WHERE email='old@example.test'`).Scan(&after)
	if old != after {
		t.Fatal("已有会员被改变")
	}
	var kept bool
	if err = db.QueryRow(`SELECT config_value='{"trial_days":0,"wechat_follow_reward_days":2,"keep":"yes"}'::jsonb FROM system_configs WHERE config_key='system.onboarding_config'`).Scan(&kept); err != nil || !kept {
		t.Fatal("配置其他字段被改", err)
	}
	// 后台后续改为2天时应确实生效，不能退回硬编码。
	_, _ = db.Exec(`UPDATE system_configs SET config_value=jsonb_set(config_value,'{trial_days}','2'::jsonb) WHERE config_key='system.onboarding_config'; INSERT INTO users(email) VALUES('configured@example.test')`)
	var days int
	if err = db.QueryRow(`SELECT round(extract(epoch FROM ((subscription->>'expires_at')::timestamptz-now()))/86400)::int FROM users WHERE email='configured@example.test'`).Scan(&days); err != nil || days != 2 {
		t.Fatalf("配置未生效: %d %v", days, err)
	}
}

// TestAITimeInvalidConfig 验证空值、负数、重复编号及溢出配置均被拒绝。
func TestAITimeInvalidConfig(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[{"id":"m","name":"包月","duration_days":30,"price_cents":-1,"balance_cents":99900}]`, `[{"id":"m","name":"包月","duration_days":30,"price_cents":1,"balance_cents":9223372036854775807}]`} {
		if _, err := parseAITimePlans(raw); err == nil {
			t.Fatal("应拒绝", raw)
		}
	}
}

// TestAITimeNoCashRefund 验证套餐有效期内按量本金仍可按原账本退款，不把999额度当本金。
func TestAITimeNoCashRefund(t *testing.T) {
	f := newWalletLedgerFixture()
	store := NewMemoryAITimeStore(f.wallet)
	f.wallet.timePackages = store
	f.credit(t, "cash-order", 10000, 2000)
	tf := aiTimeFixture{f.wallet, store, f.orders, f.email}
	_, err := store.Grant(tf.makeTimeOrder(t, "package-order"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	refundable, err := f.wallet.RefundableUnits(f.email, "cash-order")
	if err != nil || refundable != 1000000 {
		t.Fatalf("仅允许原本金退款: %d %v", refundable, err)
	}
}

// TestAITimeChatExpiresDuringUpstream 验证真实聊天入口在等待期间到期，仍保留原始扣费流水且不转扣按量余额。
func TestAITimeChatExpiresDuringUpstream(t *testing.T) {
	var store *MemoryAITimeStore
	service, wallet := embeddingFixture(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		store.mu.Lock()
		for id, a := range store.accounts {
			a.ExpiresAt = time.Now().Add(-time.Second)
			store.accounts[id] = a
		}
		store.mu.Unlock()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":181,"completion_tokens":4361}}`))}, nil
	}))
	store = NewMemoryAITimeStore(wallet)
	wallet.timePackages = store
	f := aiTimeFixture{wallet, store, NewMemoryPaymentStore(), "embedding@example.com"}
	_, err := store.Grant(f.makeTimeOrder(t, "chat-expiry"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"first-model","stream":false,"messages":[{"role":"user","content":"合成请求"}]}`))
	req.Header.Set("Authorization", "Bearer embedding-user-key")
	req.Header.Set("X-GoodHR-Billing", "1")
	response := httptest.NewRecorder()
	service.CompatibleChat(response, req)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var result struct {
		Billing aiBillingReceipt `json:"goodhr_billing"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &result)
	if result.Billing.Status != "charged" || result.Billing.CostUnits == nil || *result.Billing.CostUnits <= 0 {
		t.Fatal("实际用量必须完成扣费", response.Body.String())
	}
	if got, _ := wallet.BalanceUnits(f.email); got != 100000 {
		t.Fatal("到期请求误扣按量余额", got)
	}
	a, _ := store.Current(f.email, time.Now())
	if a.BalanceUnits != 0 {
		t.Fatal("过期额度未归零")
	}
	records, _, _ := wallet.ListRecords(f.email, 100, 0)
	found := false
	for _, r := range records {
		if r.Category == "ai_usage" && r.RelatedOrderNo == "chat-expiry" && r.ChangeUnits == -*result.Billing.CostUnits && r.BonusChangeUnits == r.ChangeUnits {
			found = true
		}
	}
	if !found {
		t.Fatal("未保留原始消费、token用量及资金归属")
	}
}

// TestRegistrationZeroNoReward 验证注册零赠送既不发到账通知，也不显示虚假的试用欢迎弹框。
func TestRegistrationZeroNoReward(t *testing.T) {
	server := mustNewServer(t)
	mailer := &recordingMailer{}
	server.auth.mailer = mailer
	routes := server.Routes()
	token := loginForTest(t, routes, "zero-gift@example.com")
	if len(mailer.rewards) != 0 {
		t.Fatal("零天不应发送到账邮件")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, req)
	var payload struct {
		Show bool `json:"show_trial_welcome"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &payload)
	if response.Code != 200 || payload.Show {
		t.Fatal("零天不应出现试用到账弹框", response.Body.String())
	}
}
