// 本文件用已确认的导入回执及同一捕获快照核验插件临时资料，拒绝重新读取或写回已删除的解析结果。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/redis/go-redis/v9"
	"slices"
	"strconv"
	"strings"
	"time"
)

// attachmentImportReceiptID 只承认服务端确认入库生成的任务编号，普通客户端幂等键不能用于清理无关任务。
func attachmentImportReceiptID(key string) string {
	if !strings.HasPrefix(key, "attachment-import:") {
		return ""
	}
	id := strings.TrimPrefix(key, "attachment-import:")
	if len(id) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return id
}

// attachmentCaptureProof 以账号外加本次捕获编号、已校验正文摘要区分两个不同候选人的临时识别。
func attachmentCaptureProof(task attachmentParseTask) string {
	if task.CaptureID == "" || len(task.TextHash) != 64 {
		return ""
	}
	return purgeDigest("capture", task.CaptureID, task.TextHash)
}

// attachmentPurgeTarget 只采用确认入库的团队及明确编号或平台来源，用于识别尚未成功入库的临时回执。
func attachmentPurgeTarget(record attachmentParseRecord) (tenant, candidate, source string) {
	if record.Import == nil {
		return "", "", ""
	}
	state := record.Import
	return state.Scope.TenantID, firstNonEmpty(state.CandidateID, state.Input.CandidateID), purgeSourceHash(state.Input.Source.PlatformID, state.Input.Source.PlatformCandidateID)
}

// verifiedAttachmentImport 只接受同租户明确绑定或同源导入；没有保存回执时以实际冻结字段对照已提交指纹，防止伪造幂等键误删其他附件。
func verifiedAttachmentImport(r attachmentParseRecord, tenant, id string, sources []string, fingerprint string) bool {
	t, c, src := attachmentPurgeTarget(r)
	if t != tenant || r.Import == nil || r.Task.Phase != "import" {
		return false
	}
	if c != "" {
		return c == id
	}
	return (src != "" && slices.Contains(sources, src)) || (fingerprint != "" && len(r.Import.Fields) > 0 && candidateFingerprint(r.Import.Fields) == fingerprint)
}

// attachmentRevocationReader 在向插件返回临时解析和开始后台处理前核实持久撤销结果。
type attachmentRevocationReader interface {
	attachmentRevoked(context.Context, attachmentParseRecord) (bool, error)
}

// guardedAttachmentStore 为既有缓存增加删除校验，保留原有模型计费及版本交换约束。
type guardedAttachmentStore struct {
	inner attachmentParseStore
	guard attachmentRevocationReader
}

// check 返回可区分的删除状态，底层不可用时不发放旧资料。
func (s *guardedAttachmentStore) check(ctx context.Context, r attachmentParseRecord) error {
	gone, err := s.guard.attachmentRevoked(ctx, r)
	if err != nil {
		return err
	}
	if !gone && attachmentCaptureProof(r.Task) != "" {
		if store, ok := s.guard.(*PostgresCandidateStore); ok {
			if cache, ok := s.inner.(*redisAttachmentParseStore); ok {
				gone, err = store.pendingCaptureRevoked(ctx, cache, r)
				if err != nil {
					return err
				}
			}
		}
	}
	if gone {
		return errCandidateDeleted
	}
	return nil
}

// Reserve 在预占前后都核对撤销状态，覆盖与删除同时发生的跨存储请求。
func (s *guardedAttachmentStore) Reserve(ctx context.Context, r attachmentParseRecord) (attachmentParseRecord, bool, error) {
	if err := s.check(ctx, r); err != nil {
		return attachmentParseRecord{}, false, err
	}
	saved, newly, err := s.inner.Reserve(ctx, r)
	if err == nil {
		err = s.check(ctx, saved)
		if errors.Is(err, errCandidateDeleted) {
			s.erase(ctx, saved)
		}
	}
	if err != nil {
		return attachmentParseRecord{}, false, err
	}
	return saved, newly, nil
}

// Get 只交付当前仍可读的快照，即使 Redis 刚从旧备份恢复也不会返回已撤销内容。
func (s *guardedAttachmentStore) Get(ctx context.Context, owner, id string) (attachmentParseRecord, error) {
	r, err := s.inner.Get(ctx, owner, id)
	if err != nil {
		return attachmentParseRecord{}, err
	}
	if err = s.check(ctx, r); err != nil {
		if errors.Is(err, errCandidateDeleted) {
			s.erase(ctx, r)
		}
		return attachmentParseRecord{}, err
	}
	return r, nil
}

