// 本文件负责定义云端岗位日志的数据模型和存储接口。
package httpapi

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const maxPositionLogsPerPosition = 1000

// PositionLog 表示一条云端岗位日志摘要。
type PositionLog struct {
	ID         string
	PositionID string
	UserEmail  string
	Level      string
	Message    string
	CreatedAt  time.Time
}

type PositionCountSummary struct {
	ScannedCount int
	GreetedCount int
	SkippedCount int
	FailedCount  int
}

// PositionLogQuery 定义岗位日志分页查询条件。
type PositionLogQuery struct {
	Since  *time.Time
	Before *time.Time
	Limit  int
}

// PositionLogStore 定义岗位日志摘要的持久化能力。
type PositionLogStore interface {
	AddPositionLog(log PositionLog) (PositionLog, error)
	ListPositionLogs(tenantID, userEmail, positionID string, isAdmin bool, query PositionLogQuery) ([]PositionLog, bool, error)
	ClearPositionLogs(tenantID, userEmail, positionID string, isAdmin bool) error
	SummarizePositionCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]PositionCountSummary, error)
}

// PositionLogFlushStore 定义岗位日志缓存落库能力。
type PositionLogFlushStore interface {
	FlushPositionLogs(positionID, userEmail string) error
}

// MemoryPositionLogStore 提供开发期使用的内存岗位日志存储。
type MemoryPositionLogStore struct {
	mu     sync.Mutex
	logs   []PositionLog
	now    func() time.Time
	nextID func() string
}

// NewMemoryPositionLogStore 创建开发期内存岗位日志存储。
func NewMemoryPositionLogStore() *MemoryPositionLogStore {
	seq := 0
	return &MemoryPositionLogStore{
		logs: make([]PositionLog, 0),
		now:  time.Now,
		nextID: func() string {
			seq++
			return "position_log_" + intString(seq)
		},
	}
}

// AddPositionLog 新增一条岗位日志摘要。
func (s *MemoryPositionLogStore) AddPositionLog(log PositionLog) (PositionLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.ID = s.nextID()
	if log.CreatedAt.IsZero() {
		log.CreatedAt = s.now()
	}
	if log.Level == "" {
		log.Level = "info"
	}
	s.trimPositionLogsLocked(log.PositionID, log.UserEmail, 1)
	s.logs = append(s.logs, log)
	return log, nil
}

// ListPositionLogs 列出当前用户某个岗位的日志摘要。
func (s *MemoryPositionLogStore) ListPositionLogs(tenantID, userEmail, positionID string, isAdmin bool, query PositionLogQuery) ([]PositionLog, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := make([]PositionLog, 0)
	for _, log := range s.logs {
		if (!isAdmin && log.UserEmail != userEmail) || log.PositionID != positionID {
			continue
		}
		if query.Since != nil && log.CreatedAt.Before(*query.Since) {
			continue
		}
		if query.Before != nil && !log.CreatedAt.Before(*query.Before) {
			continue
		}
		items = append(items, log)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	limit := normalizePositionLogLimit(query.Limit)
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

// ClearPositionLogs 清空当前用户某个岗位的日志摘要。
func (s *MemoryPositionLogStore) ClearPositionLogs(tenantID, userEmail, positionID string, isAdmin bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := make([]PositionLog, 0, len(s.logs))
	for _, log := range s.logs {
		isTargetPosition := log.PositionID == positionID
		isTargetUser := isAdmin || log.UserEmail == userEmail
		if isTargetPosition && isTargetUser {
			continue
		}
		filtered = append(filtered, log)
	}
	s.logs = filtered
	return nil
}

// SummarizePositionCounts 汇总指定时间范围内各岗位的扫描/打招呼/跳过/失败数量。
func (s *MemoryPositionLogStore) SummarizePositionCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]PositionCountSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := map[string]PositionCountSummary{}
	for _, log := range s.logs {
		if !isAdmin && log.UserEmail != userEmail {
			continue
		}
		if since != nil && log.CreatedAt.Before(*since) {
			continue
		}
		scanned, greeted, skipped, failed := classifyPositionLogMessage(log.Message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		item := result[log.PositionID]
		item.ScannedCount += scanned
		item.GreetedCount += greeted
		item.SkippedCount += skipped
		item.FailedCount += failed
		result[log.PositionID] = item
	}
	return result, nil
}

// trimPositionLogsLocked 写入前检查内存日志数量，超过上限时删除最早日志。
func (s *MemoryPositionLogStore) trimPositionLogsLocked(positionID string, userEmail string, incoming int) {
	count := 0
	for _, item := range s.logs {
		if item.PositionID == positionID && item.UserEmail == userEmail {
			count++
		}
	}
	removeCount := count + incoming - maxPositionLogsPerPosition
	if removeCount <= 0 {
		return
	}
	targets := make([]PositionLog, 0, count)
	for _, item := range s.logs {
		if item.PositionID == positionID && item.UserEmail == userEmail {
			targets = append(targets, item)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].CreatedAt.Before(targets[j].CreatedAt)
	})
	removeIDs := map[string]struct{}{}
	for i := 0; i < removeCount && i < len(targets); i++ {
		removeIDs[targets[i].ID] = struct{}{}
	}
	kept := make([]PositionLog, 0, len(s.logs))
	for _, item := range s.logs {
		if _, ok := removeIDs[item.ID]; ok {
			continue
		}
		kept = append(kept, item)
	}
	s.logs = kept
}

func classifyPositionLogMessage(message string) (int, int, int, int) {
	switch {
	case strings.HasPrefix(message, "开始处理"):
		return 1, 0, 0, 0
	case strings.Contains(message, "打招呼成功"):
		return 0, 1, 0, 0
	case strings.Contains(message, "筛选跳过"):
		return 0, 0, 1, 0
	case strings.Contains(message, "打招呼失败"), strings.Contains(message, "AI筛选失败"), strings.Contains(message, "AI 筛选失败"):
		return 0, 0, 0, 1
	default:
		return 0, 0, 0, 0
	}
}
