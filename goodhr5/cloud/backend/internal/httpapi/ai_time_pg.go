// Package httpapi 本文件实现 PostgreSQL 时间套餐独立账本，按用户事务锁串行处理续费、扣费和到期回收。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresAITimeStore 使用独立账期表保护按量充值本金，并把消费写入共用流水。
type PostgresAITimeStore struct{ db *sql.DB }

// NewPostgresAITimeStore 创建数据库套餐账本。
func NewPostgresAITimeStore(db *sql.DB) *PostgresAITimeStore { return &PostgresAITimeStore{db: db} }

// transaction 在同一账号锁内加载完整账期，业务变动和消费流水一并提交。
func (s *PostgresAITimeStore) transaction(email string, apply func(context.Context, *sql.Tx, []AITimeAccount) (AITimeAccount, error)) (AITimeAccount, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AITimeAccount{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('ai-time:' || $1,0))`, email); err != nil {
		return AITimeAccount{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,user_email,plan_id,plan_name,expires_at,balance_units FROM ai_time_accounts WHERE user_email=$1 ORDER BY expires_at DESC,id FOR UPDATE`, email)
	if err != nil {
		return AITimeAccount{}, err
	}
	accounts := []AITimeAccount{}
	for rows.Next() {
		var a AITimeAccount
		if err = rows.Scan(&a.ID, &a.UserEmail, &a.PlanID, &a.PlanName, &a.ExpiresAt, &a.BalanceUnits); err != nil {
			rows.Close()
			return a, err
		}
		accounts = append(accounts, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AITimeAccount{}, err
	}
	result, err := apply(ctx, tx, accounts)
	if err != nil {
		return AITimeAccount{}, err
	}
	return result, tx.Commit()
}

// savePGTimeRecord 同事务保存非本金额度和原始用量，不更新用户按量钱包。
func savePGTimeRecord(ctx context.Context, tx *sql.Tx, a AITimeAccount, r AIWalletRecord, now time.Time) error {
	if r.Category == "ai_usage" {
		r.BonusChangeUnits = r.ChangeUnits
	}
	if r.Category == "admin_adjust" {
		r.Category = "time_package_adjust"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ai_time_accounts SET balance_units=$2,plan_id=$3,plan_name=$4,expires_at=$5,updated_at=$6 WHERE id=$1`, a.ID, a.BalanceUnits, a.PlanID, a.PlanName, a.ExpiresAt, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_balance_records(user_id,user_email,change_cents,balance_after_cents,change_units,balance_after_units,bonus_change_units,bonus_balance_after_units,wallet_ledger_version,category,reason,related_order_no,model_id,prompt_tokens,completion_tokens,created_at) VALUES((SELECT id FROM users WHERE email=$1),$1,$2,$3,$4,$5,$15,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, a.UserEmail, aiUnitsToCents(r.ChangeUnits), aiUnitsToCents(a.BalanceUnits), r.ChangeUnits, a.BalanceUnits, max(int64(0), a.BalanceUnits), aiWalletLedgerVersion, r.Category, timeAccountReason(r.Reason), a.ID, r.ModelID, r.PromptTokens, r.CompletionTokens, now, r.BonusChangeUnits)
	return err
}

// expirePGTimeAccount 回收旧账期剩余额度，保留所有消费明细。
func expirePGTimeAccount(ctx context.Context, tx *sql.Tx, a AITimeAccount, now time.Time) (AITimeAccount, error) {
	if a.active(now) || a.BalanceUnits == 0 {
		return a, nil
	}
	change := -a.BalanceUnits
	a.BalanceUnits = 0
	return a, savePGTimeRecord(ctx, tx, a, AIWalletRecord{ChangeUnits: change, Category: "time_package_expiry", Reason: "套餐到期回收剩余额度"}, now)
}

// Current 返回本人最新账期并清理旧额度，普通查询不修改真实余额。
func (s *PostgresAITimeStore) Current(email string, now time.Time) (AITimeAccount, error) {
	return s.transaction(email, func(ctx context.Context, tx *sql.Tx, accounts []AITimeAccount) (AITimeAccount, error) {
		var current AITimeAccount
		for i, a := range accounts {
			updated, err := expirePGTimeAccount(ctx, tx, a, now)
			if err != nil {
				return a, err
			}
			if i == 0 {
				current = updated
			}
		}
		return current, nil
	})
}

// Account 只读取请求所属账期，避免过期请求误用新购买额度。
func (s *PostgresAITimeStore) Account(email, id string, now time.Time) (AITimeAccount, error) {
	return s.transaction(email, func(ctx context.Context, tx *sql.Tx, accounts []AITimeAccount) (AITimeAccount, error) {
		for _, a := range accounts {
			if a.ID == id {
				return expirePGTimeAccount(ctx, tx, a, now)
			}
		}
		return AITimeAccount{}, errors.New("套餐账期不存在")
	})
}

// SaveTerms 保存下单额度快照，后续调价不改变未支付订单权益。
func (s *PostgresAITimeStore) SaveTerms(order PaymentOrder, terms aiTimeTerms) error {
	if terms.BalanceUnits <= 0 {
		return errors.New("套餐额度必须大于0")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var balance int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO ai_time_order_terms(order_no,balance_units) VALUES($1,$2) ON CONFLICT(order_no) DO UPDATE SET order_no=EXCLUDED.order_no RETURNING balance_units`, order.OrderNo, terms.BalanceUnits).Scan(&balance)
	if err == nil && balance != terms.BalanceUnits {
		return errors.New("套餐订单额度已固定")
	}
	return err
}

