// 本文件验证余额流水API明确区分历史算法与分笔算法，避免用迁移赠送变化倒算历史现金退款。
package httpapi

import (
	"encoding/json"
	"testing"
)

// TestAIWalletRecordLedgerVersionAPI 验证版本0不会省略，版本1明确返回，原流水金额与赠送变更均保持原值。
func TestAIWalletRecordLedgerVersionAPI(t *testing.T) {
	for _, version := range []int{0, 1} {
		record := AIWalletRecord{Category: "refund", ChangeUnits: -100000, BonusChangeUnits: -7000, WalletLedgerVersion: version}
		raw, err := json.Marshal(publicAIWalletRecord(record))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err = json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		var actualVersion int
		if value, exists := payload["wallet_ledger_version"]; !exists {
			t.Fatal("API omitted historical ledger version")
		} else if err = json.Unmarshal(value, &actualVersion); err != nil || actualVersion != version {
			t.Fatalf("version payload=%s expected=%d err=%v", raw, version, err)
		}
		var change, bonus int64
		if err = json.Unmarshal(payload["change_units"], &change); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(payload["bonus_change_units"], &bonus); err != nil {
			t.Fatal(err)
		}
		if change != record.ChangeUnits || bonus != record.BonusChangeUnits {
			t.Fatalf("version metadata changed historical monetary values: %s", raw)
		}
	}
}

// TestMemoryAIWalletRecordsUseNewLedgerVersion 验证全部新内存入账、消费、退款及返还流水都由保存入口固定为版本1。
func TestMemoryAIWalletRecordsUseNewLedgerVersion(t *testing.T) {
	f := newWalletLedgerFixture()
	for _, category := range []string{"signup_bonus", "admin_adjust"} {
		if _, err := f.wallet.AdjustBalance(AIWalletRecord{UserEmail: f.email, ChangeUnits: 7000, Category: category, WalletLedgerVersion: 0}); err != nil {
			t.Fatal(err)
		}
	}
	f.credit(t, "version-order", 10000, 2000)
	f.consume(t, 900000)
	refund, err := f.wallet.ReserveRefund(f.refund("version-refund", "version-order", 1000), f.orders)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.wallet.ResolveRefund(refund.RefundNo, "closed", "", `{}`, f.orders); err != nil {
		t.Fatal(err)
	}
	records, total, err := f.wallet.ListRecords(f.email, 100, 0)
	if err != nil || total != 6 || len(records) != 6 {
		t.Fatalf("new memory records=%d total=%d err=%v", len(records), total, err)
	}
	for _, record := range records {
		if record.WalletLedgerVersion != 1 || publicAIWalletRecord(record)["wallet_ledger_version"] != 1 {
			t.Fatalf("new memory record missing version1: %+v", record)
		}
	}
}