// Swap 在迟到模型结果提交前校验同一份回执，不能用新状态覆盖删除。
func (s *guardedAttachmentStore) Swap(ctx context.Context, r attachmentParseRecord, version int64) (bool, error) {
	if err := s.check(ctx, r); err != nil {
		return false, err
	}
	ok, err := s.inner.Swap(ctx, r, version)
	if ok && err == nil {
		if err = s.check(ctx, r); err != nil {
			if errors.Is(err, errCandidateDeleted) {
				s.erase(ctx, r)
			}
			return false, err
		}
	}
	return ok, err
}

// PendingImports 只恢复不在删除范围内的旧任务，不因一个已删除回执阻塞其余任务。
func (s *guardedAttachmentStore) PendingImports(ctx context.Context) ([]attachmentParseRecord, error) {
	store, ok := s.inner.(attachmentImportPendingStore)
	if !ok {
		return nil, nil
	}
	items, err := store.PendingImports(ctx)
	if err != nil {
		return nil, err
	}
	out := []attachmentParseRecord{}
	for _, r := range items {
		if err = s.check(ctx, r); err != nil {
			if errors.Is(err, errCandidateDeleted) {
				s.erase(ctx, r)
				continue
			}
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// erase 清理已确定的记录，同时释放并发名额和恢复索引，持久待办会补做暂时失败的缓存删除。
func (s *guardedAttachmentStore) erase(ctx context.Context, r attachmentParseRecord) {
	if cache, ok := s.inner.(*redisAttachmentParseStore); ok {
		db, ok := s.guard.(*PostgresCandidateStore)
		if !ok {
			return
		}
		// 先保存精确捕获证据，再释放原始回执；中断时其他 quick/full 任务仍能追溯。
		if _, err := db.db.ExecContext(ctx, `INSERT INTO candidate_attachment_revocations(owner_hash,key_hash) SELECT $1,k FROM unnest(ARRAY[$2::text,$3::text]) k WHERE k<>'' ON CONFLICT DO NOTHING`, purgeDigest(r.Owner), r.Task.ID, attachmentCaptureProof(r.Task)); err == nil {
			_ = cache.erasePurgeRecord(ctx, r)
		}
	} else if cache, ok := s.inner.(*memoryAttachmentParseStore); ok {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if old, found := cache.records[r.Task.ID]; found && old.Owner == r.Owner {
			delete(cache.records, r.Task.ID)
		}
	}
}

// erasePurgeRecord 仅删除原账号的目标记录及两个派生索引，不扫描或清空其他缓存。
func (s *redisAttachmentParseStore) erasePurgeRecord(ctx context.Context, r attachmentParseRecord) error {
	key := attachmentRecordKey(r.Owner, r.Task.ID)
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, attachmentActiveKey(r.Owner), r.Task.ID)
		pipe.ZRem(ctx, attachmentImportPendingKey, key)
		return nil
	})
	return err
}

