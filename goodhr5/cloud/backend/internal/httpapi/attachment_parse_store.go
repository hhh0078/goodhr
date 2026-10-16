// 本文件保存附件解析和已确认入库任务的幂等回执；生产复用认证 Redis，确认入库后的临时文字随终态清理。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const attachmentTaskTTL = 7 * 24 * time.Hour

var errAttachmentBusy = errors.New("同时处理的附件有点多，请等当前任务结束再试")

// attachmentParseRecord 保存账号归属和回执；Import 只存用户确认入库的临时资料，绝不保存 AI Key 或登录凭证。
type attachmentParseRecord struct {
	Task        attachmentParseTask    `json:"task"`
	Owner       string                 `json:"owner"`
	Fingerprint string                 `json:"fingerprint"`
	Version     int64                  `json:"version"`
	LeaseUntil  int64                  `json:"lease_until"`
	Import      *attachmentImportState `json:"import,omitempty"`
}

// attachmentImportPendingStore 枚举已确认但尚未结束的入库任务，用于重启后的安全恢复。
type attachmentImportPendingStore interface {
	PendingImports(context.Context) ([]attachmentParseRecord, error)
}

// attachmentParseStore 提供任务预占及版本交换，保证取消和模型结果不能相互覆盖。
type attachmentParseStore interface {
	Reserve(context.Context, attachmentParseRecord) (attachmentParseRecord, bool, error)
	Get(context.Context, string, string) (attachmentParseRecord, error)
	Swap(context.Context, attachmentParseRecord, int64) (bool, error)
}

// memoryAttachmentParseStore 仅供未配置 Redis 的本地开发和合同测试使用。
type memoryAttachmentParseStore struct {
	mu      sync.Mutex
	records map[string]attachmentParseRecord
}

// newAttachmentParseStore 在生产复用已检查连接的认证 Redis，本地开发使用内存实现。
func newAttachmentParseStore(auth AuthStore) attachmentParseStore {
	if store, ok := auth.(*RedisAuthStore); ok {
		return &redisAttachmentParseStore{client: store.client}
	}
	return &memoryAttachmentParseStore{records: map[string]attachmentParseRecord{}}
}

// Reserve 原子预占一个请求；同账号最多同时处理两个任务，重复请求不占用第二个名额。
func (s *memoryAttachmentParseStore) Reserve(_ context.Context, record attachmentParseRecord) (attachmentParseRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.records[record.Task.ID]; ok {
		return old, false, nil
	}
	active := 0
	for _, old := range s.records {
		if old.Owner == record.Owner && attachmentTaskActive(old.Task.Status) && old.LeaseUntil > time.Now().UnixMilli() {
			active++
		}
	}
	if active >= 2 {
		return attachmentParseRecord{}, false, errAttachmentBusy
	}
	s.records[record.Task.ID] = attachmentRecordCopy(record)
	return record, true, nil
}

// Get 只允许原账号读取回执，不向其它账号暴露任务是否存在。
func (s *memoryAttachmentParseStore) Get(_ context.Context, owner, id string) (attachmentParseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok || record.Owner != owner {
		return attachmentParseRecord{}, ErrNotFound
	}
	return attachmentRecordCopy(record), nil
}

// Swap 使用版本号交换状态，已取消任务不能被迟到的成功结果覆盖。
func (s *memoryAttachmentParseStore) Swap(_ context.Context, record attachmentParseRecord, version int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.records[record.Task.ID]
	if !ok || old.Owner != record.Owner || old.Version != version {
		return false, nil
	}
	s.records[record.Task.ID] = attachmentRecordCopy(record)
	return true, nil
}

// PendingImports 返回尚未结束的确认意图，不把普通解析任务恢复为付费调用。
func (s *memoryAttachmentParseStore) PendingImports(_ context.Context) ([]attachmentParseRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []attachmentParseRecord{}
	for _, record := range s.records {
		if record.Import != nil && attachmentImportPending(record) {
			result = append(result, attachmentRecordCopy(record))
		}
	}
	return result, nil
}

// attachmentRecordCopy 隔离测试和开发存储的嵌套结果对象，避免锁外修改数据。
func attachmentRecordCopy(record attachmentParseRecord) attachmentParseRecord {
	data, _ := json.Marshal(record)
	var result attachmentParseRecord
	_ = json.Unmarshal(data, &result)
	return result
}

// attachmentTaskActive 判断任务是否仍占用付费调用名额。
func attachmentTaskActive(status string) bool { return status == "queued" || status == "running" }

type redisAttachmentParseStore struct{ client *redis.Client }

// attachmentRecordKey 对账号和任务编号散列命名，Redis key 不含邮箱或候选人信息。
func attachmentRecordKey(owner, id string) string {
	return "goodhr:attachment-parse:" + attachmentDigest(owner) + ":" + id
}

