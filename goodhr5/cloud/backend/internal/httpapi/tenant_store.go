// 本文件提供租户管理的数据模型和存储接口。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	InvitationID      string    `json:"invitation_id,omitempty"`
	Email             string    `json:"email"`
	Role              string    `json:"role"`
	Status            string    `json:"status"`
	InvitedBy         string    `json:"invited_by"`
	CreatedAt         time.Time `json:"created_at"`
	TodayGreetedCount int       `json:"today_greeted_count"`
	Registered        bool      `json:"registered"`
	IsOwner           bool      `json:"is_owner"`
}

// TenantInvitation 表示一条等待用户本人处理的团队邀请。
type TenantInvitation struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	TenantOwner    string    `json:"tenant_owner"`
	InviteeEmail   string    `json:"invitee_email"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	InvitedByEmail string    `json:"invited_by_email"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

var (
	ErrInvitationPendingElsewhere = errors.New("对方已经有一条待确认的团队邀请，请先处理后再试")
	ErrAlreadyTeamMember          = errors.New("对方已经在这个团队里了")
	ErrCannotMoveSharedTenant     = errors.New("当前账号还在另一个多人团队里，暂时不能直接搬家")
	ErrPositionRunning            = errors.New("当前账号还有岗位正在运行，请先停止后再加入团队")
	ErrTenantOwner                = errors.New("团队所有者不能被修改或移出")
)

type TenantStore interface {
	GetOrCreateTenant(email string) (Tenant, error)
	ListMembers(tenantID string) ([]TenantMember, error)
	InviteMember(tenantID, email, role, invitedBy string) (TenantInvitation, bool, error)
	PendingInvitations(email string) ([]TenantInvitation, error)
	AcceptInvitation(invitationID, email string) error
	RejectInvitation(invitationID, email string) error
	UpdateInvitationRole(tenantID, invitationID, role string) error
	CancelInvitation(tenantID, invitationID string) error
	MarkInvitationEmailSent(invitationID string, sentAt time.Time) error
	UpdateMemberRole(tenantID, email, role string) error
	RemoveMember(tenantID, email string) error
	IsTenantAdmin(tenantID, email string) (bool, error)
	GetCookieSharing(tenantID string) (bool, error)
	SetCookieSharing(tenantID string, enabled bool) error
}

// ---------- 内存实现 ----------

type MemoryTenantStore struct {
	mu          sync.Mutex
	tenants     map[string]Tenant
	members     map[string][]TenantMember
	invitations map[string]TenantInvitation
	now         func() time.Time
	nextInvite  func() string
}

func NewMemoryTenantStore() *MemoryTenantStore {
	sequence := 0
	return &MemoryTenantStore{
		tenants:     map[string]Tenant{},
		members:     map[string][]TenantMember{},
		invitations: map[string]TenantInvitation{},
		now:         time.Now,
		nextInvite: func() string {
			sequence++
			return fmt.Sprintf("tenant_invitation_%d", sequence)
		},
	}
}

func (s *MemoryTenantStore) GetOrCreateTenant(email string) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	for tenantID, members := range s.members {
		for _, member := range members {
			if strings.EqualFold(member.Email, email) && member.Status == "active" {
				return s.tenants[tenantID], nil
			}
		}
	}
	for _, t := range s.tenants {
		if strings.EqualFold(t.OwnerEmail, email) {
			return t, nil
		}
	}
	id := fmt.Sprintf("tenant_%s", email)
	t := Tenant{ID: id, Name: email, OwnerEmail: email, CreatedAt: s.now()}
	s.tenants[id] = t
	s.members[id] = []TenantMember{{Email: email, Role: "admin", Status: "active", CreatedAt: s.now(), Registered: true, IsOwner: true}}
	return t, nil
}

func (s *MemoryTenantStore) ListMembers(tenantID string) ([]TenantMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]TenantMember(nil), s.members[tenantID]...)
	for _, invitation := range s.invitations {
		if invitation.TenantID == tenantID && invitation.Status == "pending" {
			items = append(items, TenantMember{InvitationID: invitation.ID, Email: invitation.InviteeEmail, Role: invitation.Role, Status: "pending", InvitedBy: invitation.InvitedByEmail, CreatedAt: invitation.CreatedAt})
		}
	}
	return items, nil
}

