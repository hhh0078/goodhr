// 本文件测试团队邀请、重邀、接受、拒绝和安全移出成员的核心规则。
package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestMemoryTenantInvitationLifecycle 验证拒绝后可以重邀，接受后才真正加入团队。
func TestMemoryTenantInvitationLifecycle(t *testing.T) {
	store := NewMemoryTenantStore()
	owner := "owner@example.com"
	invitee := "worker@example.com"
	tenant, err := store.GetOrCreateTenant(owner)
	if err != nil {
		t.Fatal(err)
	}
	first, resent, err := store.InviteMember(tenant.ID, invitee, "user", owner)
	if err != nil || resent {
		t.Fatalf("first invitation = %#v, resent=%v, err=%v", first, resent, err)
	}
	if _, err = store.GetOrCreateTenant(invitee); err != nil {
		t.Fatal(err)
	}
	if err = store.RejectInvitation(first.ID, invitee); err != nil {
		t.Fatal(err)
	}
	second, resent, err := store.InviteMember(tenant.ID, invitee, "admin", owner)
	if err != nil || resent || second.ID == first.ID {
		t.Fatalf("second invitation = %#v, resent=%v, err=%v", second, resent, err)
	}
	if err = store.AcceptInvitation(second.ID, invitee); err != nil {
		t.Fatal(err)
	}
	joinedTenant, err := store.GetOrCreateTenant(invitee)
	if err != nil {
		t.Fatal(err)
	}
	if joinedTenant.ID != tenant.ID {
		t.Fatalf("joined tenant = %s, want %s", joinedTenant.ID, tenant.ID)
	}
	members, err := store.ListMembers(tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, member := range members {
		if member.Email == invitee && member.Status == "active" && member.Role == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("accepted member missing: %#v", members)
	}
}

// TestMemoryTenantInvitationResend 验证同一条待确认邀请会被复用并允许重发。
func TestMemoryTenantInvitationResend(t *testing.T) {
	store := NewMemoryTenantStore()
	tenant, _ := store.GetOrCreateTenant("owner@example.com")
	first, _, err := store.InviteMember(tenant.ID, "worker@example.com", "user", "owner@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, resent, err := store.InviteMember(tenant.ID, "worker@example.com", "admin", "owner@example.com")
	if err != nil || !resent || first.ID != second.ID || second.Role != "admin" {
		t.Fatalf("resend invitation = %#v, resent=%v, err=%v", second, resent, err)
	}
}

// TestMemoryTenantRejectsMovingSharedTeam 验证已有其他成员的团队不能被整体搬进新团队。
func TestMemoryTenantRejectsMovingSharedTeam(t *testing.T) {
	store := NewMemoryTenantStore()
	source, _ := store.GetOrCreateTenant("source-owner@example.com")
	memberInvitation, _, _ := store.InviteMember(source.ID, "source-member@example.com", "user", "source-owner@example.com")
	_, _ = store.GetOrCreateTenant("source-member@example.com")
	if err := store.AcceptInvitation(memberInvitation.ID, "source-member@example.com"); err != nil {
		t.Fatal(err)
	}
	target, _ := store.GetOrCreateTenant("target-owner@example.com")
	ownerInvitation, _, _ := store.InviteMember(target.ID, "source-owner@example.com", "user", "target-owner@example.com")
	if err := store.AcceptInvitation(ownerInvitation.ID, "source-owner@example.com"); !errors.Is(err, ErrCannotMoveSharedTenant) {
		t.Fatalf("accept shared tenant err = %v", err)
	}
}

// TestMemoryTenantRemoveMemberKeepsAccount 验证移出团队后账号仍拥有新的个人团队。
func TestMemoryTenantRemoveMemberKeepsAccount(t *testing.T) {
	store := NewMemoryTenantStore()
	tenant, _ := store.GetOrCreateTenant("owner@example.com")
	invitation, _, _ := store.InviteMember(tenant.ID, "worker@example.com", "user", "owner@example.com")
	_, _ = store.GetOrCreateTenant("worker@example.com")
	if err := store.AcceptInvitation(invitation.ID, "worker@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveMember(tenant.ID, "worker@example.com"); err != nil {
		t.Fatal(err)
	}
	personal, err := store.GetOrCreateTenant("worker@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if personal.ID == tenant.ID || personal.OwnerEmail != "worker@example.com" {
		t.Fatalf("personal tenant = %#v", personal)
	}
}

// TestTenantMemberJSONUsesFrontendFields 验证团队成员响应使用前端读取的小写字段。
func TestTenantMemberJSONUsesFrontendFields(t *testing.T) {
	body, err := json.Marshal(TenantMember{Email: "worker@example.com", Role: "user", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"email":"worker@example.com"`) || strings.Contains(text, `"Email"`) {
		t.Fatalf("member json = %s", text)
	}
}
