// 本文件实现筛选记录的内存存储与共享幂等规则，用于无数据库启动和隔离单元测试。
package httpapi

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"
)

// screeningStoredRecord 保留可信所有者和已收到的版本摘要，防止新版本到达后旧版本发生内容冲突。
type screeningStoredRecord struct {
	Record    ScreeningRecord
	UserEmail string
	Hashes    map[string]string
}

// MemoryScreeningRecordStore 通过一把锁原子完成同一筛选的身份验证、版本比较和快照替换。
type MemoryScreeningRecordStore struct {
	mu      sync.Mutex
	records map[string]screeningStoredRecord
	now     func() time.Time
}

// NewMemoryScreeningRecordStore 创建独立内存存储，不借用可清除的岗位日志。
func NewMemoryScreeningRecordStore() *MemoryScreeningRecordStore {
	return &MemoryScreeningRecordStore{records: make(map[string]screeningStoredRecord), now: time.Now}
}

// screeningStorageKey 将租户、岗位和本次筛选UUID组成不可跨范围命中的唯一键。
func screeningStorageKey(scope ScreeningScope, attemptID string) string {
	return scope.TenantID + "\x00" + scope.PositionID + "\x00" + attemptID
}

// screeningApplySnapshot 在锁或事务内验证已收版本，迟到旧版本只追加摘要而不覆盖当前内容。
func screeningApplySnapshot(old screeningStoredRecord, scope ScreeningScope, snapshot ScreeningSnapshot, hash string, now time.Time) (screeningStoredRecord, error) {
	key := strconv.FormatInt(snapshot.Revision, 10)
	if old.Record.ID != "" {
		if old.UserEmail != scope.UserEmail || !screeningIdentityMatches(old.Record.ScreeningSnapshot, snapshot) {
			return old, ErrScreeningRecordConflict
		}
		if previous, ok := old.Hashes[key]; ok && previous != hash {
			return old, ErrScreeningRecordConflict
		}
		if old.Hashes == nil {
			old.Hashes = map[string]string{}
		}
		old.Hashes[key] = hash
		if snapshot.Revision <= old.Record.Revision {
			return old, nil
		}
		old.Record.ScreeningSnapshot = snapshot
		old.Record.UpdatedAt = now.UTC()
		return old, nil
	}
	return screeningStoredRecord{Record: ScreeningRecord{ScreeningSnapshot: snapshot, ID: newScreeningRecordID(), PositionID: scope.PositionID, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, UserEmail: scope.UserEmail, Hashes: map[string]string{key: hash}}, nil
}

// Save 校验完整快照并原子保存；调用方拿到的是深拷贝，不能通过指针篡改已保存结果。
func (s *MemoryScreeningRecordStore) Save(ctx context.Context, scope ScreeningScope, snapshot ScreeningSnapshot) (ScreeningRecord, error) {
	if err := ctx.Err(); err != nil {
		return ScreeningRecord{}, err
	}
	if !screeningScopeValid(scope) {
		return ScreeningRecord{}, ErrNotFound
	}
	if err := validateScreeningSnapshot(&snapshot); err != nil {
		return ScreeningRecord{}, err
	}
	hash, err := screeningSnapshotHash(snapshot)
	if err != nil {
		return ScreeningRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := screeningStorageKey(scope, snapshot.AttemptID)
	next, err := screeningApplySnapshot(s.records[key], scope, snapshot, hash, s.now())
	if err != nil {
		return ScreeningRecord{}, err
	}
	s.records[key] = recruitmentClone(next)
	return recruitmentClone(next.Record), nil
}

// screeningNumberMatches 仅让实际数值参与闭区间比较，空值不会作为零分命中。
func screeningNumberMatches(value, min, max *float64) bool {
	return min == nil && max == nil || value != nil && (min == nil || *value >= *min) && (max == nil || *value <= *max)
}

// screeningTimeMatches 使用左闭右开时间范围，未执行轮次的空开始时间不命中。
func screeningTimeMatches(value, from, to *time.Time) bool {
	return from == nil && to == nil || value != nil && (from == nil || !value.Before(*from)) && (to == nil || value.Before(*to))
}

// screeningQueryMatches 对两轮分数与开始时间同时应用过滤，与PostgreSQL查询保持一致。
func screeningQueryMatches(item ScreeningRecord, query ScreeningRecordQuery) bool {
	return screeningNumberMatches(item.First.Score, query.FirstScoreMin, query.FirstScoreMax) && screeningNumberMatches(item.Second.Score, query.SecondScoreMin, query.SecondScoreMax) && screeningTimeMatches(item.First.StartedAt, query.FirstStartedFrom, query.FirstStartedTo) && screeningTimeMatches(item.Second.StartedAt, query.SecondStartedFrom, query.SecondStartedTo)
}

// List 返回指定可信所有者的固定20条分页，发生时间相同则按唯一主键稳定倒序。
func (s *MemoryScreeningRecordStore) List(ctx context.Context, scope ScreeningScope, query ScreeningRecordQuery) (ScreeningRecordList, error) {
	if err := ctx.Err(); err != nil {
		return ScreeningRecordList{}, err
	}
	if !screeningScopeValid(scope) {
		return ScreeningRecordList{}, ErrNotFound
	}
	if err := validateScreeningQuery(&query); err != nil {
		return ScreeningRecordList{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]ScreeningRecord, 0)
	prefix := screeningStorageKey(scope, "")
	for key, item := range s.records {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix && item.UserEmail == scope.UserEmail && screeningQueryMatches(item.Record, query) {
			items = append(items, item.Record)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].OccurredAt.Equal(items[j].OccurredAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
	result := ScreeningRecordList{Records: []ScreeningRecord{}, Total: len(items), Page: query.Page, PageSize: screeningRecordPageSize}
	start := (query.Page - 1) * screeningRecordPageSize
	if start < len(items) {
		end := start + screeningRecordPageSize
		if end > len(items) {
			end = len(items)
		}
		result.Records = recruitmentClone(items[start:end])
	}
	return result, nil
}
