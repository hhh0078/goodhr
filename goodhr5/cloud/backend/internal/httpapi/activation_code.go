// 本文件负责提供会员激活码生成、查询和兑换接口。
package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type createActivationCodesRequest struct {
	Days   int    `json:"days"`
	Remark string `json:"remark"`
	Count  int    `json:"count"`
}

type redeemActivationCodeRequest struct {
	Code string `json:"code"`
}

// ActivationCode 表示一条会员激活码。
type ActivationCode struct {
	ID          string     `json:"id"`
	Code        string     `json:"code"`
	Days        int        `json:"days"`
	Remark      string     `json:"remark"`
	Status      string     `json:"status"`
	UsedByEmail string     `json:"used_by_email"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ActivationCodeStore 定义会员激活码持久化能力。
type ActivationCodeStore interface {
	// CreateBatch 批量保存激活码。
	CreateBatch(codes []ActivationCode) ([]ActivationCode, error)
	// ListAll 列出全部激活码。
	ListAll() ([]ActivationCode, error)
	// Redeem 使用一条未使用的激活码。
	Redeem(code string, userEmail string) (ActivationCode, error)
}

// ActivationCodeService 处理激活码接口。
type ActivationCodeService struct {
	auth          *AuthService
	store         ActivationCodeStore
	subscriptions SubscriptionStore
	mailer        Mailer
}

// NewActivationCodeService 创建激活码服务。
func NewActivationCodeService(auth *AuthService, store ActivationCodeStore, subscriptions SubscriptionStore, mailer Mailer) *ActivationCodeService {
	return &ActivationCodeService{auth: auth, store: store, subscriptions: subscriptions, mailer: mailer}
}

// AdminCollection 按请求方法处理超管激活码列表和生成请求。
func (s *ActivationCodeService) AdminCollection(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if !s.auth.IsSuperAdmin(session.Email) {
		writeError(w, http.StatusForbidden, "super admin access required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.ListAdmin(w, r)
	case http.MethodPost:
		s.CreateAdmin(w, r, session.Email)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ListAdmin 返回超管可见的全部激活码。
func (s *ActivationCodeService) ListAdmin(w http.ResponseWriter, r *http.Request) {
	codes, err := s.store.ListAll()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activation codes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "codes": codes})
}

// CreateAdmin 批量生成激活码。
func (s *ActivationCodeService) CreateAdmin(w http.ResponseWriter, r *http.Request, createdBy string) {
	var req createActivationCodesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Days <= 0 {
		writeError(w, http.StatusBadRequest, "days must be greater than 0")
		return
	}
	if req.Count <= 0 || req.Count > 200 {
		writeError(w, http.StatusBadRequest, "count must be 1-200")
		return
	}
	codes := make([]ActivationCode, 0, req.Count)
	for i := 0; i < req.Count; i += 1 {
		codes = append(codes, ActivationCode{
			Code:      generateActivationCode(),
			Days:      req.Days,
			Remark:    strings.TrimSpace(req.Remark),
			Status:    "unused",
			CreatedBy: createdBy,
		})
	}
	saved, err := s.store.CreateBatch(codes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create activation codes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "codes": saved})
}

// Redeem 兑换当前用户输入的会员激活码。
func (s *ActivationCodeService) Redeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	var req redeemActivationCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	code, err := s.store.Redeem(req.Code, session.Email)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "激活码不存在或已使用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to redeem activation code")
		return
	}
	subscription, err := s.subscriptions.ExtendSubscription(session.Email, defaultMemberType, code.Days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to extend subscription")
		return
	}
	if err := sendSubscriptionRewardNotice(s.mailer, session.Email, SubscriptionRewardNotice{
		Reason:     "激活码兑换成功",
		Days:       code.Days,
		MemberType: subscription.MemberType,
		ExpiresAt:  subscription.ExpiresAt,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to send reward email")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "code": code, "subscription": publicSubscription(subscription)})
}

// generateActivationCode 生成便于复制输入的激活码。
func generateActivationCode() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "GH-" + strings.ToUpper(hex.EncodeToString([]byte(time.Now().Format("150405"))))
	}
	raw := strings.ToUpper(hex.EncodeToString(bytes))
	return "GH-" + raw[:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:16]
}

// MemoryActivationCodeStore 提供开发期内存激活码存储。
type MemoryActivationCodeStore struct {
	mu    sync.Mutex
	codes map[string]ActivationCode
}

// NewMemoryActivationCodeStore 创建内存激活码存储。
func NewMemoryActivationCodeStore() *MemoryActivationCodeStore {
	return &MemoryActivationCodeStore{codes: map[string]ActivationCode{}}
}

// CreateBatch 批量保存内存激活码。
func (s *MemoryActivationCodeStore) CreateBatch(codes []ActivationCode) ([]ActivationCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range codes {
		codes[i].ID = codes[i].Code
		codes[i].CreatedAt = now
		s.codes[codes[i].Code] = codes[i]
	}
	return codes, nil
}

// ListAll 列出全部内存激活码。
func (s *MemoryActivationCodeStore) ListAll() ([]ActivationCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]ActivationCode, 0, len(s.codes))
	for _, code := range s.codes {
		result = append(result, code)
	}
	return result, nil
}

// Redeem 使用内存激活码。
func (s *MemoryActivationCodeStore) Redeem(code string, userEmail string) (ActivationCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized := normalizeActivationCode(code)
	item, ok := s.codes[normalized]
	if !ok || item.Status != "unused" {
		return ActivationCode{}, ErrNotFound
	}
	now := time.Now()
	item.Status = "used"
	item.UsedByEmail = userEmail
	item.UsedAt = &now
	s.codes[normalized] = item
	return item, nil
}

// PostgresActivationCodeStore 使用 PostgreSQL 保存激活码。
type PostgresActivationCodeStore struct {
	db *sql.DB
}

// NewPostgresActivationCodeStore 创建 PostgreSQL 激活码存储。
func NewPostgresActivationCodeStore(db *sql.DB) *PostgresActivationCodeStore {
	return &PostgresActivationCodeStore{db: db}
}

// CreateBatch 批量保存 PostgreSQL 激活码。
func (s *PostgresActivationCodeStore) CreateBatch(codes []ActivationCode) ([]ActivationCode, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	saved := make([]ActivationCode, 0, len(codes))
	for _, code := range codes {
		var item ActivationCode
		err := tx.QueryRow(`
			INSERT INTO activation_codes (code, days, remark, status, created_by)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id::text, code, days, remark, status, used_by_email, used_at, created_by, created_at
			`, code.Code, code.Days, code.Remark, code.Status, code.CreatedBy).Scan(
			&item.ID,
			&item.Code,
			&item.Days,
			&item.Remark,
			&item.Status,
			&item.UsedByEmail,
			&item.UsedAt,
			&item.CreatedBy,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		saved = append(saved, item)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

// ListAll 列出全部 PostgreSQL 激活码。
func (s *PostgresActivationCodeStore) ListAll() ([]ActivationCode, error) {
	rows, err := s.db.Query(`
		SELECT id::text, code, days, remark, status, used_by_email, used_at, created_by, created_at
		FROM activation_codes
		ORDER BY created_at DESC
		LIMIT 1000
		`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ActivationCode{}
	for rows.Next() {
		item, err := scanActivationCode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Redeem 使用 PostgreSQL 激活码。
func (s *PostgresActivationCodeStore) Redeem(code string, userEmail string) (ActivationCode, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return ActivationCode{}, err
	}
	defer tx.Rollback()
	userID, err := ensureUserID(context.Background(), s.db, userEmail)
	if err != nil {
		return ActivationCode{}, err
	}
	var item ActivationCode
	err = tx.QueryRow(`
		UPDATE activation_codes
		SET status='used', used_by=$2::uuid, used_by_email=$3, used_at=now()
		WHERE code=$1 AND status='unused'
		RETURNING id::text, code, days, remark, status, used_by_email, used_at, created_by, created_at
		`, normalizeActivationCode(code), userID, userEmail).Scan(
		&item.ID,
		&item.Code,
		&item.Days,
		&item.Remark,
		&item.Status,
		&item.UsedByEmail,
		&item.UsedAt,
		&item.CreatedBy,
		&item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ActivationCode{}, ErrNotFound
	}
	if err != nil {
		return ActivationCode{}, err
	}
	if err := tx.Commit(); err != nil {
		return ActivationCode{}, err
	}
	return item, nil
}

// scanActivationCode 解析一条激活码记录。
func scanActivationCode(row interface{ Scan(dest ...any) error }) (ActivationCode, error) {
	var item ActivationCode
	err := row.Scan(
		&item.ID,
		&item.Code,
		&item.Days,
		&item.Remark,
		&item.Status,
		&item.UsedByEmail,
		&item.UsedAt,
		&item.CreatedBy,
		&item.CreatedAt,
	)
	return item, err
}

// normalizeActivationCode 规范化用户输入的激活码。
func normalizeActivationCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
