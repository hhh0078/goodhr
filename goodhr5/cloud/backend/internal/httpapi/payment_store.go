// 本文件负责提供支付记录存储抽象和内存/PostgreSQL 双实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// PaymentOrder 表示一条会员订阅支付记录。
type PaymentOrder struct {
	ID                  string     `json:"id"`
	OrderNo             string     `json:"order_no"`
	OrderType           string     `json:"order_type"`
	UserEmail           string     `json:"user_email"`
	PlanID              string     `json:"plan_id"`
	PlanName            string     `json:"plan_name"`
	MemberType          string     `json:"member_type"`
	DurationDays        int        `json:"duration_days"`
	OriginalAmountCents int        `json:"original_amount_cents"`
	DiscountAmountCents int        `json:"discount_amount_cents"`
	UpgradeFromType     string     `json:"upgrade_from_member_type"`
	UpgradeCreditCents  int        `json:"upgrade_credit_cents"`
	AmountCents         int        `json:"amount_cents"`
	BonusAmountCents    int        `json:"bonus_amount_cents"`
	PaymentProvider     string     `json:"payment_provider"`
	TradeNo             string     `json:"trade_no"`
	Status              string     `json:"status"`
	PaidAt              *time.Time `json:"paid_at,omitempty"`
	ExpiredAt           *time.Time `json:"expired_at,omitempty"`
	NotifyData          string     `json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// PaymentRefund 表示一条自动退款记录。
type PaymentRefund struct {
	ID                string     `json:"id"`
	RefundNo          string     `json:"refund_no"`
	OrderNo           string     `json:"order_no"`
	UserEmail         string     `json:"user_email"`
	RefundType        string     `json:"refund_type"`
	AmountCents       int        `json:"amount_cents"`
	DeductedDays      int        `json:"deducted_days"`
	RevokedBonusUnits int64      `json:"revoked_bonus_units"`
	RightsReserved    bool       `json:"rights_reserved"`
	Status            string     `json:"status"`
	ProviderRefundID  string     `json:"provider_refund_id"`
	ResponseData      string     `json:"-"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// PaymentStore 定义支付记录持久化能力。
type PaymentStore interface {
	// Create 保存一条新支付记录。
	Create(order PaymentOrder) (PaymentOrder, error)
	// ByOrderNo 按订单号读取支付记录。
	ByOrderNo(orderNo string) (PaymentOrder, error)
	// ListByUser 列出指定用户自己的支付记录。
	ListByUser(email string) ([]PaymentOrder, error)
	// ListAll 列出全部支付记录，供超级管理员查看。
	ListAll() ([]PaymentOrder, error)
	// MarkPaid 将待支付订单标记为已支付。
	MarkPaid(orderNo string, tradeNo string, notifyData string) (PaymentOrder, bool, error)
	// CreateRefund 保存待提交的退款记录，用于先占用可退额度。
	CreateRefund(refund PaymentRefund) (PaymentRefund, error)
	// ByRefundNo 按退款单号读取完整退款记录。
	ByRefundNo(refundNo string) (PaymentRefund, error)
	// ListOpenRefunds 邮箱非空时读取该用户全部活跃退款用于防重复，邮箱为空时只取到期对账批次。
	ListOpenRefunds(email string) ([]PaymentRefund, error)
	// UpdateRefund 更新第三方退款结果。
	UpdateRefund(refundNo string, status string, providerRefundID string, responseData string) (PaymentRefund, error)
	// RefundedCentsByOrder 汇总订单已占用和已受理的退款金额。
	RefundedCentsByOrder(orderNo string) (int, error)
}

// MemoryPaymentStore 在内存中保存支付记录，用于未启用 PostgreSQL 的开发环境。
type MemoryPaymentStore struct {
	mu      sync.Mutex
	orders  map[string]PaymentOrder
	refunds map[string]PaymentRefund
}

// NewMemoryPaymentStore 创建内存支付记录存储。
func NewMemoryPaymentStore() *MemoryPaymentStore {
	return &MemoryPaymentStore{orders: map[string]PaymentOrder{}, refunds: map[string]PaymentRefund{}}
}

// CreateRefund 保存内存退款记录。
func (s *MemoryPaymentStore) CreateRefund(refund PaymentRefund) (PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.refunds[refund.RefundNo]; exists {
		return PaymentRefund{}, errors.New("退款单号已存在")
	}
	now := time.Now()
	if refund.ID == "" {
		refund.ID = refund.RefundNo
	}
	refund.Status = defaultString(refund.Status, "pending")
	refund.CreatedAt = now
	refund.UpdatedAt = now
	s.refunds[refund.RefundNo] = refund
	return refund, nil
}

// ByRefundNo 读取内存中的单条退款记录。
func (s *MemoryPaymentStore) ByRefundNo(refundNo string) (PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refund, ok := s.refunds[refundNo]
	if !ok {
		return PaymentRefund{}, ErrNotFound
	}
	return refund, nil
}

// ListOpenRefunds 查询用户时保留全部活跃退款，后台批次先筛到期再限制数量，避免未到期旧单挡住新单。
func (s *MemoryPaymentStore) ListOpenRefunds(email string) ([]PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	result := []PaymentRefund{}
	for _, refund := range s.refunds {
		if refund.RefundType == refundTypeAIBalance && (email == "" || refund.UserEmail == email) && openRefundStatus(refund.Status) && (email != "" || refundReconcileDue(refund, now)) {
			result = append(result, refund)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].UpdatedAt.Equal(result[right].UpdatedAt) {
			return result[left].RefundNo < result[right].RefundNo
		}
		return result[left].UpdatedAt.Before(result[right].UpdatedAt)
	})
	if email == "" && len(result) > aiRefundReconcileBatchLimit {
		result = result[:aiRefundReconcileBatchLimit]
	}
	return result, nil
}

