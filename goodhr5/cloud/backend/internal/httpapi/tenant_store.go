// 本文件提供租户管理的数据模型和存储接口。
package httpapi

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Tenant struct {
	ID         string
	Name       string
	OwnerEmail string
	CreatedAt  time.Time
}

type TenantMember struct {
	Email     string
	Role      string
	Status    string
	InvitedBy string
	CreatedAt time.Time
}

type TenantStore interface {
	GetOrCreateTenant(email string) (Tenant, error)
	ListMembers(tenantID string) ([]TenantMember, error)
	InviteMember(tenantID, email, role, invitedBy string) error
	UpdateMemberRole(tenantID, email, role string) error
	RemoveMember(tenantID, email string) error
	IsTenantAdmin(tenantID, email string) (bool, error)
}

// ---------- 内存实现 ----------

type MemoryTenantStore struct {
	mu       sync.Mutex
	tenants  map[string]Tenant
	members  map[string][]TenantMember
	now      func() time.Time
}

func NewMemoryTenantStore() *MemoryTenantStore {
	return &MemoryTenantStore{
		tenants: map[string]Tenant{},
		members: map[string][]TenantMember{},
		now:     time.Now,
	}
}

func (s *MemoryTenantStore) GetOrCreateTenant(email string) (Tenant, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	for _, t := range s.tenants {
		if t.OwnerEmail == email { return t, nil }
	}
	id := fmt.Sprintf("tenant_%s", email)
	t := Tenant{ID: id, Name: email, OwnerEmail: email, CreatedAt: s.now()}
	s.tenants[id] = t
	s.members[id] = []TenantMember{{Email: email, Role: "admin", Status: "active", InvitedBy: "", CreatedAt: s.now()}}
	return t, nil
}

func (s *MemoryTenantStore) ListMembers(tenantID string) ([]TenantMember, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.members[tenantID], nil
}

func (s *MemoryTenantStore) InviteMember(tenantID, email, role, invitedBy string) error {
	s.mu.Lock(); defer s.mu.Unlock()
	for _, m := range s.members[tenantID] {
		if m.Email == email { return errors.New("成员已存在") }
	}
	s.members[tenantID] = append(s.members[tenantID], TenantMember{Email: email, Role: role, Status: "pending", InvitedBy: invitedBy, CreatedAt: s.now()})
	return nil
}

func (s *MemoryTenantStore) UpdateMemberRole(tenantID, email, role string) error {
	s.mu.Lock(); defer s.mu.Unlock()
	for i, m := range s.members[tenantID] {
		if m.Email == email { s.members[tenantID][i].Role = role; return nil }
	}
	return ErrNotFound
}

func (s *MemoryTenantStore) RemoveMember(tenantID, email string) error {
	s.mu.Lock(); defer s.mu.Unlock()
	list := s.members[tenantID]
	for i, m := range list {
		if m.Email == email {
			s.members[tenantID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (s *MemoryTenantStore) IsTenantAdmin(tenantID, email string) (bool, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	for _, m := range s.members[tenantID] {
		if m.Email == email { return m.Role == "admin", nil }
	}
	return false, nil
}

// ---------- PostgreSQL 实现 ----------

type PostgresTenantStore struct {
	db *sql.DB
}

func NewPostgresTenantStore(db *sql.DB) *PostgresTenantStore {
	return &PostgresTenantStore{db: db}
}

func (s *PostgresTenantStore) GetOrCreateTenant(email string) (Tenant, error) {
	var t Tenant
	err := s.db.QueryRow(
		`SELECT t.id, t.name, t.owner_email, t.created_at
		 FROM tenants t JOIN users u ON u.tenant_id = t.id
		 WHERE u.email = $1 LIMIT 1`, email,
	).Scan(&t.ID, &t.Name, &t.OwnerEmail, &t.CreatedAt)

	if err == nil { return t, nil }
	if !errors.Is(err, sql.ErrNoRows) { return Tenant{}, err }

	// 创建新租户
	err = s.db.QueryRow(
		`INSERT INTO tenants (name, owner_email) VALUES ($1, $2) RETURNING id, name, owner_email, created_at`,
		email, email,
	).Scan(&t.ID, &t.Name, &t.OwnerEmail, &t.CreatedAt)
	if err != nil { return Tenant{}, err }

	// 创建用户记录
	_, _ = s.db.Exec(
		`INSERT INTO users (email, tenant_id, role, status) VALUES ($1, $2, 'admin', 'active')
		 ON CONFLICT (email) DO UPDATE SET tenant_id = $2`,
		email, t.ID,
	)
	return t, nil
}

func (s *PostgresTenantStore) ListMembers(tenantID string) ([]TenantMember, error) {
	rows, err := s.db.Query(`SELECT email, role, status, invited_by, created_at FROM users WHERE tenant_id=$1 ORDER BY created_at`, tenantID)
	if err != nil { return nil, err }
	defer rows.Close()
	var members []TenantMember
	for rows.Next() {
		var m TenantMember
		rows.Scan(&m.Email, &m.Role, &m.Status, &m.InvitedBy, &m.CreatedAt)
		members = append(members, m)
	}
	if members == nil { members = []TenantMember{} }
	return members, rows.Err()
}

func (s *PostgresTenantStore) InviteMember(tenantID, email, role, invitedBy string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (email, tenant_id, role, status, invited_by)
		 VALUES ($1, $2, $3, 'pending', $4)
		 ON CONFLICT (email) DO UPDATE SET tenant_id=$2, role=$3, status='pending', invited_by=$4`,
		email, tenantID, role, invitedBy,
	)
	return err
}

func (s *PostgresTenantStore) UpdateMemberRole(tenantID, email, role string) error {
	r, err := s.db.Exec(`UPDATE users SET role=$1 WHERE tenant_id=$2 AND email=$3`, role, tenantID, email)
	if err != nil { return err }
	n, _ := r.RowsAffected()
	if n == 0 { return ErrNotFound }
	return nil
}

func (s *PostgresTenantStore) RemoveMember(tenantID, email string) error {
	r, err := s.db.Exec(`DELETE FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email)
	if err != nil { return err }
	n, _ := r.RowsAffected()
	if n == 0 { return ErrNotFound }
	return nil
}

func (s *PostgresTenantStore) IsTenantAdmin(tenantID, email string) (bool, error) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email).Scan(&role)
	if err != nil { return false, nil }
	return role == "admin", nil
}
