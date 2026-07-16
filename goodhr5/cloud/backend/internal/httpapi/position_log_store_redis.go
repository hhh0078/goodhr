// 本文件负责提供岗位运行期日志 Redis 缓存，并在岗位结束时批量落库。
package httpapi

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const positionLogRedisTTL = 24 * time.Hour

// RedisPositionLogStore 将岗位日志先写入 Redis，读取时优先读取缓存。
type RedisPositionLogStore struct {
	client     *redis.Client
	persistent PositionLogStore
	now        func() time.Time
}

// NewRedisPositionLogStore 创建 Redis 岗位日志缓存存储。
func NewRedisPositionLogStore(addr string, password string, db int, persistent PositionLogStore) *RedisPositionLogStore {
	return &RedisPositionLogStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		persistent: persistent,
		now:        time.Now,
	}
}

// AddPositionLog 将岗位日志写入 Redis 缓存，并限制单岗位最多 1000 条。
func (s *RedisPositionLogStore) AddPositionLog(log PositionLog) (PositionLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if log.CreatedAt.IsZero() {
		log.CreatedAt = s.now().UTC()
	}
	if log.Level == "" {
		log.Level = "info"
	}
	if log.ID == "" {
		log.ID = "position_log_cache_" + strconv.FormatInt(log.CreatedAt.UnixNano(), 10)
	}
	key := positionLogCacheKey(log.PositionID)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return PositionLog{}, err
	}
	if count >= maxPositionLogsPerPosition {
		keepFrom := count - int64(maxPositionLogsPerPosition) + 1
		if err := s.client.LTrim(ctx, key, keepFrom, -1).Err(); err != nil {
			return PositionLog{}, err
		}
	}
	body, err := json.Marshal(log)
	if err != nil {
		return PositionLog{}, err
	}
	if err := s.client.RPush(ctx, key, body).Err(); err != nil {
		return PositionLog{}, err
	}
	_ = s.client.Expire(ctx, key, positionLogRedisTTL).Err()
	return log, nil
}

// ListPositionLogs 优先从 Redis 增量读取岗位日志，缓存不存在时读取持久化存储。
func (s *RedisPositionLogStore) ListPositionLogs(tenantID, userEmail, positionID string, isAdmin bool, query PositionLogQuery) ([]PositionLog, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := positionLogCacheKey(positionID)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return nil, false, err
	}
	if count == 0 {
		return s.persistent.ListPositionLogs(tenantID, userEmail, positionID, isAdmin, query)
	}
	logs, err := s.cachedPositionLogs(ctx, key)
	if err != nil {
		return nil, false, err
	}
	matches := matchingPositionLogs(logs, userEmail, positionID, isAdmin, query)
	limit := normalizePositionLogLimit(query.Limit)
	if query.Since != nil {
		return limitPositionLogs(matches, limit), len(matches) > limit, nil
	}
	if len(matches) >= limit {
		return matches[:limit], true, nil
	}
	merged := append([]PositionLog{}, matches...)
	nextQuery := query
	nextQuery.Limit = limit - len(merged)
	if len(matches) > 0 {
		oldest := matches[len(matches)-1].CreatedAt
		nextQuery.Before = &oldest
	}
	persistentLogs, persistentHasMore, err := s.persistent.ListPositionLogs(tenantID, userEmail, positionID, isAdmin, nextQuery)
	if err != nil {
		return nil, false, err
	}
	merged = append(merged, persistentLogs...)
	return merged, persistentHasMore, nil
}

// ClearPositionLogs 同时清空 Redis 缓存和持久化数据库中的岗位日志。
func (s *RedisPositionLogStore) ClearPositionLogs(tenantID, userEmail, positionID string, isAdmin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.client.Del(ctx, positionLogCacheKey(positionID)).Err(); err != nil {
		return err
	}
	return s.persistent.ClearPositionLogs(tenantID, userEmail, positionID, isAdmin)
}

// SummarizePositionCounts 汇总数据库和 Redis 缓存中的岗位日志统计。
func (s *RedisPositionLogStore) SummarizePositionCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]PositionCountSummary, error) {
	result, err := s.persistent.SummarizePositionCounts(tenantID, userEmail, isAdmin, since)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	iter := s.client.Scan(ctx, 0, "position_logs:*", 100).Iterator()
	for iter.Next(ctx) {
		logs, err := s.cachedPositionLogs(ctx, iter.Val())
		if err != nil {
			return nil, err
		}
		addPositionLogSummary(result, logs, userEmail, isAdmin, since)
	}
	return result, iter.Err()
}

// FlushPositionLogs 将 Redis 中指定岗位日志写入持久化存储，并清空缓存。
func (s *RedisPositionLogStore) FlushPositionLogs(positionID, userEmail string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := positionLogCacheKey(positionID)
	logs, err := s.cachedPositionLogs(ctx, key)
	if err != nil {
		return err
	}
	sort.SliceStable(logs, func(i, j int) bool {
		return logs[i].CreatedAt.Before(logs[j].CreatedAt)
	})
	for _, item := range logs {
		if item.PositionID != positionID || item.UserEmail != userEmail {
			continue
		}
		if _, err := s.persistent.AddPositionLog(item); err != nil {
			return err
		}
	}
	return s.client.Del(ctx, key).Err()
}

// cachedPositionLogs 读取 Redis 中某个岗位的全部缓存日志。
func (s *RedisPositionLogStore) cachedPositionLogs(ctx context.Context, key string) ([]PositionLog, error) {
	values, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	logs := make([]PositionLog, 0, len(values))
	for _, value := range values {
		var item PositionLog
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return nil, err
		}
		logs = append(logs, item)
	}
	return logs, nil
}

// positionLogCacheKey 返回岗位日志缓存 key。
func positionLogCacheKey(positionID string) string {
	return "position_logs:" + positionID
}

// matchingPositionLogs 返回符合查询条件的岗位日志，并按创建时间倒序排列。
func matchingPositionLogs(logs []PositionLog, userEmail, positionID string, isAdmin bool, query PositionLogQuery) []PositionLog {
	items := make([]PositionLog, 0)
	for _, item := range logs {
		if item.PositionID != positionID {
			continue
		}
		if !isAdmin && item.UserEmail != userEmail {
			continue
		}
		if query.Since != nil && item.CreatedAt.Before(*query.Since) {
			continue
		}
		if query.Before != nil && !item.CreatedAt.Before(*query.Before) {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

// limitPositionLogs 按前端分页大小截取岗位日志。
func limitPositionLogs(logs []PositionLog, normalizedLimit int) []PositionLog {
	if normalizedLimit <= 0 {
		normalizedLimit = normalizePositionLogLimit(0)
	}
	if len(logs) > normalizedLimit {
		return logs[:normalizedLimit]
	}
	return logs
}

// addPositionLogSummary 将缓存日志统计合并到岗位统计结果中。
func addPositionLogSummary(result map[string]PositionCountSummary, logs []PositionLog, userEmail string, isAdmin bool, since *time.Time) {
	for _, item := range logs {
		if !isAdmin && item.UserEmail != userEmail {
			continue
		}
		if since != nil && item.CreatedAt.Before(*since) {
			continue
		}
		scanned, greeted, skipped, failed := classifyPositionLogMessage(item.Message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		summary := result[item.PositionID]
		summary.ScannedCount += scanned
		summary.GreetedCount += greeted
		summary.SkippedCount += skipped
		summary.FailedCount += failed
		result[item.PositionID] = summary
	}
}
