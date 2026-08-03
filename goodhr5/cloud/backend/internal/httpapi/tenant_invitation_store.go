// 本文件负责团队邀请的创建、重邀、确认、拒绝和数据迁移存储逻辑。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PendingInvitations 返回内存中指定邮箱尚未处理的团队邀请。
func (s *MemoryTenantStore) PendingInvitations(email string) ([]TenantInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]TenantInvitation, 0)
	for _, invitation := range s.invitations {
		if invitation.Status == "pending" && strings.EqualFold(invitation.InviteeEmail, email) {
			items = append(items, invitation)
		}
	}
	return items, nil
}

// AcceptInvitation 在内存模式下接受邀请并切换用户的团队成员关系。
func (s *MemoryTenantStore) AcceptInvitation(invitationID, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.Status != "pending" || !strings.EqualFold(invitation.InviteeEmail, email) {
		return ErrNotFound
	}
	for tenantID, members := range s.members {
		for index, member := range members {
			if !strings.EqualFold(member.Email, email) {
				continue
			}
			if tenantID == invitation.TenantID {
				invitation.Status = "accepted"
				invitation.UpdatedAt = s.now()
				s.invitations[invitationID] = invitation
				return nil
			}
			if len(members) > 1 {
				return ErrCannotMoveSharedTenant
			}
			s.members[tenantID] = append(members[:index], members[index+1:]...)
		}
	}
	target := s.members[invitation.TenantID]
	target = append(target, TenantMember{Email: email, Role: invitation.Role, Status: "active", InvitedBy: invitation.InvitedByEmail, CreatedAt: s.now(), Registered: true})
	s.members[invitation.TenantID] = target
	invitation.Status = "accepted"
	invitation.UpdatedAt = s.now()
	s.invitations[invitationID] = invitation
	return nil
}

// RejectInvitation 在内存模式下记录用户拒绝团队邀请。
func (s *MemoryTenantStore) RejectInvitation(invitationID, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.Status != "pending" || !strings.EqualFold(invitation.InviteeEmail, email) {
		return ErrNotFound
	}
	invitation.Status = "rejected"
	invitation.UpdatedAt = s.now()
	s.invitations[invitationID] = invitation
	return nil
}

// UpdateInvitationRole 在内存模式下修改待确认邀请的成员角色。
func (s *MemoryTenantStore) UpdateInvitationRole(tenantID, invitationID, role string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.TenantID != tenantID || invitation.Status != "pending" {
		return ErrNotFound
	}
	invitation.Role = normalizeTenantRole(role)
	invitation.UpdatedAt = s.now()
	s.invitations[invitationID] = invitation
	return nil
}

// CancelInvitation 在内存模式下取消一条待确认邀请。
func (s *MemoryTenantStore) CancelInvitation(tenantID, invitationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitation, ok := s.invitations[invitationID]
	if !ok || invitation.TenantID != tenantID || invitation.Status != "pending" {
		return ErrNotFound
	}
	invitation.Status = "canceled"
	invitation.UpdatedAt = s.now()
	s.invitations[invitationID] = invitation
	return nil
}

// MarkInvitationEmailSent 在内存模式下记录邀请邮件发送完成时间。
func (s *MemoryTenantStore) MarkInvitationEmailSent(invitationID string, sentAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invitation, ok := s.invitations[invitationID]
	if !ok {
		return ErrNotFound
	}
	invitation.UpdatedAt = sentAt
	s.invitations[invitationID] = invitation
	return nil
}