// UpdateRefund 更新内存退款结果。
func (s *MemoryPaymentStore) UpdateRefund(refundNo string, status string, providerRefundID string, responseData string) (PaymentRefund, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refund, ok := s.refunds[refundNo]
	if !ok {
		return PaymentRefund{}, ErrNotFound
	}
	now := time.Now()
	refund.Status = status
	if status == "failed" || status == "closed" {
		refund.RightsReserved = false
	}
	refund.ProviderRefundID = providerRefundID
	refund.ResponseData = responseData
	refund.UpdatedAt = now
	if status == "success" {
		refund.CompletedAt = &now
	}
	s.refunds[refundNo] = refund
	return refund, nil
}

// RefundedCentsByOrder 汇总内存订单仍占用额度的退款金额。
func (s *MemoryPaymentStore) RefundedCentsByOrder(orderNo string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, refund := range s.refunds {
		if refund.OrderNo == orderNo && refund.Status != "failed" && refund.Status != "closed" {
			total += refund.AmountCents
		}
	}
	return total, nil
}

// Create 保存内存支付记录。
func (s *MemoryPaymentStore) Create(order PaymentOrder) (PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if order.ID == "" {
		order.ID = order.OrderNo
	}
	order.OrderType = defaultString(order.OrderType, "subscription")
	order.CreatedAt = now
	order.UpdatedAt = now
	s.orders[order.OrderNo] = order
	return order, nil
}

// ByOrderNo 按订单号读取内存支付记录。
func (s *MemoryPaymentStore) ByOrderNo(orderNo string) (PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderNo]
	if !ok {
		return PaymentOrder{}, ErrNotFound
	}
	return order, nil
}

// ListByUser 列出指定用户自己的内存支付记录。
func (s *MemoryPaymentStore) ListByUser(email string) ([]PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []PaymentOrder{}
	for _, order := range s.orders {
		if order.UserEmail == email {
			result = append(result, order)
		}
	}
	return result, nil
}

// ListAll 列出全部内存支付记录。
func (s *MemoryPaymentStore) ListAll() ([]PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]PaymentOrder, 0, len(s.orders))
	for _, order := range s.orders {
		result = append(result, order)
	}
	return result, nil
}

