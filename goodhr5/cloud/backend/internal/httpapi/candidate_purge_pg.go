// 本文件在 PostgreSQL 短事务内删除单份简历及独立招聘快照，并保护已删除身份不被旧上传重建。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// lockCandidateDeletion 以团队共享锁保护并行上传，排他锁保证删除、事件序号及提交顺序一致。
func lockCandidateDeletion(ctx context.Context, tx *sql.Tx, tenant string, exclusive bool) error {
	q := "SELECT pg_advisory_xact_lock_shared(hashtextextended($1,0))"
	if exclusive {
		q = "SELECT pg_advisory_xact_lock(hashtextextended($1,0))"
	}
	_, err := tx.ExecContext(ctx, q, "candidate-deletion:"+tenant)
	return err
}

// rememberPurgeKey 保存已核实的身份摘要，重复删除共享的历史标识可安全重试。
func rememberPurgeKey(ctx context.Context, tx *sql.Tx, tenant, kind, key string) error {
	if key == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO candidate_deletion_keys(tenant_id,kind,identity_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", tenant, kind, key)
	return err
}

// rejectPurgedKey 阻止与已删除简历准确关联的旧任务；调用方与删除事务共用团队锁。
func rejectPurgedKey(ctx context.Context, tx *sql.Tx, tenant, kind, key string) error {
	if key == "" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM candidate_deletion_keys WHERE tenant_id=$1 AND kind=$2 AND identity_hash=$3)", tenant, kind, key).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errCandidateDeleted
	}
	return nil
}

// candidateWasDeleted 核对一个报告引用的旧云端编号，严格限定本团队。
func (s *PostgresCandidateStore) candidateWasDeleted(ctx context.Context, scope CandidateLibraryScope, id string) (bool, error) {
	var yes bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM candidate_deletions WHERE tenant_id=$1 AND candidate_id::text=$2 AND ($3 OR audiences ? $4))", scope.TenantID, id, scope.IsAdmin, purgeDigest(strings.ToLower(scope.UserEmail))).Scan(&yes)
	return yes, err
}

// purgeCandidate 在同一事务内核实新鲜版本，清理招聘副本并登记设备删除事件。
func (s *PostgresCandidateStore) purgeCandidate(ctx context.Context, scope CandidateLibraryScope, id string, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, scope.TenantID, true); err != nil {
		return err
	}
	if err = s.purgeCandidateLockedTx(ctx, tx, scope, id, version); err != nil {
		return err
	}
	return tx.Commit()
}

