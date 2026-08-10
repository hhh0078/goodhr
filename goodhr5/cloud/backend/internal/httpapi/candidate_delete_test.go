// 本文件负责验证候选人删除时会同步清理内存触达和事件数据。
package httpapi

import (
	"errors"
	"testing"
)

// TestMemoryCandidateStoreDeleteCandidate 验证单个候选人删除不会留下触达和事件记录。
func TestMemoryCandidateStoreDeleteCandidate(t *testing.T) {
	store := NewMemoryCandidateStore()
	candidate, err := store.SaveCandidateProfile(CandidateProfileInput{
		CandidateID:   "candidate-delete-test",
		CandidateName: "测试候选人",
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
	if result.Deleted != 1 || len(store.engagements) != 0 || len(store.events) != 0 {
		t.Fatalf("delete result=%+v engagements=%d events=%d", result, len(store.engagements), len(store.events))
	}
	if _, err = store.GetPositionCandidate("tenant-delete-test", candidate.ID, "", "", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted candidate error=%v", err)
	}
}
