// 本文件负责验证候选人删除时会同步清理内存触达和事件数据。
package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMemoryCandidateStoreRequiresTenantOnSave 验证内存候选人新增和更新都必须显式携带团队。
func TestMemoryCandidateStoreRequiresTenantOnSave(t *testing.T) {
	store := NewMemoryCandidateStore()
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-without-tenant", CandidateName: "无团队候选人",
	}); err == nil {
		t.Fatal("缺少团队的候选人新增不应成功")
	}
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-tenant-a", TenantID: "tenant-a", CandidateName: "原候选人",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-tenant-a", CandidateName: "不应覆盖",
	}); err == nil {
		t.Fatal("缺少团队的候选人更新不应成功")
	}
	if got := store.profiles["candidate-tenant-a"].CandidateName; got != "原候选人" {
		t.Fatalf("失败更新覆盖了原候选人：%q", got)
	}
}

// TestMemoryCandidateStoreDeleteCandidate 验证单个候选人删除不会留下触达和事件记录。
func TestMemoryCandidateStoreDeleteCandidate(t *testing.T) {
	store := NewMemoryCandidateStore()
	candidate, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID:   "candidate-delete-test",
		TenantID:      "tenant-delete-test",
		CandidateName: "测试候选人",
		AvatarURL:     candidateAvatarPublicPrefix + strings.Repeat("a", 64) + ".png",
	})
	if err != nil {
		t.Fatal(err)
	}
	engagement, err := store.UpsertCandidateEngagement(CandidateEngagement{
		ID:          "engagement-delete-test",
		CandidateID: candidate.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCandidateEvent(CandidateEvent{
		CandidateID:  candidate.ID,
		EngagementID: engagement.ID,
		EventType:    "manual_note",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := store.DeleteCandidate("tenant-delete-test", candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 || len(result.AvatarURLs) != 1 || len(store.engagements) != 0 || len(store.events) != 0 {
		t.Fatalf("delete result=%+v engagements=%d events=%d", result, len(store.engagements), len(store.events))
	}
	if _, err = store.GetPositionCandidate("tenant-delete-test", candidate.ID, "", "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted candidate error=%v", err)
	}
}

// TestMemoryCandidateStoreTenantIsolation 验证内存回退的保存、列表、详情、删除和清空都严格限定团队。
func TestMemoryCandidateStoreTenantIsolation(t *testing.T) {
	store := NewMemoryCandidateStore()
	tenantA, tenantB := "tenant-a", "tenant-b"
	candidateA, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-a", TenantID: tenantA, CandidateName: "团队甲候选人",
		AvatarURL: candidateAvatarPublicPrefix + strings.Repeat("c", 64) + ".png",
	})
	if err != nil {
		t.Fatal(err)
	}
	candidateB, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: "candidate-b", TenantID: tenantB, CandidateName: "团队乙候选人",
		AvatarURL: candidateAvatarPublicPrefix + strings.Repeat("d", 64) + ".png",
	})
	if err != nil {
		t.Fatal(err)
	}
	engagementA, err := store.UpsertCandidateEngagement(CandidateEngagement{CandidateID: candidateA.ID})
	if err != nil {
		t.Fatal(err)
	}
	engagementB, err := store.UpsertCandidateEngagement(CandidateEngagement{CandidateID: candidateB.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCandidateEvent(CandidateEvent{CandidateID: candidateA.ID, EngagementID: engagementA.ID, EventType: "manual_note"}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCandidateEvent(CandidateEvent{CandidateID: candidateB.ID, EngagementID: engagementB.ID, EventType: "manual_note"}); err != nil {
		t.Fatal(err)
	}

	if _, err = store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID: candidateA.ID, TenantID: tenantB, CandidateName: "不应覆盖",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨团队同编号保存应被拒绝：%v", err)
	}
	if store.profiles[candidateA.ID].CandidateName != "团队甲候选人" {
		t.Fatalf("跨团队保存覆盖了原候选人：%+v", store.profiles[candidateA.ID])
	}
	for tenantID, wantID := range map[string]string{tenantA: candidateA.ID, tenantB: candidateB.ID} {
		result, listErr := store.ListPositionCandidates(tenantID, PositionCandidateQuery{PageSize: 10})
		if listErr != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != wantID {
			t.Fatalf("团队 %s 列表越界：items=%+v err=%v", tenantID, result.Items, listErr)
		}
	}
	if _, err = store.GetPositionCandidate(tenantB, candidateA.ID, "", "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("团队乙管理员不应读取团队甲候选人：%v", err)
	}
	if _, err = store.DeleteCandidate(tenantB, candidateA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("团队乙不应删除团队甲候选人：%v", err)
	}

	result, err := store.DeleteTeamCandidates(tenantA)
	if err != nil || result.Deleted != 1 || len(result.AvatarURLs) != 1 {
		t.Fatalf("清空团队甲结果不正确：result=%+v err=%v", result, err)
	}
	if _, exists := store.profiles[candidateA.ID]; exists {
		t.Fatal("团队甲候选人没有被清空")
	}
	if _, exists := store.profiles[candidateB.ID]; !exists || len(store.engagements) != 1 || len(store.events) != 1 {
		t.Fatalf("清空团队甲误删了团队乙数据：profiles=%+v engagements=%+v events=%+v", store.profiles, store.engagements, store.events)
	}
}

// TestCleanupCandidateAvatars 验证删除简历后只清理系统自托管头像，不访问外部地址。
func TestCleanupCandidateAvatars(t *testing.T) {
	resumeDir := t.TempDir()
	avatarURL := candidateAvatarPublicPrefix + strings.Repeat("b", 64) + ".png"
	avatarPath := filepath.Join(resumeDir, candidateAvatarStorageDir, strings.TrimPrefix(avatarURL, candidateAvatarPublicPrefix))
	if err := os.MkdirAll(filepath.Dir(avatarPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(avatarPath, []byte("avatar"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &CandidateService{resumeDir: resumeDir}
	if failed := service.cleanupCandidateAvatars([]string{avatarURL, avatarURL, "https://example.com/avatar.png"}); failed != 0 {
		t.Fatalf("头像清理失败数=%d", failed)
	}
	if _, err := os.Stat(avatarPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("自托管头像仍然存在：%v", err)
	}
}
