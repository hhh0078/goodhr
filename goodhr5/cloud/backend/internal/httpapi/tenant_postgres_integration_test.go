// 本文件使用临时 PostgreSQL 验证团队邀请迁移、简历合并、岗位排序和安全移出成员。
package httpapi

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPostgresTenantInvitationMigrationFlow 验证团队邀请在真实数据库中的完整数据搬迁流程。
func TestPostgresTenantInvitationMigrationFlow(t *testing.T) {
	dsn := os.Getenv("GOODHR_TEAM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("GOODHR_TEAM_TEST_PG_DSN is not configured")
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(originalWorkingDirectory) }()
	db, err := (Config{PostgresDSN: dsn}).PostgresDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	suffix := time.Now().Format("150405.000000000")
	ownerEmail := "team-owner-" + suffix + "@example.com"
	memberEmail := "team-member-" + suffix + "@example.com"
	tenantStore := NewPostgresTenantStore(db)
	positionStore := NewPostgresPositionStore(db)
	candidateStore := NewPostgresCandidateStore(db)
	targetTenant, err := tenantStore.GetOrCreateTenant(ownerEmail)
	if err != nil {
		t.Fatal(err)
	}
	sourceTenant, err := tenantStore.GetOrCreateTenant(memberEmail)
	if err != nil {
		t.Fatal(err)
	}
	ownerPosition, err := positionStore.SavePosition(Position{UserEmail: ownerEmail, PlatformID: "boss", Name: "团队岗位"})
	if err != nil {
		t.Fatal(err)
	}
	memberPosition, err := positionStore.SavePosition(Position{UserEmail: memberEmail, PlatformID: "boss", Name: "我的岗位"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE positions SET daily_greeted_count=7, daily_greeted_date=$2::date WHERE id=$1`, memberPosition.ID, positionBusinessDate(time.Now())); err != nil {
		t.Fatal(err)
	}
	if _, err = candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: ownerEmail, PlatformID: "boss", PlatformCandidateID: "candidate-1", CandidateName: "候选人"}); err != nil {
		t.Fatal(err)
	}
	sourceCandidate, err := candidateStore.SaveCandidateProfile(CandidateProfileInput{UserEmail: memberEmail, PlatformID: "boss", PlatformCandidateID: "candidate-1", CandidateName: "候选人", RawText: "需要迁移的简历原文"})
	if err != nil {
		t.Fatal(err)
	}
	engagement, err := candidateStore.UpsertCandidateEngagement(CandidateEngagement{CandidateID: sourceCandidate.ID, UserEmail: memberEmail, PositionID: memberPosition.ID, PlatformID: "boss", Status: "greeted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = candidateStore.SaveCandidateEvent(CandidateEvent{CandidateID: sourceCandidate.ID, EngagementID: engagement.ID, PositionID: memberPosition.ID, PlatformID: "boss", EventType: "greet", Reason: "测试迁移"}); err != nil {
		t.Fatal(err)
	}
	var memberID string
	if err = db.QueryRowContext(ctx, `SELECT id::text FROM users WHERE email=$1`, memberEmail).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO cookie_data (tenant_id,user_id,platform_id,display_name,cookie_type,encrypted_data) VALUES ($1,$2,'boss','测试账号','folder',$3)`, sourceTenant.ID, memberID, []byte("encrypted")); err != nil {
		t.Fatal(err)
	}
	invitation, _, err := tenantStore.InviteMember(targetTenant.ID, memberEmail, "user", ownerEmail)
	if err != nil {
		t.Fatal(err)
	}
	if err = tenantStore.AcceptInvitation(invitation.ID, memberEmail); err != nil {
		t.Fatal(err)
	}
	joinedTenant, err := tenantStore.GetOrCreateTenant(memberEmail)
	if err != nil {
		t.Fatal(err)
	}
	if joinedTenant.ID != targetTenant.ID {
		t.Fatalf("joined tenant = %s, want %s", joinedTenant.ID, targetTenant.ID)
	}
	var sourceTenantCount, candidateCount, eventCount, targetCookieCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM tenants WHERE id=$1`, sourceTenant.ID).Scan(&sourceTenantCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM candidate_profiles WHERE tenant_id=$1 AND source_platform_candidate_id='candidate-1'`, targetTenant.ID).Scan(&candidateCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM candidate_events WHERE tenant_id=$1 AND reason='测试迁移'`, targetTenant.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM cookie_data WHERE tenant_id=$1 AND user_id=$2`, targetTenant.ID, memberID).Scan(&targetCookieCount); err != nil {
		t.Fatal(err)
	}
	if sourceTenantCount != 0 || candidateCount != 1 || eventCount != 1 || targetCookieCount != 1 {
		t.Fatalf("migration counts source=%d candidate=%d event=%d cookie=%d", sourceTenantCount, candidateCount, eventCount, targetCookieCount)
	}
	positions, err := positionStore.ListPositions(targetTenant.ID, memberEmail, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 2 || positions[0].ID != memberPosition.ID || positions[0].UserEmail != memberEmail || positions[1].ID != ownerPosition.ID {
		t.Fatalf("team positions order = %#v", positions)
	}
	members, err := tenantStore.ListMembers(targetTenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberToday := -1
	for _, member := range members {
		if member.Email == memberEmail {
			memberToday = member.TodayGreetedCount
		}
	}
	if memberToday != 7 {
		t.Fatalf("member today greeted = %d, want 7", memberToday)
	}
	if err = tenantStore.RemoveMember(targetTenant.ID, memberEmail); err != nil {
		t.Fatal(err)
	}
	var currentTenantID string
	if err = db.QueryRow(`SELECT tenant_id::text FROM users WHERE id=$1`, memberID).Scan(&currentTenantID); err != nil {
		t.Fatal(err)
	}
	var teamCandidateCount, personalCookieCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM candidate_profiles WHERE tenant_id=$1`, targetTenant.ID).Scan(&teamCandidateCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM cookie_data WHERE tenant_id=$1 AND user_id=$2`, currentTenantID, memberID).Scan(&personalCookieCount); err != nil {
		t.Fatal(err)
	}
	if currentTenantID == targetTenant.ID || teamCandidateCount != 1 || personalCookieCount != 1 {
		t.Fatalf("remove member tenant=%s candidate=%d cookie=%d", currentTenantID, teamCandidateCount, personalCookieCount)
	}
}
