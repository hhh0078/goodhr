// 本文件在 PostgreSQL 用户行锁事务内维护 AI 钱包分笔资金，确保充值、消费、退款预占和失败返还不会重复花钱。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// loadPGWalletLedger 锁定用户并读取分笔账本，所有钱包写入按用户行锁串行生效。
func loadPGWalletLedger(ctx context.Context, tx *sql.Tx, email string) (string, aiWalletLedger, error) {
	if strings.TrimSpace(email) == "" {
		return "", aiWalletLedger{}, errors.New("钱包账号不能为空。")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users(email) VALUES($1) ON CONFLICT(email) DO NOTHING`, email); err != nil {
		return "", aiWalletLedger{}, err
	}
	var userID string
	var ledger aiWalletLedger
	err := tx.QueryRowContext(ctx, `SELECT id::text, ai_balance_units, ai_bonus_balance_units, ai_wallet_refund_block_reason FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&userID, &ledger.BalanceUnits, &ledger.BonusUnits, &ledger.RefundBlockReason)
	if err != nil {
		return "", aiWalletLedger{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, order_no, source_category, principal_units, bonus_units, remaining_principal_units, remaining_bonus_units, created_at FROM ai_wallet_lots WHERE user_id=$1 ORDER BY id`, userID)
	if err != nil {
		return "", aiWalletLedger{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var lot aiWalletLot
		if err = rows.Scan(&lot.ID, &lot.OrderNo, &lot.SourceCategory, &lot.PrincipalUnits, &lot.BonusUnits, &lot.RemainingPrincipalUnits, &lot.RemainingBonusUnits, &lot.CreatedAt); err != nil {
			return "", aiWalletLedger{}, err
		}
		ledger.Lots = append(ledger.Lots, lot)
	}
	if err = rows.Err(); err != nil {
		return "", aiWalletLedger{}, err
	}
	return userID, ledger, nil
}

// savePGWalletLedger 保存实际变化的分笔余额及缓存，流水与资金变更使用同一事务。
func savePGWalletLedger(ctx context.Context, tx *sql.Tx, userID string, before, after aiWalletLedger, record *AIWalletRecord) error {
	if err := after.validate(); err != nil {
		return err
	}
	previous := make(map[int64]aiWalletLot, len(before.Lots))
	for _, lot := range before.Lots {
		previous[lot.ID] = lot
	}
	for _, lot := range after.Lots {
		if lot.ID == 0 {
			_, err := tx.ExecContext(ctx, `INSERT INTO ai_wallet_lots(user_id, order_no, source_category, principal_units, bonus_units, remaining_principal_units, remaining_bonus_units, created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, userID, lot.OrderNo, lot.SourceCategory, lot.PrincipalUnits, lot.BonusUnits, lot.RemainingPrincipalUnits, lot.RemainingBonusUnits, lot.CreatedAt)
			if err != nil {
				return err
			}
		} else if old := previous[lot.ID]; old.RemainingPrincipalUnits != lot.RemainingPrincipalUnits || old.RemainingBonusUnits != lot.RemainingBonusUnits {
			if _, err := tx.ExecContext(ctx, `UPDATE ai_wallet_lots SET remaining_principal_units=$2, remaining_bonus_units=$3 WHERE id=$1 AND user_id=$4`, lot.ID, lot.RemainingPrincipalUnits, lot.RemainingBonusUnits, userID); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET ai_balance_units=$2, ai_bonus_balance_units=$3, ai_wallet_refund_block_reason=$4 WHERE id=$1`, userID, after.BalanceUnits, after.BonusUnits, after.RefundBlockReason); err != nil {
		return err
	}
	if record == nil {
		return nil
	}
	record.WalletLedgerVersion = aiWalletLedgerVersion
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,bonus_change_units,bonus_balance_after_units,wallet_ledger_version,category,reason,related_order_no,model_id,prompt_tokens,completion_tokens,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, userID, record.UserEmail, aiUnitsToCents(record.ChangeUnits), aiUnitsToCents(record.BalanceAfterUnits), record.ChangeUnits, record.BalanceAfterUnits, record.BonusChangeUnits, record.BonusBalanceAfterUnits, record.WalletLedgerVersion, record.Category, record.Reason, record.RelatedOrderNo, record.ModelID, record.PromptTokens, record.CompletionTokens, record.CreatedAt)
	return err
}

// adjustLedgerBalance 原子入账本金及赠送，用户锁内检查订单去重，避免并发回调二次到账。
func (s *PostgresAIWalletStore) adjustLedgerBalance(record AIWalletRecord) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	userID, before, err := loadPGWalletLedger(ctx, tx, record.UserEmail)
	if err != nil {
		return 0, err
	}
	if record.Category == "recharge" && strings.TrimSpace(record.RelatedOrderNo) != "" {
		var recordedEmail string
		err = tx.QueryRowContext(ctx, `SELECT user_email FROM ai_balance_records WHERE category='recharge' AND related_order_no=$1`, record.RelatedOrderNo).Scan(&recordedEmail)
		if err == nil {
			if recordedEmail != record.UserEmail {
				return 0, errors.New("充值订单所属账号不一致。")
			}
			return before.BalanceUnits, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	after := before.clone()
	record.CreatedAt = time.Now()
	if err = after.applyRecord(&record); err != nil {
		return 0, err
	}
	if err = savePGWalletLedger(ctx, tx, userID, before, after, &record); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return after.BalanceUnits, nil
}

// BonusBalanceUnits 读取 PostgreSQL 中不可退款的赠送缓存。
func (s *PostgresAIWalletStore) BonusBalanceUnits(email string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return 0, err
	}
	var units int64
	err := s.db.QueryRowContext(ctx, `SELECT ai_bonus_balance_units FROM users WHERE email=$1`, email).Scan(&units)
	return units, err
}

// BalanceSnapshot 通过同一 SQL 快照读取总余额和不可退款赠送缓存。
func (s *PostgresAIWalletStore) BalanceSnapshot(email string) (int64, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := ensureUserID(ctx, s.db, email); err != nil {
		return 0, 0, err
	}
	var total, bonus int64
	err := s.db.QueryRowContext(ctx, `SELECT ai_balance_units,ai_bonus_balance_units FROM users WHERE email=$1`, email).Scan(&total, &bonus)
	return total, bonus, err
}

// RefundableUnits 在一致的用户快照中读取指定订单尚未使用的本金。
func (s *PostgresAIWalletStore) RefundableUnits(email, orderNo string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, ledger, err := loadPGWalletLedger(ctx, tx, email)
	if err != nil {
		return 0, err
	}
	units, err := ledger.refundable(orderNo)
	if err != nil {
		return 0, err
	}
	var open bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM payment_refunds WHERE user_email=$1 AND order_no=$2 AND status IN('pending','unknown','processing','action_required'))`, email, orderNo).Scan(&open)
	if err != nil {
		return 0, err
	}
	if open {
		units = 0
	}
	return units, tx.Commit()
}

// ReserveRefund 在数据库事务中校验订单、占用本金、收回赠送和创建退款记录。
func (s *PostgresAIWalletStore) ReserveRefund(refund PaymentRefund, orders PaymentStore) (PaymentRefund, error) {
	if store, ok := orders.(*PostgresPaymentStore); !ok || store.db != s.db {
		return PaymentRefund{}, errors.New("退款与钱包必须使用同一个数据库。")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaymentRefund{}, err
	}
	defer tx.Rollback()
	userID, before, err := loadPGWalletLedger(ctx, tx, refund.UserEmail)
	if err != nil {
		return PaymentRefund{}, err
	}
	existing, err := scanPaymentRefund(tx.QueryRowContext(ctx, `SELECT `+paymentRefundColumns+` FROM payment_refunds WHERE refund_no=$1`, refund.RefundNo))
	if err == nil {
		if existing.OrderNo != refund.OrderNo || existing.UserEmail != refund.UserEmail || existing.AmountCents != refund.AmountCents {
			return PaymentRefund{}, errors.New("退款单号与原请求不一致。")
		}
		return existing, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return PaymentRefund{}, err
	}
	var order PaymentOrder
	order.OrderNo = refund.OrderNo
	err = tx.QueryRowContext(ctx, `SELECT user_email,order_type,status,amount_cents FROM payment_orders WHERE order_no=$1 FOR UPDATE`, refund.OrderNo).Scan(&order.UserEmail, &order.OrderType, &order.Status, &order.AmountCents)
	if err != nil {
		return PaymentRefund{}, err
	}
	var occupiedCents, openCount int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_cents) FILTER(WHERE status NOT IN('failed','closed')),0),COUNT(*) FILTER(WHERE status IN('pending','unknown','processing','action_required')) FROM payment_refunds WHERE order_no=$1`, refund.OrderNo).Scan(&occupiedCents, &openCount)
	if err != nil {
		return PaymentRefund{}, err
	}
	if err = validateWalletRefundOrder(order, refund, occupiedCents); err != nil {
		return PaymentRefund{}, err
	}
	if openCount > 0 {
		return PaymentRefund{}, errors.New("这笔充值已有退款在处理中，等它确认后再申请。")
	}
	after := before.clone()
	record, err := after.reserve(&refund)
	if err != nil {
		return PaymentRefund{}, err
	}
	refund.Status = "pending"
	stored, err := scanPaymentRefund(tx.QueryRowContext(ctx, `INSERT INTO payment_refunds(refund_no,order_no,user_id,user_email,refund_type,amount_cents,deducted_days,revoked_bonus_units,rights_reserved,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true,'pending') RETURNING `+paymentRefundColumns, refund.RefundNo, refund.OrderNo, userID, refund.UserEmail, refund.RefundType, refund.AmountCents, refund.DeductedDays, refund.RevokedBonusUnits))
	if err != nil {
		return PaymentRefund{}, err
	}
	if err = savePGWalletLedger(ctx, tx, userID, before, after, &record); err != nil {
		return PaymentRefund{}, err
	}
	if err = tx.Commit(); err != nil {
		return PaymentRefund{}, err
	}
	return stored, nil
}

// ResolveRefund 在同一事务保存渠道结果并在确认失败时返还预扣资金，所有终态处理保持幂等。
func (s *PostgresAIWalletStore) ResolveRefund(refundNo, status, providerRefundID, responseData string, orders PaymentStore) (PaymentRefund, error) {
	if store, ok := orders.(*PostgresPaymentStore); !ok || store.db != s.db {
		return PaymentRefund{}, errors.New("退款与钱包必须使用同一个数据库。")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var email string
	if err := s.db.QueryRowContext(ctx, `SELECT user_email FROM payment_refunds WHERE refund_no=$1`, refundNo).Scan(&email); err != nil {
		return PaymentRefund{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaymentRefund{}, err
	}
	defer tx.Rollback()
	userID, before, err := loadPGWalletLedger(ctx, tx, email)
	if err != nil {
		return PaymentRefund{}, err
	}
	refund, err := scanPaymentRefund(tx.QueryRowContext(ctx, `SELECT `+paymentRefundColumns+` FROM payment_refunds WHERE refund_no=$1 FOR UPDATE`, refundNo))
	if err != nil {
		return PaymentRefund{}, err
	}
	if refund.RefundType != refundTypeAIBalance {
		return PaymentRefund{}, errors.New("退款类型与钱包不一致。")
	}
	if walletRefundTerminal(refund.Status) {
		return refund, tx.Commit()
	}
	status = normalizeWalletRefundStatus(status)
	after := before.clone()
	var record *AIWalletRecord
	if (status == "failed" || status == "closed") && refund.RightsReserved {
		var hasReservation bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_balance_records WHERE category='refund' AND related_order_no=$1 AND user_id=$2 AND change_units=$3)`, refundNo, userID, -centsToAIUnits(refund.AmountCents)-refund.RevokedBonusUnits).Scan(&hasReservation)
		if err != nil {
			return PaymentRefund{}, err
		}
		if !hasReservation {
			return PaymentRefund{}, errors.New("退款预扣流水未核对，暂不能自动返还。")
		}
		item, releaseErr := after.release(refund)
		if releaseErr != nil {
			return PaymentRefund{}, releaseErr
		}
		record = &item
		refund.RightsReserved = false
	}
	if providerRefundID == "" {
		providerRefundID = refund.ProviderRefundID
	}
	stored, err := scanPaymentRefund(tx.QueryRowContext(ctx, `UPDATE payment_refunds SET status=$2,provider_refund_id=$3,response_data=$4::jsonb,rights_reserved=$5,updated_at=NOW(),completed_at=CASE WHEN $2='success' THEN NOW() ELSE completed_at END WHERE refund_no=$1 RETURNING `+paymentRefundColumns, refundNo, status, providerRefundID, safeJSONText(responseData), refund.RightsReserved))
	if err != nil {
		return PaymentRefund{}, err
	}
	if record != nil {
		if err = savePGWalletLedger(ctx, tx, userID, before, after, record); err != nil {
			return PaymentRefund{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return PaymentRefund{}, err
	}
	return stored, nil
}