// InviteMember 创建 PostgreSQL 团队邀请；同一团队重复邀请时复用待确认记录并重发邮件。
func (s *PostgresTenantStore) InviteMember(tenantID, email, role, invitedBy string) (TenantInvitation, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	email = strings.ToLower(strings.TrimSpace(email))
	role = normalizeTenantRole(role)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TenantInvitation{}, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, email); err != nil {
		return TenantInvitation{}, false, err
	}
	var memberExists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE tenant_id=$1 AND LOWER(email)=LOWER($2) AND status='active')`, tenantID, email).Scan(&memberExists); err != nil {
		return TenantInvitation{}, false, err
	}
	if memberExists {
		return TenantInvitation{}, false, ErrAlreadyTeamMember
	}
	var existing TenantInvitation
	err = tx.QueryRowContext(ctx, `
		SELECT invitation.id::text, invitation.tenant_id::text, tenant.name, tenant.owner_email,
		       invitation.invitee_email, invitation.role, invitation.status,
		       invitation.invited_by_email, invitation.created_at, invitation.updated_at
		FROM tenant_invitations invitation
		INNER JOIN tenants tenant ON tenant.id = invitation.tenant_id
		WHERE LOWER(invitation.invitee_email)=LOWER($1) AND invitation.status='pending'
		FOR UPDATE OF invitation
	`, email).Scan(&existing.ID, &existing.TenantID, &existing.TenantName, &existing.TenantOwner, &existing.InviteeEmail, &existing.Role, &existing.Status, &existing.InvitedByEmail, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		if existing.TenantID != tenantID {
			return TenantInvitation{}, false, ErrInvitationPendingElsewhere
		}
		err = tx.QueryRowContext(ctx, `
			UPDATE tenant_invitations
			SET role=$2,
			    invited_by_user_id=(SELECT id FROM users WHERE LOWER(email)=LOWER($3) LIMIT 1),
			    invited_by_email=$3,
			    updated_at=now()
			WHERE id=$1
			RETURNING role, invited_by_email, updated_at
		`, existing.ID, role, invitedBy).Scan(&existing.Role, &existing.InvitedByEmail, &existing.UpdatedAt)
		if err != nil {
			return TenantInvitation{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return TenantInvitation{}, false, err
		}
		existing.TenantName = tenantDisplayName(existing.TenantName, existing.TenantOwner)
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TenantInvitation{}, false, err
	}
	var invitation TenantInvitation
	err = tx.QueryRowContext(ctx, `
		INSERT INTO tenant_invitations (
			tenant_id, invitee_email, role, invited_by_user_id, invited_by_email
		)
		VALUES (
			$1, $2, $3, (SELECT id FROM users WHERE LOWER(email)=LOWER($4) LIMIT 1), $4
		)
		RETURNING id::text, tenant_id::text, invitee_email, role, status, invited_by_email, created_at, updated_at
	`, tenantID, email, role, invitedBy).Scan(&invitation.ID, &invitation.TenantID, &invitation.InviteeEmail, &invitation.Role, &invitation.Status, &invitation.InvitedByEmail, &invitation.CreatedAt, &invitation.UpdatedAt)
	if err != nil {
		return TenantInvitation{}, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT name, owner_email FROM tenants WHERE id=$1`, tenantID).Scan(&invitation.TenantName, &invitation.TenantOwner); err != nil {
		return TenantInvitation{}, false, err
	}
	invitation.TenantName = tenantDisplayName(invitation.TenantName, invitation.TenantOwner)
	if err = tx.Commit(); err != nil {
		return TenantInvitation{}, false, err
	}
	return invitation, false, nil
}

// PendingInvitations 返回 PostgreSQL 中当前邮箱尚未处理的邀请。
func (s *PostgresTenantStore) PendingInvitations(email string) ([]TenantInvitation, error) {
	rows, err := s.db.Query(`
		SELECT invitation.id::text, invitation.tenant_id::text, tenant.name, tenant.owner_email,
		       invitation.invitee_email, invitation.role, invitation.status,
		       invitation.invited_by_email, invitation.created_at, invitation.updated_at
		FROM tenant_invitations invitation
		INNER JOIN tenants tenant ON tenant.id = invitation.tenant_id
		WHERE LOWER(invitation.invitee_email)=LOWER($1) AND invitation.status='pending'
		ORDER BY invitation.created_at
	`, strings.TrimSpace(email))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]TenantInvitation, 0)
	for rows.Next() {
		var item TenantInvitation
		if err := rows.Scan(&item.ID, &item.TenantID, &item.TenantName, &item.TenantOwner, &item.InviteeEmail, &item.Role, &item.Status, &item.InvitedByEmail, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.TenantName = tenantDisplayName(item.TenantName, item.TenantOwner)
		items = append(items, item)
	}
	return items, rows.Err()
}