func (s *MemoryTenantStore) InviteMember(tenantID, email, role, invitedBy string) (TenantInvitation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	for _, member := range s.members[tenantID] {
		if strings.EqualFold(member.Email, email) {
			return TenantInvitation{}, false, ErrAlreadyTeamMember
		}
	}
	for id, invitation := range s.invitations {
		if invitation.Status != "pending" || !strings.EqualFold(invitation.InviteeEmail, email) {
			continue
		}
		if invitation.TenantID != tenantID {
			return TenantInvitation{}, false, ErrInvitationPendingElsewhere
		}
		invitation.Role = normalizeTenantRole(role)
		invitation.InvitedByEmail = invitedBy
		invitation.UpdatedAt = s.now()
		s.invitations[id] = invitation
		return invitation, true, nil
	}
	tenant := s.tenants[tenantID]
	now := s.now()
	invitation := TenantInvitation{ID: s.nextInvite(), TenantID: tenantID, TenantName: tenantDisplayName(tenant.Name, tenant.OwnerEmail), TenantOwner: tenant.OwnerEmail, InviteeEmail: email, Role: normalizeTenantRole(role), Status: "pending", InvitedByEmail: invitedBy, CreatedAt: now, UpdatedAt: now}
	s.invitations[invitation.ID] = invitation
	return invitation, false, nil
}

func (s *MemoryTenantStore) UpdateMemberRole(tenantID, email, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	role = normalizeTenantRole(role)
	for i, m := range s.members[tenantID] {
		if strings.EqualFold(m.Email, email) {
			if m.IsOwner {
				return ErrTenantOwner
			}
			s.members[tenantID][i].Role = role
			return nil
		}
	}
	return ErrNotFound
}

func (s *MemoryTenantStore) RemoveMember(tenantID, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.members[tenantID]
	for i, m := range list {
		if strings.EqualFold(m.Email, email) {
			if m.IsOwner {
				return ErrTenantOwner
			}
			s.members[tenantID] = append(list[:i], list[i+1:]...)
			personalID := fmt.Sprintf("tenant_%s_%d", m.Email, s.now().UnixNano())
			personal := Tenant{ID: personalID, Name: m.Email, OwnerEmail: m.Email, CreatedAt: s.now()}
			s.tenants[personalID] = personal
			s.members[personalID] = []TenantMember{{Email: m.Email, Role: "admin", Status: "active", CreatedAt: s.now(), Registered: true, IsOwner: true}}
			return nil
		}
	}
	return ErrNotFound
}

