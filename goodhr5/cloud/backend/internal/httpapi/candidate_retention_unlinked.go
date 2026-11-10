// 本文件清理未开启正式入库时的服务器临时候选资料，复用完整删除事务，避免只清简历库却遗留聊天和附件。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// unlinkedRetentionCandidate 只携带删除路由和时间，不携带候选人原文或联系方式。
type unlinkedRetentionCandidate struct {
	Tenant, User, Platform, Source string
	ActiveAt                       time.Time
}

// unlinkedRetentionSQL 按同一团队的平台原生身份汇总真实活动；任一已知手机号、正式来源或近期沟通均保护整个同人来源。
const unlinkedRetentionSQL = `WITH identities AS (
 SELECT i.tenant_id,i.user_id,i.payload->>'platform_id' AS platform,i.payload->>'platform_candidate_id' AS source,c.id AS candidate,
 coalesce(nullif(c.payload#>>'{online_profile,updated_at}','')::timestamptz,nullif(c.updated_at,'')::timestamptz) AS profile_at,
 regexp_replace(coalesce(nullif(c.payload->>'Phone',''),c.payload->>'phone',''),'[[:space:]()（）-]','','g') ~ '^(1[3-9][0-9]{9}|(\+|00)[1-9][0-9]{7,14}|861[3-9][0-9]{9})$' AS has_phone,
 (jsonb_typeof(c.payload->'online_profile')='object' AND c.payload->'online_profile'<>'{}'::jsonb) AS has_online
 FROM auto_reply_cloud_entities i JOIN auto_reply_cloud_entities c ON c.tenant_id=i.tenant_id AND c.user_id=i.user_id AND c.kind='candidates' AND c.id=i.payload->>'candidate_id'
 WHERE i.kind='identities' AND coalesce(i.payload->>'platform_candidate_id','')<>''
 ), activity AS (
 SELECT i.tenant_id,i.user_id,i.platform,i.source,i.profile_at,i.has_phone,i.has_online,
 (SELECT max(nullif(m.payload->>'platform_sent_at','')::timestamptz) FROM auto_reply_cloud_entities cv JOIN auto_reply_cloud_entities m ON m.tenant_id=cv.tenant_id AND m.user_id=cv.user_id AND m.kind='messages' AND m.payload->>'conversation_id'=cv.id
 WHERE cv.tenant_id=i.tenant_id AND cv.user_id=i.user_id AND cv.kind='conversations' AND cv.payload->>'candidate_id'=i.candidate AND m.payload->>'direction' IN ('self','candidate') AND nullif(m.payload->>'platform_sent_at','')::timestamptz<=now()) AS communication_at
 FROM identities i
 ), grouped AS (
 SELECT tenant_id,min(user_id::text) AS user_id,platform,source,greatest(max(profile_at),max(communication_at)) AS active_at,bool_or(has_phone) AS has_phone,bool_or(has_online) AS has_online
 FROM activity GROUP BY tenant_id,platform,source
 ) SELECT tenant_id::text,user_id,platform,source,active_at FROM grouped g
 WHERE active_at<$1 AND has_online AND NOT has_phone AND NOT EXISTS(SELECT 1 FROM candidate_profiles cp WHERE cp.tenant_id=g.tenant_id AND
 ((cp.source_platform_id=g.platform AND cp.source_platform_candidate_id=g.source) OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=cp.tenant_id AND s.candidate_id=cp.id AND s.platform_id=g.platform AND s.platform_candidate_id=g.source)))`

// expiredUnlinkedCandidates 分批找出没有任何正式来源的过期临时资料，避免对同一原生人物重复计数。
func (s *CandidateRetentionService) expiredUnlinkedCandidates(ctx context.Context, cutoff time.Time) ([]unlinkedRetentionCandidate, error) {
	rows, err := s.store.db.QueryContext(ctx, unlinkedRetentionSQL+` ORDER BY active_at,tenant_id,platform,source LIMIT 100`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []unlinkedRetentionCandidate{}
	for rows.Next() {
		var item unlinkedRetentionCandidate
		if err = rows.Scan(&item.Tenant, &item.User, &item.Platform, &item.Source, &item.ActiveAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// purgeUnlinkedCandidate 在同一事务中重新核验活动，并构造即删的主档以执行已有完整删除触发器；该主档不会提交或显示到简历库。
func (s *CandidateRetentionService) purgeUnlinkedCandidate(ctx context.Context, job CandidateRetentionJob, item unlinkedRetentionCandidate) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, item.Tenant, true); err != nil {
		return err
	}
	var current unlinkedRetentionCandidate
	err = tx.QueryRowContext(ctx, unlinkedRetentionSQL+` AND tenant_id=$2 AND platform=$3 AND source=$4`, job.Cutoff, item.Tenant, item.Platform, item.Source).Scan(&current.Tenant, &current.User, &current.Platform, &current.Source, &current.ActiveAt)
	if errors.Is(err, sql.ErrNoRows) {
		return errCandidateRetentionChanged
	}
	if err != nil {
		return err
	}
	var id string
	var version int64
	err = tx.QueryRowContext(ctx, `INSERT INTO candidate_profiles(tenant_id,created_by_user_id,source_platform_id,source_platform_candidate_id,created_at,updated_at)
 VALUES($1,$2,$3,$4,$5,$5) RETURNING id::text,row_version`, current.Tenant, current.User, current.Platform, current.Source, current.ActiveAt).Scan(&id, &version)
	if err != nil {
		return err
	}
	scope := CandidateLibraryScope{TenantID: current.Tenant, IsAdmin: true, retentionCutoff: &job.Cutoff, retentionJobID: job.ID}
	if err = s.store.purgeCandidateLockedTx(ctx, tx, scope, id, version); err != nil {
		return err
	}
	return tx.Commit()
}