// Grant 在账号事务锁内按支付订单幂等发放权益，续费补足目标额度并延长期限。
func (s *PostgresAITimeStore) Grant(order PaymentOrder, now time.Time) (AITimeAccount, error) {
	if order.Status != "paid" || order.OrderType != "ai_time" || order.DurationDays <= 0 {
		return AITimeAccount{}, errors.New("套餐订单尚未支付")
	}
	return s.transaction(order.UserEmail, func(ctx context.Context, tx *sql.Tx, accounts []AITimeAccount) (AITimeAccount, error) {
		var grantedID string
		err := tx.QueryRowContext(ctx, `SELECT account_id FROM ai_time_grants WHERE order_no=$1`, order.OrderNo).Scan(&grantedID)
		if err == nil {
			for _, a := range accounts {
				if a.ID == grantedID {
					return expirePGTimeAccount(ctx, tx, a, now)
				}
			}
			return AITimeAccount{}, errors.New("套餐权益账期缺失")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return AITimeAccount{}, err
		}
		var target int64
		if err = tx.QueryRowContext(ctx, `SELECT balance_units FROM ai_time_order_terms WHERE order_no=$1`, order.OrderNo).Scan(&target); err != nil {
			return AITimeAccount{}, err
		}
		var current AITimeAccount
		for _, a := range accounts {
			a, err = expirePGTimeAccount(ctx, tx, a, now)
			if err != nil {
				return a, err
			}
			if current.ID == "" && a.active(now) {
				current = a
			}
		}
		if current.ID == "" {
			current = AITimeAccount{ID: order.OrderNo, UserEmail: order.UserEmail, ExpiresAt: now, PlanID: order.PlanID, PlanName: order.PlanName}
			if _, err = tx.ExecContext(ctx, `INSERT INTO ai_time_accounts(id,user_email,plan_id,plan_name,expires_at,balance_units,created_at,updated_at) VALUES($1,$2,$3,$4,$5,0,$5,$5)`, current.ID, current.UserEmail, current.PlanID, current.PlanName, now); err != nil {
				return current, err
			}
		}
		current.PlanID, current.PlanName = order.PlanID, order.PlanName
		current.ExpiresAt = current.ExpiresAt.Add(time.Duration(order.DurationDays) * 24 * time.Hour)
		change, err := timeTopUpUnits(current.BalanceUnits, target)
		if err != nil {
			return current, err
		}
		current.BalanceUnits += change
		if err = savePGTimeRecord(ctx, tx, current, AIWalletRecord{ChangeUnits: change, Category: "time_package_grant", Reason: order.PlanName + "支付成功：" + order.OrderNo}, now); err != nil {
			return current, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ai_time_grants(order_no,account_id,granted_units,applied_at) VALUES($1,$2,$3,$4)`, order.OrderNo, current.ID, change, now)
		return current, err
	})
}

// Adjust 将原始 AI 消费扣在发起请求的账期，过期结算后清零且不会转扣其他资金。
func (s *PostgresAITimeStore) Adjust(record AIWalletRecord, id string, now time.Time) (int64, error) {
	a, err := s.transaction(record.UserEmail, func(ctx context.Context, tx *sql.Tx, accounts []AITimeAccount) (AITimeAccount, error) {
		for _, a := range accounts {
			if a.ID != id {
				continue
			}
			if record.Category != "ai_usage" && (!a.active(now) || record.Category != "admin_adjust") {
				return a, errors.New("套餐已到期，不能再调整额度")
			}
			next, err := checkedAIWalletAdd(a.BalanceUnits, record.ChangeUnits)
			if err != nil || next == -1<<63 {
				return a, errors.New("套餐额度超出可记账范围")
			}
			a.BalanceUnits = next
			if err = savePGTimeRecord(ctx, tx, a, record, now); err != nil {
				return a, err
			}
			return expirePGTimeAccount(ctx, tx, a, now)
		}
		return AITimeAccount{}, errors.New("套餐账期不匹配")
	})
	return a.BalanceUnits, err
}

// Expire 定期扫描到期账户，每个账号单独事务回收，避免持有全表写锁。
func (s *PostgresAITimeStore) Expire(now time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_email FROM ai_time_accounts WHERE expires_at<=$1 AND balance_units<>0`, now)
	if err != nil {
		return err
	}
	emails := []string{}
	for rows.Next() {
		var email string
		if err = rows.Scan(&email); err != nil {
			rows.Close()
			return err
		}
		emails = append(emails, email)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, email := range emails {
		if _, err = s.Current(email, now); err != nil {
			return err
		}
	}
	return nil
}