func (s *MemoryTenantStore) IsTenantAdmin(tenantID, email string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members[tenantID] {
		if strings.EqualFold(m.Email, email) {
			return m.Status == "active" && m.Role == "admin", nil
		}
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
	email = strings.ToLower(strings.TrimSpace(email))
	var t Tenant
	err := s.db.QueryRow(
		`SELECT t.id, t.name, t.owner_email, t.created_at
		 FROM tenants t JOIN users u ON u.tenant_id = t.id
		 WHERE u.email = $1 LIMIT 1`, email,
	).Scan(&t.ID, &t.Name, &t.OwnerEmail, &t.CreatedAt)

	if err == nil {
		return t, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Tenant{}, err
	}

	// 创建新租户
	err = s.db.QueryRow(
		`INSERT INTO tenants (name, owner_email) VALUES ($1, $2) RETURNING id, name, owner_email, created_at`,
		email, email,
	).Scan(&t.ID, &t.Name, &t.OwnerEmail, &t.CreatedAt)
	if err != nil {
		return Tenant{}, err
	}

	// 创建用户记录
	_, _ = s.db.Exec(
		`INSERT INTO users (email, tenant_id, role, status, tenant_joined_at) VALUES ($1, $2, 'admin', 'active', now())
		 ON CONFLICT (email) DO UPDATE SET tenant_id = $2, role = 'admin', status = 'active', tenant_joined_at = now()`,
		email, t.ID,
	)
	return t, nil
}

func (s *PostgresTenantStore) ListMembers(tenantID string) ([]TenantMember, error) {
	rows, err := s.db.Query(`
		SELECT '', u.email, u.role, 'active', u.invited_by,
		       COALESCE(u.tenant_joined_at, u.created_at),
		       COALESCE(position_stats.today_greeted_count, 0)::int,
		       true,
		       LOWER(u.email) = LOWER(t.owner_email)
		FROM users u
		INNER JOIN tenants t ON t.id = u.tenant_id
		LEFT JOIN (
			SELECT user_id, SUM(daily_greeted_count)::int AS today_greeted_count
			FROM positions
			WHERE daily_greeted_date = $2::date
			GROUP BY user_id
		) position_stats ON position_stats.user_id = u.id
		WHERE u.tenant_id = $1 AND u.status = 'active'
		UNION ALL
		SELECT invitation.id::text, invitation.invitee_email, invitation.role, invitation.status,
		       invitation.invited_by_email, invitation.created_at, 0,
		       EXISTS (
			   SELECT 1 FROM users registered
			   WHERE LOWER(registered.email) = LOWER(invitation.invitee_email)
			     AND registered.last_login_at IS NOT NULL
		       ),
		       false
		FROM tenant_invitations invitation
		WHERE invitation.tenant_id = $1 AND invitation.status = 'pending'
		ORDER BY 9 DESC, 4, 6
	`, tenantID, positionBusinessDate(time.Now()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []TenantMember
	for rows.Next() {
		var m TenantMember
		if err := rows.Scan(&m.InvitationID, &m.Email, &m.Role, &m.Status, &m.InvitedBy, &m.CreatedAt, &m.TodayGreetedCount, &m.Registered, &m.IsOwner); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if members == nil {
		members = []TenantMember{}
	}
	return members, rows.Err()
}

func (s *PostgresTenantStore) UpdateMemberRole(tenantID, email, role string) error {
	role = normalizeTenantRole(role)
	r, err := s.db.Exec(`
		UPDATE users member
		SET role=$1
		FROM tenants tenant
		WHERE member.tenant_id=$2 AND LOWER(member.email)=LOWER($3)
		  AND tenant.id=member.tenant_id
		  AND LOWER(member.email) <> LOWER(tenant.owner_email)
	`, role, tenantID, email)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresTenantStore) RemoveMember(tenantID, email string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID, memberEmail, ownerEmail string
	err = tx.QueryRowContext(ctx, `
		SELECT member.id::text, member.email, tenant.owner_email
		FROM users member
		INNER JOIN tenants tenant ON tenant.id = member.tenant_id
		WHERE member.tenant_id=$1 AND LOWER(member.email)=LOWER($2)
		FOR UPDATE OF member, tenant
	`, tenantID, email).Scan(&userID, &memberEmail, &ownerEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(memberEmail, ownerEmail) {
		return ErrTenantOwner
	}
	var personalTenantID string
	err = tx.QueryRowContext(ctx, `INSERT INTO tenants (name, owner_email) VALUES ($1,$1) RETURNING id::text`, memberEmail).Scan(&personalTenantID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cookie_data SET tenant_id=$1 WHERE user_id=$2`, personalTenantID, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET tenant_id=$1, role='admin', status='active', invited_by='', tenant_joined_at=now() WHERE id=$2`, personalTenantID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresTenantStore) IsTenantAdmin(tenantID, email string) (bool, error) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM users WHERE tenant_id=$1 AND LOWER(email)=LOWER($2) AND status='active'`, tenantID, email).Scan(&role)
	if err != nil {
		return false, nil
	}
	return role == "admin", nil
}

func (s *PostgresTenantStore) GetCookieSharing(tenantID string) (bool, error) {
	var enabled bool
	err := s.db.QueryRow(`SELECT cookie_sharing_enabled FROM tenants WHERE id=$1`, tenantID).Scan(&enabled)
	if err != nil {
		return true, nil
	}
	return enabled, nil
}
func (s *PostgresTenantStore) SetCookieSharing(tenantID string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE tenants SET cookie_sharing_enabled=$1 WHERE id=$2`, enabled, tenantID)
	return err
}
func (s *MemoryTenantStore) GetCookieSharing(tenantID string) (bool, error)       { return true, nil }
func (s *MemoryTenantStore) SetCookieSharing(tenantID string, enabled bool) error { return nil }

// normalizeTenantRole 把团队角色收敛为后端支持的管理员或普通成员。
func normalizeTenantRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return "admin"
	}
	return "user"
}

// tenantDisplayName 返回适合弹框和邮件展示的团队名称。
func tenantDisplayName(name, ownerEmail string) string {
	name = strings.TrimSpace(name)
	ownerEmail = strings.TrimSpace(ownerEmail)
	if name == "" || strings.EqualFold(name, ownerEmail) {
		return ownerEmail + " 的团队"
	}
	return name
}
