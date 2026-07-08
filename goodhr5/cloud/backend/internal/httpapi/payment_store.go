// 本文件负责提供支付记录存储抽象和内存/PostgreSQL 双实现。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	AmountCents         int        `json:"amount_cents"`
	PaymentProvider     string     `json:"payment_provider"`
	TradeNo             string     `json:"trade_no"`
	Status              string     `json:"status"`
	PaidAt              *time.Time `json:"paid_at,omitempty"`
	ExpiredAt           *time.Time `json:"expired_at,omitempty"`
	NotifyData          string     `json:"-"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
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
}

// MemoryPaymentStore 在内存中保存支付记录，用于未启用 PostgreSQL 的开发环境。
type MemoryPaymentStore struct {
	mu     sync.Mutex
	orders map[string]PaymentOrder
}

// NewMemoryPaymentStore 创建内存支付记录存储。
func NewMemoryPaymentStore() *MemoryPaymentStore {
	return &MemoryPaymentStore{orders: map[string]PaymentOrder{}}
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
			original_amount_cents, discount_amount_cents, amount_cents, payment_provider,
			status, expired_at, notify_data
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, '{}'::jsonb)
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
		order.AmountCents,
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
			original_amount_cents, discount_amount_cents, amount_cents, payment_provider,
			trade_no, status, paid_at, expired_at, notify_data::text, created_at, updated_at
		FROM payment_orders
		WHERE order_no=$1
		`, orderNo))
}

// ListByUser 列出指定用户自己的 PostgreSQL 支付记录。
func (s *PostgresPaymentStore) ListByUser(email string) ([]PaymentOrder, error) {
	rows, err := s.db.Query(`
		SELECT id, order_no, COALESCE(order_type, 'subscription'), user_email, plan_id, plan_name, member_type, duration_days,
			original_amount_cents, discount_amount_cents, amount_cents, payment_provider,
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
			original_amount_cents, discount_amount_cents, amount_cents, payment_provider,
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
		&order.AmountCents,
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