// MarkPaid 将内存待支付订单标记为已支付。
func (s *MemoryPaymentStore) MarkPaid(orderNo string, tradeNo string, notifyData string) (PaymentOrder, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order, ok := s.orders[orderNo]
	if !ok {
		return PaymentOrder{}, false, ErrNotFound
	}
	if order.Status == "paid" {
		return order, false, nil
	}
	if order.Status != "pending" {
		return order, false, nil
	}
	now := time.Now()
	order.Status = "paid"
	order.TradeNo = tradeNo
	order.NotifyData = notifyData
	order.PaidAt = &now
	order.UpdatedAt = now
	s.orders[orderNo] = order
	return order, true, nil
}

// PostgresPaymentStore 在 PostgreSQL 中保存支付记录。
type PostgresPaymentStore struct {
	db *sql.DB
}

// NewPostgresPaymentStore 创建 PostgreSQL 支付记录存储。
func NewPostgresPaymentStore(db *sql.DB) *PostgresPaymentStore {
	return &PostgresPaymentStore{db: db}
}

// Create 保存 PostgreSQL 支付记录。
func (s *PostgresPaymentStore) Create(order PaymentOrder) (PaymentOrder, error) {
	userID, err := ensureUserID(context.Background(), s.db, order.UserEmail)
	if err != nil {
		return PaymentOrder{}, err
	}
	err = s.db.QueryRow(`
		INSERT INTO payment_orders (
			order_no, order_type, user_id, user_email, plan_id, plan_name, member_type, duration_days,
			original_amount_cents, discount_amount_cents, upgrade_from_member_type, upgrade_credit_cents,
			amount_cents, bonus_amount_cents, payment_provider, status, expired_at, notify_data
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, '{}'::jsonb)
		RETURNING id, created_at, updated_at
	`,
		order.OrderNo,
		defaultString(order.OrderType, "subscription"),
		userID,
		order.UserEmail,
		order.PlanID,
		order.PlanName,
		order.MemberType,
		order.DurationDays,
		order.OriginalAmountCents,
		order.DiscountAmountCents,
		order.UpgradeFromType,
		order.UpgradeCreditCents,
		order.AmountCents,
		order.BonusAmountCents,
		order.PaymentProvider,
		order.Status,
		order.ExpiredAt,
	).Scan(&order.ID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return PaymentOrder{}, err
	}
	return order, nil
}

// ByOrderNo 按订单号读取 PostgreSQL 支付记录。
func (s *PostgresPaymentStore) ByOrderNo(orderNo string) (PaymentOrder, error) {
	return scanPaymentOrder(s.db.QueryRow(`
		SELECT id, order_no, COALESCE(order_type, 'subscription'), user_email, plan_id, plan_name, member_type, duration_days,
			original_amount_cents, discount_amount_cents, upgrade_from_member_type, upgrade_credit_cents,
			amount_cents, bonus_amount_cents, payment_provider,
			trade_no, status, paid_at, expired_at, notify_data::text, created_at, updated_at
		FROM payment_orders
		WHERE order_no=$1
		`, orderNo))
}

// ListByUser 列出指定用户自己的 PostgreSQL 支付记录。
func (s *PostgresPaymentStore) ListByUser(email string) ([]PaymentOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, order_no, COALESCE(order_type, 'subscription'), user_email, plan_id, plan_name, member_type, duration_days,
			original_amount_cents, discount_amount_cents, upgrade_from_member_type, upgrade_credit_cents,
			amount_cents, bonus_amount_cents, payment_provider,
			trade_no, status, paid_at, expired_at, notify_data::text, created_at, updated_at
		FROM payment_orders
		WHERE user_email=$1
		ORDER BY created_at DESC
		`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPaymentOrders(rows)
}

// ListAll 列出全部 PostgreSQL 支付记录。
func (s *PostgresPaymentStore) ListAll() ([]PaymentOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, order_no, COALESCE(order_type, 'subscription'), user_email, plan_id, plan_name, member_type, duration_days,
			original_amount_cents, discount_amount_cents, upgrade_from_member_type, upgrade_credit_cents,
			amount_cents, bonus_amount_cents, payment_provider,
			trade_no, status, paid_at, expired_at, notify_data::text, created_at, updated_at
		FROM payment_orders
		ORDER BY created_at DESC
		LIMIT 500
		`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPaymentOrders(rows)
}

