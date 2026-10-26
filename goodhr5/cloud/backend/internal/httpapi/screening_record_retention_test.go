// 本文件验证每个岗位仅保留最新500条筛选记录，重传、版本更新和相同时间不会扰动保留窗口。
package httpapi

import (
	"context"
	"testing"
)

// screeningRetentionRows 读取指定岗位全部分页，返回实际记录并检查总数、页长和唯一编号。
func screeningRetentionRows(t *testing.T, store ScreeningRecordStore, scope ScreeningScope) []ScreeningRecord {
	t.Helper()
	var records []ScreeningRecord
	seen := map[string]bool{}
	total := -1
	for page := 1; page <= 100; page++ {
		result, err := store.List(context.Background(), scope, ScreeningRecordQuery{Page: page})
		if err != nil {
			t.Fatal(err)
		}
		if total < 0 {
			total = result.Total
		}
		if result.Total != total || result.PageSize != 20 || len(result.Records) > 20 {
			t.Fatalf("分页合同变化: page=%d total=%d want=%d count=%d", page, result.Total, total, len(result.Records))
		}
		for _, item := range result.Records {
			if seen[item.AttemptID] {
				t.Fatalf("分页重复筛选编号: %s", item.AttemptID)
			}
			seen[item.AttemptID] = true
			records = append(records, item)
		}
		if len(records) == total {
			return records
		}
		if len(result.Records) == 0 || len(records) > total {
			t.Fatalf("分页记录不完整: got=%d total=%d", len(records), total)
		}
	}
	t.Fatal("分页未在合理页数内结束")
	return nil
}

// screeningRetentionSave 保存一份真实快照，并确认即使该旧记录被淘汰也提供可确认发送版本的回执。
func screeningRetentionSave(t *testing.T, store ScreeningRecordStore, scope ScreeningScope, snapshot ScreeningSnapshot) ScreeningRecord {
	t.Helper()
	item, err := store.Save(context.Background(), scope, snapshot)
	if err != nil || item.ID == "" || item.AttemptID != snapshot.AttemptID || item.Revision < snapshot.Revision {
		t.Fatalf("筛选保存回执无效: attempt=%s revision=%d ack=%+v err=%v", snapshot.AttemptID, snapshot.Revision, item, err)
	}
	return item
}

// screeningRetentionRange 检查实际完整保留集合和顺序，low/high为已知合成筛选编号，不依赖存储的淘汰实现。
func screeningRetentionRange(t *testing.T, store ScreeningRecordStore, scope ScreeningScope, low, high int) []ScreeningRecord {
	t.Helper()
	items := screeningRetentionRows(t, store, scope)
	if len(items) != high-low+1 {
		t.Fatalf("保留条数=%d，期望=%d", len(items), high-low+1)
	}
	for index, item := range items {
		want := screeningTestSnapshot(high - index).AttemptID
		if item.AttemptID != want {
			t.Fatalf("第%d条保留编号=%s，期望=%s", index, item.AttemptID, want)
		}
	}
	return items
}

// screeningRetentionChronological 在真实时间序列上验证第501条、版本更新、晚到旧记录以及岗位和租户隔离。
func screeningRetentionChronological(t *testing.T, store ScreeningRecordStore, scope, anotherPosition, anotherTenant ScreeningScope) {
	t.Helper()
	for _, control := range []ScreeningScope{anotherPosition, anotherTenant} {
		screeningRetentionSave(t, store, control, screeningTestSnapshot(0))
	}
	for index := 0; index < 500; index++ {
		screeningRetentionSave(t, store, scope, screeningTestSnapshot(index))
	}
	screeningRetentionRange(t, store, scope, 0, 499)
	latest := screeningTestSnapshot(500)
	saved := screeningRetentionSave(t, store, scope, latest)
	screeningRetentionRange(t, store, scope, 1, 500)

	updated := recruitmentClone(latest)
	updated.Revision = 2
	updated.TerminationReason = "已补齐最终原因"
	ack := screeningRetentionSave(t, store, scope, updated)
	if ack.ID != saved.ID || ack.Revision != 2 {
		t.Fatal("更新同一次筛选生成了新的记录")
	}
	for _, retry := range []ScreeningSnapshot{updated, latest} {
		ack = screeningRetentionSave(t, store, scope, retry)
		if ack.ID != saved.ID || ack.Revision != 2 || ack.TerminationReason != updated.TerminationReason {
			t.Fatal("重传改变了已有记录或覆盖新版本")
		}
	}
	old := screeningTestSnapshot(0)
	old.Revision = 3
	old.TerminationReason = "断网后才补传的旧筛选"
	screeningRetentionSave(t, store, scope, old)
	screeningRetentionSave(t, store, scope, old)
	items := screeningRetentionRange(t, store, scope, 1, 500)
	if items[0].ID != saved.ID || items[0].Revision != 2 {
		t.Fatal("晚到旧筛选挤掉或修改最新记录")
	}
	for _, control := range []ScreeningScope{anotherPosition, anotherTenant} {
		items := screeningRetentionRows(t, store, control)
		if len(items) != 1 || items[0].AttemptID != screeningTestSnapshot(0).AttemptID {
			t.Fatalf("淘汰触及其他岗位或租户: %+v", control)
		}
	}
	empty, err := store.List(context.Background(), scope, ScreeningRecordQuery{Page: 26})
	if err != nil || empty.Total != 500 || len(empty.Records) != 0 {
		t.Fatalf("第26页没有正确为空: total=%d rows=%d err=%v", empty.Total, len(empty.Records), err)
	}
}

// screeningRetentionSameTime 在完全相同时间下重复补传已淘汰编号，确认随机云端主键不再改变保留窗口。
func screeningRetentionSameTime(t *testing.T, store ScreeningRecordStore, scope ScreeningScope) {
	t.Helper()
	stamp := screeningTestSnapshot(0).OccurredAt
	for index := 0; index <= 500; index++ {
		item := screeningTestSnapshot(index)
		item.OccurredAt = stamp
		screeningRetentionSave(t, store, scope, item)
	}
	before := screeningRetentionRange(t, store, scope, 1, 500)
	for revision := int64(1); revision <= 4; revision++ {
		old := screeningTestSnapshot(0)
		old.OccurredAt = stamp
		old.Revision = revision
		screeningRetentionSave(t, store, scope, old)
	}
	after := screeningRetentionRange(t, store, scope, 1, 500)
	for index := range before {
		if before[index].ID != after[index].ID || before[index].Revision != after[index].Revision {
			t.Fatal("相同时间旧记录重放扰动已保留的500条记录")
		}
	}
}

// TestScreeningMemoryRetention 验证内存实现的500条上限与持久存储使用相同业务合同。
func TestScreeningMemoryRetention(t *testing.T) {
	scope := ScreeningScope{TenantID: "tenant-a", PositionID: "position-a", UserEmail: "owner@example.com"}
	t.Run("顺序与隔离", func(t *testing.T) {
		screeningRetentionChronological(t, NewMemoryScreeningRecordStore(), scope, ScreeningScope{TenantID: scope.TenantID, PositionID: "position-b", UserEmail: scope.UserEmail}, ScreeningScope{TenantID: "tenant-b", PositionID: scope.PositionID, UserEmail: "other@example.com"})
	})
	t.Run("相同时间旧记录重放", func(t *testing.T) {
		screeningRetentionSameTime(t, NewMemoryScreeningRecordStore(), scope)
	})
}