// AcceptInvitation 接受 PostgreSQL 团队邀请，并在同一事务内迁移个人租户数据。
func (s *PostgresTenantStore) AcceptInvitation(invitationID, email string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var targetTenantID, inviteeEmail, role, invitedByEmail string
	err = tx.QueryRowContext(ctx, `
		SELECT tenant_id::text, invitee_email, role, invited_by_email
		FROM tenant_invitations
		WHERE id::text=$1 AND status='pending' AND LOWER(invitee_email)=LOWER($2)
		FOR UPDATE
	`, invitationID, email).Scan(&targetTenantID, &inviteeEmail, &role, &invitedByEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var userID, sourceTenantID string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, COALESCE(tenant_id::text,'')
		FROM users
		WHERE LOWER(email)=LOWER($1)
		FOR UPDATE
	`, inviteeEmail).Scan(&userID, &sourceTenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sourceTenantID != "" && sourceTenantID != targetTenantID {
		var otherMembers, pendingInvitations, runningPositions int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id=$1 AND id::text<>$2 AND status='active'`, sourceTenantID, userID).Scan(&otherMembers); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_invitations WHERE tenant_id=$1 AND status='pending'`, sourceTenantID).Scan(&pendingInvitations); err != nil {
			return err
		}
		if otherMembers > 0 || pendingInvitations > 0 {
			return ErrCannotMoveSharedTenant
		}
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM positions WHERE user_id=$1 AND status='running'`, userID).Scan(&runningPositions); err != nil {
			return err
		}
		if runningPositions > 0 {
			return ErrPositionRunning
		}
		if err = mergeCandidateTenantData(ctx, tx, sourceTenantID, targetTenantID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cookie_data SET tenant_id=$1 WHERE tenant_id=$2`, targetTenantID, sourceTenantID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET tenant_id=$1, role=$2, status='active', invited_by=$3, tenant_joined_at=now() WHERE id=$4`, targetTenantID, normalizeTenantRole(role), invitedByEmail, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE tenant_invitations SET status='accepted', responded_at=now(), updated_at=now() WHERE id::text=$1`, invitationID); err != nil {
		return err
	}
	if sourceTenantID != "" && sourceTenantID != targetTenantID {
		if _, err = tx.ExecContext(ctx, `DELETE FROM tenants WHERE id=$1`, sourceTenantID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// mergeCandidateTenantData 合并个人租户和目标团队重复的候选人，再迁移简历三张表。
func mergeCandidateTenantData(ctx context.Context, tx *sql.Tx, sourceTenantID, targetTenantID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT source.id::text, target.id::text
		FROM candidate_profiles source
		INNER JOIN candidate_profiles target
		  ON target.tenant_id=$2
		 AND target.source_platform_id=source.source_platform_id
		 AND target.source_platform_candidate_id=source.source_platform_candidate_id
		WHERE source.tenant_id=$1
	`, sourceTenantID, targetTenantID)
	if err != nil {
		return err
	}
	pairs := make([][2]string, 0)
	for rows.Next() {
		var pair [2]string
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, pair)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, pair := range pairs {
		if _, err = tx.ExecContext(ctx, `
			UPDATE candidate_profiles target
			SET colleague_communications = COALESCE(target.colleague_communications, '[]'::jsonb) || COALESCE(source.colleague_communications, '[]'::jsonb),
			    first_seen_at = LEAST(target.first_seen_at, source.first_seen_at),
			    updated_at = GREATEST(target.updated_at, source.updated_at)
			FROM candidate_profiles source
			WHERE target.id=$2 AND source.id=$1
		`, pair[0], pair[1]); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE candidate_events SET tenant_id=$3, candidate_id=$2 WHERE candidate_id=$1`, pair[0], pair[1], targetTenantID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE candidate_engagements SET tenant_id=$3, candidate_id=$2 WHERE candidate_id=$1`, pair[0], pair[1], targetTenantID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_profiles WHERE id=$1`, pair[0]); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_events SET tenant_id=$1 WHERE tenant_id=$2`, targetTenantID, sourceTenantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_engagements SET tenant_id=$1 WHERE tenant_id=$2`, targetTenantID, sourceTenantID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_profiles SET tenant_id=$1 WHERE tenant_id=$2`, targetTenantID, sourceTenantID); err != nil {
		return err
	}
	return nil
}

// RejectInvitation 记录 PostgreSQL 用户拒绝邀请，后续允许重新发起新邀请。
func (s *PostgresTenantStore) RejectInvitation(invitationID, email string) error {
	result, err := s.db.Exec(`UPDATE tenant_invitations SET status='rejected', responded_at=now(), updated_at=now() WHERE id::text=$1 AND LOWER(invitee_email)=LOWER($2) AND status='pending'`, invitationID, email)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateInvitationRole 修改 PostgreSQL 待确认邀请的成员角色。
func (s *PostgresTenantStore) UpdateInvitationRole(tenantID, invitationID, role string) error {
	result, err := s.db.Exec(`UPDATE tenant_invitations SET role=$3, updated_at=now() WHERE tenant_id=$1 AND id::text=$2 AND status='pending'`, tenantID, invitationID, normalizeTenantRole(role))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// CancelInvitation 取消 PostgreSQL 待确认邀请，取消后允许重新邀请。
func (s *PostgresTenantStore) CancelInvitation(tenantID, invitationID string) error {
	result, err := s.db.Exec(`UPDATE tenant_invitations SET status='canceled', responded_at=now(), updated_at=now() WHERE tenant_id=$1 AND id::text=$2 AND status='pending'`, tenantID, invitationID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkInvitationEmailSent 记录 PostgreSQL 邀请邮件最近一次发送成功时间。
func (s *PostgresTenantStore) MarkInvitationEmailSent(invitationID string, sentAt time.Time) error {
	_, err := s.db.Exec(`UPDATE tenant_invitations SET email_sent_at=$2, updated_at=$2 WHERE id::text=$1`, invitationID, sentAt)
	return err
}