// MarkPaid 将 PostgreSQL 待支付订单标记为已支付。
func (s *PostgresPaymentStore) MarkPaid(orderNo string, tradeNo string, notifyData string) (PaymentOrder, bool, error) {
	current, err := s.ByOrderNo(orderNo)
	if err != nil {
		return PaymentOrder{}, false, err
	}
	if current.Status == "paid" {
		return current, false, nil
	}
	now := time.Now()
	result, err := s.db.Exec(`
		UPDATE payment_orders
		SET status='paid', trade_no=$2, paid_at=$3, notify_data=$4::jsonb, updated_at=$3
		WHERE order_no=$1 AND status='pending'
		`, orderNo, tradeNo, now, safeJSONText(notifyData))
	if err != nil {
		return PaymentOrder{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return PaymentOrder{}, false, err
	}
	updated, err := s.ByOrderNo(orderNo)
	return updated, affected > 0, err
}

// CreateRefund 保存 PostgreSQL 退款记录。
func (s *PostgresPaymentStore) CreateRefund(refund PaymentRefund) (PaymentRefund, error) {
	userID, err := ensureUserID(context.Background(), s.db, refund.UserEmail)
	if err != nil {
		return PaymentRefund{}, err
	}
	err = s.db.QueryRow(`
		INSERT INTO payment_refunds (
			refund_no, order_no, user_id, user_email, refund_type, amount_cents,
			deducted_days, revoked_bonus_units, rights_reserved, status, response_data
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'{}'::jsonb)
		RETURNING id::text, created_at, updated_at
	`, refund.RefundNo, refund.OrderNo, userID, refund.UserEmail, refund.RefundType, refund.AmountCents, refund.DeductedDays, refund.RevokedBonusUnits, refund.RightsReserved, defaultString(refund.Status, "pending")).Scan(&refund.ID, &refund.CreatedAt, &refund.UpdatedAt)
	if err != nil {
		return PaymentRefund{}, err
	}
	return refund, nil
}

// paymentRefundColumns 统一完整退款记录的读取顺序，供钱包事务和支付存储复用。
const paymentRefundColumns = `id::text, refund_no, order_no, user_email, refund_type, amount_cents,
	deducted_days, revoked_bonus_units, rights_reserved, status, provider_refund_id, response_data::text,
	created_at, updated_at, completed_at`

// ByRefundNo 从 PostgreSQL 读取单条退款记录。
func (s *PostgresPaymentStore) ByRefundNo(refundNo string) (PaymentRefund, error) {
	return scanPaymentRefund(s.db.QueryRow(`SELECT `+paymentRefundColumns+` FROM payment_refunds WHERE refund_no=$1`, refundNo))
}

// refundReconcileDueSQL 按 Go 共用规则生成到期条件和时间参数，参数从邮箱之后开始编号。
func refundReconcileDueSQL(now time.Time) (string, []any) {
	var expression strings.Builder
	expression.WriteString("updated_at <= (CASE ")
	args := make([]any, 0, len(aiRefundReconcileRules)*2+1)
	for _, rule := range aiRefundReconcileRules {
		fmt.Fprintf(&expression, "WHEN created_at < $%d THEN $%d::timestamptz ", len(args)+2, len(args)+3)
		args = append(args, now.Add(-rule.OlderThan), now.Add(-rule.Interval))
	}
	fmt.Fprintf(&expression, "ELSE $%d::timestamptz END)", len(args)+2)
	args = append(args, now.Add(-aiRefundReconcileInterval))
	return expression.String(), args
}

// ListOpenRefunds 查询用户时不截断活跃退款；后台先在 SQL 中筛到期再取最久未核实的50条。
func (s *PostgresPaymentStore) ListOpenRefunds(email string) ([]PaymentRefund, error) {
	dueSQL, dueArgs := refundReconcileDueSQL(time.Now())
	query := `SELECT ` + paymentRefundColumns + ` FROM payment_refunds
		WHERE refund_type='ai_balance' AND status IN ('pending','unknown','processing','action_required')
		AND ($1='' OR user_email=$1) AND ($1<>'' OR ` + dueSQL + `) ORDER BY updated_at,refund_no`
	if email == "" {
		query += fmt.Sprintf(" LIMIT %d", aiRefundReconcileBatchLimit)
	}
	args := append([]any{email}, dueArgs...)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PaymentRefund{}
	for rows.Next() {
		refund, err := scanPaymentRefund(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, refund)
	}
	return result, rows.Err()
}

// UpdateRefund 更新 PostgreSQL 退款结果。
func (s *PostgresPaymentStore) UpdateRefund(refundNo string, status string, providerRefundID string, responseData string) (PaymentRefund, error) {
	completed := status == "success"
	row := s.db.QueryRow(`
		UPDATE payment_refunds
		SET status=$2, provider_refund_id=$3, response_data=$4::jsonb,
			rights_reserved=CASE WHEN $2 IN ('failed','closed') THEN false ELSE rights_reserved END,
			updated_at=NOW(), completed_at=CASE WHEN $5 THEN NOW() ELSE completed_at END
		WHERE refund_no=$1
		RETURNING `+paymentRefundColumns+`
	`, refundNo, status, providerRefundID, safeJSONText(responseData), completed)
	return scanPaymentRefund(row)
}

// RefundedCentsByOrder 汇总 PostgreSQL 订单仍占用额度的退款金额。
func (s *PostgresPaymentStore) RefundedCentsByOrder(orderNo string) (int, error) {
	var total int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(amount_cents), 0)
		FROM payment_refunds
		WHERE order_no=$1 AND status NOT IN ('failed', 'closed')
	`, orderNo).Scan(&total)
	return total, err
}

// scanPaymentRefund 解析单条退款记录。
func scanPaymentRefund(row interface{ Scan(dest ...any) error }) (PaymentRefund, error) {
	var refund PaymentRefund
	err := row.Scan(&refund.ID, &refund.RefundNo, &refund.OrderNo, &refund.UserEmail,
		&refund.RefundType, &refund.AmountCents, &refund.DeductedDays, &refund.RevokedBonusUnits, &refund.RightsReserved, &refund.Status,
		&refund.ProviderRefundID, &refund.ResponseData, &refund.CreatedAt, &refund.UpdatedAt,
		&refund.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PaymentRefund{}, ErrNotFound
	}
	return refund, err
}

// scanPaymentOrder 解析单条支付记录。
func scanPaymentOrder(row interface{ Scan(dest ...any) error }) (PaymentOrder, error) {
	var order PaymentOrder
	err := row.Scan(
		&order.ID,
		&order.OrderNo,
		&order.OrderType,
		&order.UserEmail,
		&order.PlanID,
		&order.PlanName,
		&order.MemberType,
		&order.DurationDays,
		&order.OriginalAmountCents,
		&order.DiscountAmountCents,
		&order.UpgradeFromType,
		&order.UpgradeCreditCents,
		&order.AmountCents,
		&order.BonusAmountCents,
		&order.PaymentProvider,
		&order.TradeNo,
		&order.Status,
		&order.PaidAt,
		&order.ExpiredAt,
		&order.NotifyData,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PaymentOrder{}, ErrNotFound
	}
	return order, err
}

// scanPaymentOrders 解析多条支付记录。
func scanPaymentOrders(rows *sql.Rows) ([]PaymentOrder, error) {
	result := []PaymentOrder{}
	for rows.Next() {
		order, err := scanPaymentOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, order)
	}
	return result, rows.Err()
}

// safeJSONText 确保写入数据库的回调文本是合法 JSON。
func safeJSONText(value string) string {
	var payload any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return "{}"
	}
	return value
}