// attachmentRevoked 用确切任务编号、捕获对或本团队确认过的入库来源校验旧数据。
func (s *PostgresCandidateStore) attachmentRevoked(ctx context.Context, r attachmentParseRecord) (bool, error) {
	tenant, id, source := attachmentPurgeTarget(r)
	var gone bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM candidate_attachment_revocations WHERE owner_hash=$1 AND key_hash IN ($2,$3))
 OR EXISTS(SELECT 1 FROM candidate_deletions WHERE tenant_id=NULLIF($4,'')::uuid AND candidate_id::text=$5)
 OR EXISTS(SELECT 1 FROM candidate_deletion_keys WHERE $5='' AND tenant_id=NULLIF($4,'')::uuid AND kind='source' AND identity_hash=$6)`, purgeDigest(r.Owner), r.Task.ID, attachmentCaptureProof(r.Task), tenant, id, source).Scan(&gone)
	return gone, err
}

// attachmentRevoked 在本地开发存储中同样阻止已删除候选人和快照重新返回。
func (s *MemoryCandidateStore) attachmentRevoked(_ context.Context, r attachmentParseRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant, id, source := attachmentPurgeTarget(r)
	owner := purgeDigest(r.Owner)
	return s.purgeKeys[owner+"|attachment|"+r.Task.ID] || s.purgeKeys[owner+"|attachment|"+attachmentCaptureProof(r.Task)] || s.purgeKeys[tenant+"|candidate|"+id] || (id == "" && s.purgeKeys[tenant+"|source|"+source]), nil
}

// pendingCaptureRevoked 在后台首轮扫描前按已知导入编号核对当前读取的快照，关闭删除后立即打开旧解析的窗口。
func (s *PostgresCandidateStore) pendingCaptureRevoked(ctx context.Context, cache *redisAttachmentParseStore, r attachmentParseRecord) (bool, error) {
	owner := purgeDigest(r.Owner)
	rows, err := s.db.QueryContext(ctx, `SELECT a.key,a.value,d.tenant_id,d.candidate_id,d.source_hashes FROM candidate_attachment_purge_jobs j JOIN candidate_deletions d ON d.sequence=j.deletion_sequence CROSS JOIN LATERAL jsonb_each_text(j.anchor_ids) a WHERE j.owner_hash=$1 AND NOT j.removing`, owner)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	proof := attachmentCaptureProof(r.Task)
	for rows.Next() {
		var id, fingerprint, tenant, candidate string
		var raw []byte
		var sources []string
		if err = rows.Scan(&id, &fingerprint, &tenant, &candidate, &raw); err != nil {
			return false, err
		}
		if err = json.Unmarshal(raw, &sources); err != nil {
			return false, err
		}
		anchor, err := cache.Get(ctx, r.Owner, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if verifiedAttachmentImport(anchor, tenant, candidate, sources, fingerprint) && attachmentCaptureProof(anchor.Task) == proof {
			return true, nil
		}
	}
	return false, rows.Err()
}

// startAttachmentPurge 在后台按小批次处理持久清理记录，Redis 不可用时不阻塞业务删除。
func startAttachmentPurge(ctx context.Context, db *sql.DB, cache *redisAttachmentParseStore) {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			for i := 0; i < 12; i++ {
				done, err := advanceAttachmentPurge(ctx, db, cache)
				if err != nil || !done {
					break
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// advanceAttachmentPurge 一次只扫描一个账号的一页；跨实例领取、重试和游标推进均保留事务边界。
func advanceAttachmentPurge(ctx context.Context, db *sql.DB, cache *redisAttachmentParseStore) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var seq int64
	var owner, tenant, id string
	var cursor uint64
	var removing bool
	var raw, rawAnchors []byte
	err = tx.QueryRowContext(ctx, `SELECT j.deletion_sequence,j.owner_hash,j.scan_cursor,j.removing,d.tenant_id,d.candidate_id,d.source_hashes,j.anchor_ids FROM candidate_attachment_purge_jobs j JOIN candidate_deletions d ON d.sequence=j.deletion_sequence WHERE j.next_attempt_at<=now() ORDER BY j.next_attempt_at,j.deletion_sequence LIMIT 1 FOR UPDATE OF j SKIP LOCKED`).Scan(&seq, &owner, &cursor, &removing, &tenant, &id, &raw, &rawAnchors)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var sources []string
	var anchors map[string]string
	if err = json.Unmarshal(rawAnchors, &anchors); err != nil {
		return false, err
	}
	if err = json.Unmarshal(raw, &sources); err != nil {
		return false, err
	}
	keys, next, err := cache.client.Scan(ctx, cursor, "goodhr:attachment-parse:"+owner+":*", 100).Result()
	if err != nil {
		if _, retryErr := tx.ExecContext(ctx, "UPDATE candidate_attachment_purge_jobs SET next_attempt_at=now()+interval '30 seconds' WHERE deletion_sequence=$1 AND owner_hash=$2", seq, owner); retryErr == nil {
			_ = tx.Commit()
		}
		return false, err
	}
	if len(keys) > 0 {
		values, err := cache.client.MGet(ctx, keys...).Result()
		if err != nil {
			return false, err
		}
		for _, v := range values {
			text, ok := v.(string)
			if !ok {
				continue
			}
			var r attachmentParseRecord
			if err = json.Unmarshal([]byte(text), &r); err != nil {
				return false, err
			}
			if purgeDigest(r.Owner) != owner {
				continue
			}
			direct := verifiedAttachmentImport(r, tenant, id, sources, anchors[r.Task.ID])
			var marked bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM candidate_attachment_revocations WHERE owner_hash=$1 AND key_hash IN ($2,$3))", owner, r.Task.ID, attachmentCaptureProof(r.Task)).Scan(&marked); err != nil {
				return false, err
			}
			if !direct && !marked {
				continue
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO candidate_attachment_revocations(owner_hash,key_hash) SELECT $1,x FROM unnest(ARRAY[$2::text,$3::text]) x WHERE x<>'' ON CONFLICT DO NOTHING`, owner, r.Task.ID, attachmentCaptureProof(r.Task)); err != nil {
				return false, err
			}
			if removing {
				if err = cache.erasePurgeRecord(ctx, r); err != nil {
					return false, err
				}
			}
		}
	}
	if next == 0 && removing {
		_, err = tx.ExecContext(ctx, "DELETE FROM candidate_attachment_purge_jobs WHERE deletion_sequence=$1 AND owner_hash=$2", seq, owner)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE candidate_attachment_purge_jobs SET scan_cursor=$3,removing=$4,next_attempt_at=now() WHERE deletion_sequence=$1 AND owner_hash=$2", seq, owner, strconv.FormatUint(next, 10), removing || next == 0)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
