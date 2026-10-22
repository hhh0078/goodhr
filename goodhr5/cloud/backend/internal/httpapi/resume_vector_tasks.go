// 本文件保存独立七日向量后台任务，原子限制每账号一个任务和全局两个执行租约，不依赖对话连接。
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const resumeVectorTaskTTL = 7 * 24 * time.Hour
const resumeVectorLease = 4 * time.Minute

// resumeVectorTask 只保存任务进度和候选人编号，密钥、简历原文及向量保存在各自受权存储。
type resumeVectorTask struct {
	ID          string    `json:"id"`
	Owner       string    `json:"owner"`
	TenantID    string    `json:"tenant_id"`
	Status      string    `json:"status"`
	Stage       string    `json:"stage"`
	Message     string    `json:"message"`
	Version     int64     `json:"version"`
	LeaseToken  string    `json:"lease_token"`
	LeaseUntil  int64     `json:"lease_until"`
	CandidateID string    `json:"candidate_id"`
	BatchIDs    []string  `json:"batch_ids"`
	Processed   int       `json:"processed"`
	Failed      int       `json:"failed"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// resumeVectorTaskStore 通过版本与租约保存断点，后台请求无权修改别人的任务。
type resumeVectorTaskStore interface {
	Get(context.Context, string) (resumeVectorTask, error)
	Reserve(context.Context, resumeVectorTask) (resumeVectorTask, error)
	Claim(context.Context, string) (resumeVectorTask, bool, error)
	Save(context.Context, resumeVectorTask, int64) (bool, error)
	Pending(context.Context) ([]resumeVectorTask, error)
}

// vectorTaskActive 判断排队和运行任务，终态不再占用账号或全局名额。
func vectorTaskActive(status string) bool { return status == "queued" || status == "running" }

// vectorTaskID 生成不可预测的任务或租约编号，不使用邮箱或时间作为可猜测凭据。
func vectorTaskID() string { return rand.Text() }

// vectorTaskKey 使用账号散列隔离Redis key，避免邮箱直接进入缓存运维输出。
func vectorTaskKey(owner string) string {
	hash := sha256.Sum256([]byte(owner))
	return "goodhr:resume-vector-task:" + hex.EncodeToString(hash[:16])
}

// copyVectorTask 复制嵌套任务状态，避免调用方在存储锁外修改断点。
func copyVectorTask(task resumeVectorTask) resumeVectorTask {
	raw, _ := json.Marshal(task)
	var result resumeVectorTask
	_ = json.Unmarshal(raw, &result)
	return result
}

// memoryResumeVectorTasks 仅供无数据库开发和可控测试使用，所有实例内执行租约合计最多两个。
type memoryResumeVectorTasks struct {
	mu      sync.Mutex
	records map[string]resumeVectorTask
}

// newResumeVectorTaskStore 复用已验证认证Redis连接；生产缺少Redis时由服务禁止付费任务。
func newResumeVectorTaskStore(auth AuthStore) resumeVectorTaskStore {
	if store, ok := auth.(*RedisAuthStore); ok {
		return &redisResumeVectorTasks{client: store.client}
	}
	return &memoryResumeVectorTasks{records: map[string]resumeVectorTask{}}
}

// Get 返回原账号七日内任务，超过期限的任务不能用于重新执行付费操作。
func (s *memoryResumeVectorTasks) Get(_ context.Context, owner string) (resumeVectorTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[owner]
	if !ok || !time.Now().Before(item.ExpiresAt) {
		return resumeVectorTask{}, ErrNotFound
	}
	return copyVectorTask(item), nil
}

// Reserve 复用活动任务，重复点击和网络重试不会额外创建任务。
func (s *memoryResumeVectorTasks) Reserve(_ context.Context, item resumeVectorTask) (resumeVectorTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.records[item.Owner]; ok && time.Now().Before(old.ExpiresAt) && vectorTaskActive(old.Status) {
		return copyVectorTask(old), nil
	}
	s.records[item.Owner] = copyVectorTask(item)
	return copyVectorTask(item), nil
}

// Claim 原子领取执行租约，全局两个名额满时继续排队，不在HTTP请求中等待。
func (s *memoryResumeVectorTasks) Claim(_ context.Context, owner string) (resumeVectorTask, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[owner]
	now := time.Now()
	if !ok || !now.Before(item.ExpiresAt) || !vectorTaskActive(item.Status) || item.LeaseUntil > now.UnixMilli() {
		return item, false, nil
	}
	active := 0
	for _, old := range s.records {
		if vectorTaskActive(old.Status) && old.LeaseUntil > now.UnixMilli() {
			active++
		}
	}
	if active >= 2 {
		return item, false, nil
	}
	item.Status = "running"
	item.LeaseToken = vectorTaskID()
	item.LeaseUntil = now.Add(resumeVectorLease).UnixMilli()
	item.Version++
	s.records[owner] = copyVectorTask(item)
	return item, true, nil
}

// Save 只接受当前执行者和版本；旧进程及迟到响应无法覆盖新任务。
func (s *memoryResumeVectorTasks) Save(_ context.Context, item resumeVectorTask, expected int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[item.Owner]
	if !ok || old.ID != item.ID || old.Version != expected || old.LeaseToken != item.LeaseToken || !time.Now().Before(old.ExpiresAt) {
		return false, nil
	}
	s.records[item.Owner] = copyVectorTask(item)
	return true, nil
}

// Pending 返回尚未结束的任务快照，执行者仍须通过Claim领取租约。
func (s *memoryResumeVectorTasks) Pending(_ context.Context) ([]resumeVectorTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []resumeVectorTask{}
	for _, item := range s.records {
		if vectorTaskActive(item.Status) && time.Now().Before(item.ExpiresAt) {
			items = append(items, copyVectorTask(item))
		}
	}
	return items, nil
}

// redisResumeVectorTasks 在多个后端实例之间协调账号任务与全局并发。
type redisResumeVectorTasks struct{ client *redis.Client }

const vectorTasksPendingKey = "goodhr:resume-vector-pending"
const vectorTasksActiveKey = "goodhr:resume-vector-active"

// Get 只按服务器已认证账号定位任务，不接受浏览器指定拥有者。
func (s *redisResumeVectorTasks) Get(ctx context.Context, owner string) (resumeVectorTask, error) {
	raw, err := s.client.Get(ctx, vectorTaskKey(owner)).Bytes()
	if errors.Is(err, redis.Nil) {
		return resumeVectorTask{}, ErrNotFound
	}
	var item resumeVectorTask
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(raw, &item)
	if err == nil && (item.Owner != owner || !time.Now().Before(item.ExpiresAt)) {
		err = ErrNotFound
	}
	return item, err
}

var reserveVectorTaskScript = redis.NewScript(`
local raw=redis.call('GET',KEYS[1])
if raw then local old=cjson.decode(raw);if old.status=='queued' or old.status=='running' then return raw end end
redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]);redis.call('ZADD',KEYS[2],ARGV[3],KEYS[1]);return ARGV[1]
`)

// Reserve 原子保存七日任务并加入持久恢复列表，活动任务不会被重复请求覆盖。
func (s *redisResumeVectorTasks) Reserve(ctx context.Context, item resumeVectorTask) (resumeVectorTask, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	value, err := reserveVectorTaskScript.Run(ctx, s.client, []string{vectorTaskKey(item.Owner), vectorTasksPendingKey}, raw, time.Until(item.ExpiresAt).Milliseconds(), time.Now().UnixMilli()).Text()
	if err == nil {
		err = json.Unmarshal([]byte(value), &item)
	}
	return item, err
}

var claimVectorTaskScript = redis.NewScript(`
local raw=redis.call('GET',KEYS[1]);if not raw then return '' end
local old=cjson.decode(raw);if old.status~='queued' and old.status~='running' then return '' end
if tonumber(old.lease_until)>tonumber(ARGV[1]) then return '' end
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',ARGV[1]);if redis.call('ZCARD',KEYS[2])>=2 then return '' end
old.status='running';old.lease_token=ARGV[2];old.lease_until=tonumber(ARGV[3]);old.version=old.version+1
local next=cjson.encode(old);local ttl=redis.call('PTTL',KEYS[1]);if ttl<=0 then return '' end
redis.call('SET',KEYS[1],next,'PX',ttl);redis.call('ZADD',KEYS[2],ARGV[3],KEYS[1]);return next
`)

// Claim 使用同一Redis脚本限制跨实例全局并发，并用新令牌隔离过期工作进程。
func (s *redisResumeVectorTasks) Claim(ctx context.Context, owner string) (resumeVectorTask, bool, error) {
	now := time.Now()
	value, err := claimVectorTaskScript.Run(ctx, s.client, []string{vectorTaskKey(owner), vectorTasksActiveKey}, now.UnixMilli(), vectorTaskID(), now.Add(resumeVectorLease).UnixMilli()).Text()
	var item resumeVectorTask
	if err != nil || value == "" {
		return item, false, err
	}
	err = json.Unmarshal([]byte(value), &item)
	return item, err == nil, err
}

var saveVectorTaskScript = redis.NewScript(`
local raw=redis.call('GET',KEYS[1]);if not raw then return 0 end
local old=cjson.decode(raw);if old.id~=ARGV[1] or old.version~=tonumber(ARGV[2]) or old.lease_token~=ARGV[3] then return 0 end
local ttl=redis.call('PTTL',KEYS[1]);if ttl<=0 then return 0 end
redis.call('SET',KEYS[1],ARGV[4],'PX',ttl)
if ARGV[5]=='1' then redis.call('ZREM',KEYS[2],KEYS[1]);redis.call('ZREM',KEYS[3],KEYS[1]) else redis.call('ZADD',KEYS[2],ARGV[6],KEYS[1]) end
return 1
`)

// Save 保存固定截止时间内的断点与租约；终态释放全局及账号恢复名额。
func (s *redisResumeVectorTasks) Save(ctx context.Context, item resumeVectorTask, expected int64) (bool, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return false, err
	}
	terminal := "0"
	if !vectorTaskActive(item.Status) {
		terminal = "1"
	}
	result, err := saveVectorTaskScript.Run(ctx, s.client, []string{vectorTaskKey(item.Owner), vectorTasksActiveKey, vectorTasksPendingKey}, item.ID, expected, item.LeaseToken, raw, terminal, item.LeaseUntil).Int()
	return result == 1, err
}

// Pending 公平轮转读取有限恢复任务，过期或终态从恢复索引移除，不扫描简历正文。
func (s *redisResumeVectorTasks) Pending(ctx context.Context) ([]resumeVectorTask, error) {
	keys, err := s.client.ZRange(ctx, vectorTasksPendingKey, 0, 99).Result()
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	items := []resumeVectorTask{}
	for i, value := range values {
		raw, ok := value.(string)
		var item resumeVectorTask
		if !ok || json.Unmarshal([]byte(raw), &item) != nil || !vectorTaskActive(item.Status) || !time.Now().Before(item.ExpiresAt) {
			_ = s.client.ZRem(ctx, vectorTasksPendingKey, keys[i]).Err()
			continue
		}
		items = append(items, item)
		_ = s.client.ZAdd(ctx, vectorTasksPendingKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: keys[i]}).Err()
	}
	return items, nil
}