// attachmentActiveKey 保存账号正在处理的任务租约，不记录简历原文。
func attachmentActiveKey(owner string) string {
	return "goodhr:attachment-active:" + attachmentDigest(owner)
}

var reserveAttachmentScript = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if old then return {0, old} end
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[1])
if redis.call('ZCARD', KEYS[2]) >= 2 then return {-1, ''} end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('ZADD', KEYS[2], ARGV[4], ARGV[5])
redis.call('PEXPIRE', KEYS[2], ARGV[3])
if ARGV[6] == '1' then redis.call('ZADD', KEYS[3], ARGV[1], KEYS[1]) end
return {1, ARGV[2]}
`)

// Reserve 在 Redis 中一次性写入幂等回执并预占账号名额，两个服务实例不会重复调用模型。
func (s *redisAttachmentParseStore) Reserve(ctx context.Context, record attachmentParseRecord) (attachmentParseRecord, bool, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return attachmentParseRecord{}, false, err
	}
	importTask := "0"
	if record.Import != nil {
		importTask = "1"
	}
	value, err := reserveAttachmentScript.Run(ctx, s.client, []string{attachmentRecordKey(record.Owner, record.Task.ID), attachmentActiveKey(record.Owner), attachmentImportPendingKey}, time.Now().UnixMilli(), data, attachmentTaskTTL.Milliseconds(), record.LeaseUntil, record.Task.ID, importTask).Slice()
	if err != nil {
		return attachmentParseRecord{}, false, err
	}
	if len(value) != 2 {
		return attachmentParseRecord{}, false, errors.New("附件任务回执格式不正确")
	}
	code, ok := value[0].(int64)
	if !ok {
		return attachmentParseRecord{}, false, errors.New("附件任务回执格式不正确")
	}
	if code == -1 {
		return attachmentParseRecord{}, false, errAttachmentBusy
	}
	encoded, ok := value[1].(string)
	if !ok {
		return attachmentParseRecord{}, false, errors.New("附件任务回执格式不正确")
	}
	var result attachmentParseRecord
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		return result, false, err
	}
	return result, code == 1, nil
}

// Get 从 Redis 读取原账号的任务；回执失效后不会自动恢复付费请求。
func (s *redisAttachmentParseStore) Get(ctx context.Context, owner, id string) (attachmentParseRecord, error) {
	data, err := s.client.Get(ctx, attachmentRecordKey(owner, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return attachmentParseRecord{}, ErrNotFound
	}
	if err != nil {
		return attachmentParseRecord{}, err
	}
	var record attachmentParseRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.Owner != owner {
		return attachmentParseRecord{}, ErrNotFound
	}
	return record, nil
}

var swapAttachmentScript = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
if not old then return 0 end
local parsed = cjson.decode(old)
if parsed.version ~= tonumber(ARGV[1]) then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
if ARGV[4] == '1' then redis.call('ZREM', KEYS[2], ARGV[5]) end
if ARGV[6] == '1' then
  redis.call('ZADD', KEYS[3], ARGV[7], KEYS[1])
else
  redis.call('ZREM', KEYS[3], KEYS[1])
end
return 1
`)

// Swap 原子保存模型调用前状态或终态；进程崩溃后保留原回执，客户端轮询不会重放调用。
func (s *redisAttachmentParseStore) Swap(ctx context.Context, record attachmentParseRecord, version int64) (bool, error) {
	data, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	terminal := "0"
	if !attachmentTaskActive(record.Task.Status) {
		terminal = "1"
	}
	pending := "0"
	if record.Import != nil && attachmentImportPending(record) {
		pending = "1"
	}
	result, err := swapAttachmentScript.Run(ctx, s.client, []string{attachmentRecordKey(record.Owner, record.Task.ID), attachmentActiveKey(record.Owner), attachmentImportPendingKey}, version, data, attachmentTaskTTL.Milliseconds(), terminal, record.Task.ID, pending, time.Now().UnixMilli()).Int()
	return result == 1, err
}

const attachmentImportPendingKey = "goodhr:attachment-import-pending"

// PendingImports 按公平轮转读取有限批次的持久任务；失效回执同时从恢复索引清除。
func (s *redisAttachmentParseStore) PendingImports(ctx context.Context) ([]attachmentParseRecord, error) {
	keys, err := s.client.ZRange(ctx, attachmentImportPendingKey, 0, 127).Result()
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := []attachmentParseRecord{}
	for index, value := range values {
		raw, ok := value.(string)
		var record attachmentParseRecord
		if !ok || json.Unmarshal([]byte(raw), &record) != nil || record.Import == nil || !attachmentImportPending(record) {
			_ = s.client.ZRem(ctx, attachmentImportPendingKey, keys[index]).Err()
			continue
		}
		result = append(result, record)
		_ = s.client.ZAdd(ctx, attachmentImportPendingKey, redis.Z{Score: float64(time.Now().UnixMilli()), Member: keys[index]}).Err()
	}
	return result, nil
}