// purgeCandidateLockedTx 在已有团队删除锁和事务中复用完整清理，维护任务与人工删除保持同样的封印和附件回收。
func (s *PostgresCandidateStore) purgeCandidateLockedTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string, version int64) error {
	item, err := candidateReadTx(ctx, tx, scope, id, true)
	if errors.Is(err, ErrNotFound) && !scope.IsExtension && id != builtinCandidateID {
		var previous int64
		err = tx.QueryRowContext(ctx, "SELECT row_version FROM candidate_deletions WHERE tenant_id=$1 AND candidate_id::text=$2 AND ($3 OR owner_hash=$4)", scope.TenantID, id, scope.IsAdmin, purgeDigest(strings.ToLower(scope.UserEmail))).Scan(&previous)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if previous != version {
			return ErrCandidateVersion
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !canPurgeCandidate(scope, id, item.UserEmail, item.LibraryFields["readonly"] == true) {
		return ErrNotFound
	}
	if item.RowVersion != version {
		return ErrCandidateVersion
	}
	if scope.retentionCutoff != nil {
		var touched time.Time
		var temporary, protected bool
		if err = tx.QueryRowContext(ctx, `SELECT greatest(updated_at,coalesce(last_communication_at,created_at)),`+retentionOnlineTemporarySQL+`,`+retentionProtectedActorSQL+` FROM candidate_profiles cp WHERE tenant_id=$2 AND id=$3`, *scope.retentionCutoff, scope.TenantID, id).Scan(&touched, &temporary, &protected); err != nil {
			return err
		}
		if normalizeCandidatePhone(item.Phone) != "" || !temporary || protected || !touched.Before(*scope.retentionCutoff) {
			return errCandidateRetentionChanged
		}
	}
	audience := map[string][]string{}
	anchors := map[string]map[string]string{}
	addActor := func(email string) {
		if email != "" {
			key := purgeDigest(strings.ToLower(email))
			if _, ok := audience[key]; !ok {
				audience[key] = []string{}
			}
		}
	}
	addActor(item.UserEmail)
	addActor(scope.UserEmail)
	rows, err := tx.QueryContext(ctx, `SELECT u.email FROM candidate_engagements ce JOIN positions p ON p.id=ce.position_id JOIN users u ON u.id=p.user_id WHERE ce.tenant_id=$1 AND ce.candidate_id=$2`, scope.TenantID, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var email string
		if err = rows.Scan(&email); err != nil {
			rows.Close()
			return err
		}
		addActor(email)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT actor_email,request_key,fingerprint FROM candidate_library_requests WHERE tenant_id=$1 AND result_id=$2 AND left(operation,10)='candidate:'`, scope.TenantID, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var email, key, fingerprint string
		if err = rows.Scan(&email, &key, &fingerprint); err != nil {
			rows.Close()
			return err
		}
		addActor(email)
		actor := purgeDigest(strings.ToLower(email))
		audience[actor] = append(audience[actor], purgeDigest(key))
		if task := attachmentImportReceiptID(key); task != "" {
			if anchors[actor] == nil {
				anchors[actor] = map[string]string{}
			}
			anchors[actor][task] = fingerprint
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	sources := append([]CandidateSource{}, item.Sources...)
	var primary CandidateSource
	if err = tx.QueryRowContext(ctx, "SELECT source_platform_id,source_platform_candidate_id FROM candidate_profiles WHERE tenant_id=$1 AND id=$2", scope.TenantID, id).Scan(&primary.PlatformID, &primary.PlatformCandidateID); err != nil {
		return err
	}
	sources = append(sources, primary)
	// 仅有云端身份而未入库、未关联岗位的其它成员也必须收到删除回执和解析缓存清理任务。
	// 未启用云端自动回复表的旧库仍沿用原本的人工删除路径。
	var cloudStateAvailable bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('auto_reply_cloud_entities') IS NOT NULL`).Scan(&cloudStateAvailable); err != nil {
		return err
	}
	if cloudStateAvailable {
		rows, err = tx.QueryContext(ctx, `SELECT DISTINCT u.email FROM auto_reply_cloud_entities e JOIN users u ON u.tenant_id=e.tenant_id AND u.id=e.user_id
 WHERE e.tenant_id=$1 AND ((e.kind='cloud_links' AND left(e.id,10)='candidate:' AND e.payload->>'value'=$2) OR
 (e.kind='identities' AND coalesce(e.payload->>'platform_candidate_id','')<>'' AND ((e.payload->>'platform_id'=$3 AND e.payload->>'platform_candidate_id'=$4)
	 OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=$1 AND s.candidate_id::text=$2 AND s.platform_id=e.payload->>'platform_id' AND s.platform_candidate_id=e.payload->>'platform_candidate_id'))))`, scope.TenantID, id, primary.PlatformID, primary.PlatformCandidateID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var email string
			if err = rows.Scan(&email); err != nil {
				rows.Close()
				return err
			}
			addActor(email)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	seen := map[string]bool{}
	hashes := []string{}
	for _, source := range sources {
		hash := purgeSourceHash(source.PlatformID, source.PlatformCandidateID)
		if hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
		if err = rememberPurgeKey(ctx, tx, scope.TenantID, "source", hash); err != nil {
			return err
		}
		// 只清完全匹配平台临时编号的筛选记录，姓名与无来源卡片不足以识别同一人。
		rows, err := tx.QueryContext(ctx, `DELETE FROM position_screening_records sr USING users u WHERE sr.tenant_id=$1 AND sr.owner_user_id=u.id AND sr.snapshot->>'platform_id'=$2 AND sr.snapshot->>'platform_candidate_id'=$3 RETURNING sr.position_id::text,sr.attempt_id::text,u.email`, scope.TenantID, source.PlatformID, source.PlatformCandidateID)
		if err != nil {
			return err
		}
		attempts := []string{}
		for rows.Next() {
			var pos, attempt, email string
			if err = rows.Scan(&pos, &attempt, &email); err != nil {
				rows.Close()
				return err
			}
			addActor(email)
			attempts = append(attempts, purgeDigest(pos, attempt))
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, attempt := range attempts {
			if err = rememberPurgeKey(ctx, tx, scope.TenantID, "screening", attempt); err != nil {
				return err
			}
		}
	}
	inherited, err := inheritedReopenSourcesTx(ctx, tx, scope.TenantID, id)
	if err != nil {
		return err
	}
	for _, h := range inherited {
		if !seen[h] {
			hashes = append(hashes, h)
			seen[h] = true
		}
	}
	contacts := []string{}
	if phone := normalizeCandidatePhone(item.Phone); phone != "" {
		var shared bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM candidate_profiles WHERE tenant_id=$1 AND id<>$2 AND normalized_phone=$3)`, scope.TenantID, id, phone).Scan(&shared); err != nil {
			return err
		}
		if !shared {
			contacts = append(contacts, purgeDigest("phone", scope.TenantID, phone))
			if err = rememberPurgeKey(ctx, tx, scope.TenantID, "phone", purgeDigest(scope.TenantID, phone)); err != nil {
				return err
			}
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id FROM recruitment_applications WHERE tenant_id=$1 AND candidate_id=$2 FOR UPDATE", scope.TenantID, id)
	if err != nil {
		return err
	}
	apps := []string{}
	for rows.Next() {
		var app string
		if err = rows.Scan(&app); err != nil {
			rows.Close()
			return err
		}
		apps = append(apps, app)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, app := range apps {
		if err = rememberPurgeKey(ctx, tx, scope.TenantID, "application", purgeDigest(app)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM recruitment_mail_deliveries WHERE tenant_id=$1 AND application_id=$2", scope.TenantID, app); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM recruitment_applications WHERE tenant_id=$1 AND candidate_id=$2", scope.TenantID, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM candidate_profiles WHERE tenant_id=$1 AND id=$2", scope.TenantID, id); err != nil {
		return err
	}
	encodedAudience, _ := json.Marshal(audience)
	encodedSources, _ := json.Marshal(hashes)
	encodedContacts, _ := json.Marshal(contacts)
	var seq int64
	if err = tx.QueryRowContext(ctx, "INSERT INTO candidate_deletions(tenant_id,candidate_id,row_version,owner_hash,audiences,source_hashes,contact_hashes) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb) RETURNING sequence", scope.TenantID, id, version, purgeDigest(strings.ToLower(item.UserEmail)), string(encodedAudience), string(encodedSources), string(encodedContacts)).Scan(&seq); err != nil {
		return err
	}
	for actor := range audience {
		if anchors[actor] == nil {
			anchors[actor] = map[string]string{}
		}
		rawAnchors, _ := json.Marshal(anchors[actor])
		if _, err = tx.ExecContext(ctx, "INSERT INTO candidate_attachment_purge_jobs(deletion_sequence,owner_hash,anchor_ids) VALUES($1,$2,$3::jsonb)", seq, actor, string(rawAnchors)); err != nil {
			return err
		}

	}
	if scope.retentionJobID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE candidate_retention_jobs SET deleted_count=deleted_count+1,lease_until=now()+interval '2 minutes',updated_at=now() WHERE id=$1 AND status='running'`, scope.retentionJobID); err != nil {
			return err
		}
	}
	return nil
}

// candidateDeletions 只查询本团队且与当前账号有关的删除事件，额外一条用于判断是否还有下页。
func (s *PostgresCandidateStore) candidateDeletions(ctx context.Context, tenant, email string, admin bool, after int64) ([]candidateDeletionEvent, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT sequence,candidate_id::text,tenant_id::text,deleted_at,cutoff_from_migration,source_hashes,CASE WHEN EXISTS(SELECT 1 FROM candidate_deletion_keys k WHERE k.tenant_id=$1 AND k.kind='source' AND source_hashes ? k.identity_hash AND k.replacement_candidate_id IS NOT NULL) THEN '[]'::jsonb ELSE contact_hashes END,COALESCE((SELECT jsonb_agg(h) FROM jsonb_array_elements_text(source_hashes) h WHERE NOT EXISTS(SELECT 1 FROM candidate_deletion_keys k WHERE k.tenant_id=$1 AND k.kind='source' AND k.identity_hash=h AND k.replacement_candidate_id IS NOT NULL)),'[]'::jsonb),COALESCE(audiences->$3,'[]'::jsonb) FROM candidate_deletions WHERE tenant_id=$1 AND sequence>$2 AND ($4 OR audiences ? $3) ORDER BY sequence LIMIT 101", tenant, after, purgeDigest(strings.ToLower(email)), admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []candidateDeletionEvent{}
	for rows.Next() {
		var e candidateDeletionEvent
		var sources, keys, contacts, boundaries []byte
		if err := rows.Scan(&e.Sequence, &e.CandidateID, &e.TenantID, &e.DeletedAt, &e.CutoffFromMigration, &boundaries, &contacts, &sources, &keys); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(boundaries, &e.BoundarySourceHashes); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(contacts, &e.ContactHashes); err != nil {
			return nil, err
		}
		e.DeletedAt = e.DeletedAt.UTC()
		if err = json.Unmarshal(sources, &e.SourceHashes); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(keys, &e.RequestHashes); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
