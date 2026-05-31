// 本文件负责提供任务运行期日志 Redis 缓存，并在任务结束时批量落库。
package httpapi

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const taskLogRedisTTL = 24 * time.Hour

// RedisTaskLogStore 将任务日志先写入 Redis，读取时优先读取缓存。
type RedisTaskLogStore struct {
	client     *redis.Client
	persistent TaskLogStore
	now        func() time.Time
}

// NewRedisTaskLogStore 创建 Redis 任务日志缓存存储。
func NewRedisTaskLogStore(addr string, password string, db int, persistent TaskLogStore) *RedisTaskLogStore {
	return &RedisTaskLogStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		persistent: persistent,
		now:        time.Now,
	}
}

// AddTaskLog 将任务日志写入 Redis 缓存，并限制单任务最多 1000 条。
func (s *RedisTaskLogStore) AddTaskLog(log TaskLog) (TaskLog, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if log.CreatedAt.IsZero() {
		log.CreatedAt = s.now().UTC()
	}
	if log.Level == "" {
		log.Level = "info"
	}
	if log.ID == "" {
		log.ID = "task_log_cache_" + strconv.FormatInt(log.CreatedAt.UnixNano(), 10)
	}
	key := taskLogCacheKey(log.TaskID)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return TaskLog{}, err
	}
	if count >= maxTaskLogsPerTask {
		keepFrom := count - int64(maxTaskLogsPerTask) + 1
		if err := s.client.LTrim(ctx, key, keepFrom, -1).Err(); err != nil {
			return TaskLog{}, err
		}
	}
	body, err := json.Marshal(log)
	if err != nil {
		return TaskLog{}, err
	}
	if err := s.client.RPush(ctx, key, body).Err(); err != nil {
		return TaskLog{}, err
	}
	_ = s.client.Expire(ctx, key, taskLogRedisTTL).Err()
	return log, nil
}

// ListTaskLogs 优先从 Redis 增量读取任务日志，缓存不存在时读取持久化存储。
func (s *RedisTaskLogStore) ListTaskLogs(tenantID, userEmail, taskID string, isAdmin bool, query TaskLogQuery) ([]TaskLog, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := taskLogCacheKey(taskID)
	count, err := s.client.LLen(ctx, key).Result()
	if err != nil {
		return nil, false, err
	}
	if count == 0 {
		return s.persistent.ListTaskLogs(tenantID, userEmail, taskID, isAdmin, query)
	}
	logs, err := s.cachedTaskLogs(ctx, key)
	if err != nil {
		return nil, false, err
	}
	matches := matchingTaskLogs(logs, userEmail, taskID, isAdmin, query)
	limit := normalizeTaskLogLimit(query.Limit)
	if query.Since != nil {
		return limitTaskLogs(matches, limit), len(matches) > limit, nil
	}
	if len(matches) >= limit {
		return matches[:limit], true, nil
	}
	merged := append([]TaskLog{}, matches...)
	nextQuery := query
	nextQuery.Limit = limit - len(merged)
	if len(matches) > 0 {
		oldest := matches[len(matches)-1].CreatedAt
		nextQuery.Before = &oldest
	}
	persistentLogs, persistentHasMore, err := s.persistent.ListTaskLogs(tenantID, userEmail, taskID, isAdmin, nextQuery)
	if err != nil {
		return nil, false, err
	}
	merged = append(merged, persistentLogs...)
	return merged, persistentHasMore, nil
}

// ClearTaskLogs 同时清空 Redis 缓存和持久化数据库中的任务日志。
func (s *RedisTaskLogStore) ClearTaskLogs(tenantID, userEmail, taskID string, isAdmin bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.client.Del(ctx, taskLogCacheKey(taskID)).Err(); err != nil {
		return err
	}
	return s.persistent.ClearTaskLogs(tenantID, userEmail, taskID, isAdmin)
}

// SummarizeTaskCounts 汇总数据库和 Redis 缓存中的任务日志统计。
func (s *RedisTaskLogStore) SummarizeTaskCounts(tenantID, userEmail string, isAdmin bool, since *time.Time) (map[string]TaskCountSummary, error) {
	result, err := s.persistent.SummarizeTaskCounts(tenantID, userEmail, isAdmin, since)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	iter := s.client.Scan(ctx, 0, "task_logs:*", 100).Iterator()
	for iter.Next(ctx) {
		logs, err := s.cachedTaskLogs(ctx, iter.Val())
		if err != nil {
			return nil, err
		}
		addTaskLogSummary(result, logs, userEmail, isAdmin, since)
	}
	return result, iter.Err()
}

// FlushTaskLogs 将 Redis 中指定任务日志写入持久化存储，并清空缓存。
func (s *RedisTaskLogStore) FlushTaskLogs(taskID, userEmail string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := taskLogCacheKey(taskID)
	logs, err := s.cachedTaskLogs(ctx, key)
	if err != nil {
		return err
	}
	sort.SliceStable(logs, func(i, j int) bool {
		return logs[i].CreatedAt.Before(logs[j].CreatedAt)
	})
	for _, item := range logs {
		if item.TaskID != taskID || item.UserEmail != userEmail {
			continue
		}
		if _, err := s.persistent.AddTaskLog(item); err != nil {
			return err
		}
	}
	return s.client.Del(ctx, key).Err()
}

// cachedTaskLogs 读取 Redis 中某个任务的全部缓存日志。
func (s *RedisTaskLogStore) cachedTaskLogs(ctx context.Context, key string) ([]TaskLog, error) {
	values, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	logs := make([]TaskLog, 0, len(values))
	for _, value := range values {
		var item TaskLog
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return nil, err
		}
		logs = append(logs, item)
	}
	return logs, nil
}

// taskLogCacheKey 返回任务日志缓存 key。
func taskLogCacheKey(taskID string) string {
	return "task_logs:" + taskID
}

// matchingTaskLogs 返回符合查询条件的任务日志，并按创建时间倒序排列。
func matchingTaskLogs(logs []TaskLog, userEmail, taskID string, isAdmin bool, query TaskLogQuery) []TaskLog {
	items := make([]TaskLog, 0)
	for _, item := range logs {
		if item.TaskID != taskID {
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

// limitTaskLogs 按前端分页大小截取任务日志。
func limitTaskLogs(logs []TaskLog, normalizedLimit int) []TaskLog {
	if normalizedLimit <= 0 {
		normalizedLimit = normalizeTaskLogLimit(0)
	}
	if len(logs) > normalizedLimit {
		return logs[:normalizedLimit]
	}
	return logs
}

// addTaskLogSummary 将缓存日志统计合并到任务统计结果中。
func addTaskLogSummary(result map[string]TaskCountSummary, logs []TaskLog, userEmail string, isAdmin bool, since *time.Time) {
	for _, item := range logs {
		if !isAdmin && item.UserEmail != userEmail {
			continue
		}
		if since != nil && item.CreatedAt.Before(*since) {
			continue
		}
		scanned, greeted, skipped, failed := classifyTaskLogMessage(item.Message)
		if scanned == 0 && greeted == 0 && skipped == 0 && failed == 0 {
			continue
		}
		summary := result[item.TaskID]
		summary.ScannedCount += scanned
		summary.GreetedCount += greeted
		summary.SkippedCount += skipped
		summary.FailedCount += failed
		result[item.TaskID] = summary
	}
}
