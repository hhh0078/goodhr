// 本文件在同一 PostgreSQL 事务内合并旧设备档案，以云端原有记录、删除代际及幂等回执为准。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lib/pq"
)

// AutoReplyImportReceipt 是设备决定是否清理源资料所需的不可变回执。
type AutoReplyImportReceipt struct {
	Digest     string `json:"digest"`
	Accepted   int    `json:"accepted"`
	Reconciled int    `json:"reconciled"`
	Suppressed int    `json:"suppressed"`
	FileCount  int    `json:"file_count"`
}

// ImportAutoReplyState 先暂存整包，再按所有身份标记传播删除范围，合并和保存回执同时提交。
func (s *PostgresAutoReplyStore) ImportAutoReplyState(ctx context.Context, tenant, user, digest string, changes []AutoReplyStateChange, fileCount int, install ...func(map[string]bool) error) (AutoReplyImportReceipt, error) {
	rec := AutoReplyImportReceipt{Digest: digest, FileCount: fileCount}
	decoded, e := hex.DecodeString(digest)
	if e != nil || len(decoded) != 32 || digest != strings.ToLower(digest) || fileCount < 0 {
		return rec, newAutoReplyValidationError("迁移资料摘要或文件数无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rec, err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, tenant, false); err != nil {
		return rec, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO auto_reply_state_owners(tenant_id,user_id) SELECT tenant_id,id FROM users WHERE tenant_id=$1 AND id=$2 AND status='active' ON CONFLICT DO NOTHING", tenant, user)
	if err != nil {
		return rec, err
	}
	var active bool
	err = tx.QueryRowContext(ctx, "SELECT u.status='active' FROM auto_reply_state_owners o JOIN users u ON u.tenant_id=o.tenant_id AND u.id=o.user_id WHERE o.tenant_id=$1 AND o.user_id=$2 FOR UPDATE OF o", tenant, user).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !active {
		return rec, ErrAutoReplyForbidden
	}
	if err != nil {
		return rec, err
	}
	err = tx.QueryRowContext(ctx, "SELECT accepted,reconciled,suppressed,file_count FROM auto_reply_import_receipts WHERE tenant_id=$1 AND user_id=$2 AND digest=$3", tenant, user, digest).Scan(&rec.Accepted, &rec.Reconciled, &rec.Suppressed, &rec.FileCount)
	if err == nil {
		return rec, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return rec, err
	}
	if err = stageAutoReplyImport(ctx, tx, tenant, changes); err != nil {
		return rec, err
	}
	// 明确历史删除的来源即使没有云端关联收据，也不能用旧设备缓存复活。精确核对替代简历的云端编号允许已重新建档的新代资料导入。
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE ar_import_blocked(k text PRIMARY KEY) ON COMMIT DROP`)
	if err != nil {
		return rec, err
	}
	_, err = tx.ExecContext(ctx, `WITH RECURSIVE roots(k) AS (
 SELECT s.kind||':'||s.id FROM ar_import_stage s JOIN auto_reply_deleted_entity_keys d ON d.tenant_id=$1 AND d.user_id=$2 AND d.key_hash=encode(sha256(convert_to(s.kind||':'||s.id,'UTF8')),'hex')
 UNION SELECT s.kind||':'||s.id FROM ar_import_stage s JOIN candidate_deletions d ON d.tenant_id=$1 AND s.parents @> ARRAY['cloud:'||d.candidate_id::text]
 UNION SELECT 'candidates:'||substr(s.id,11) FROM ar_import_stage s JOIN candidate_deletions d ON d.tenant_id=$1 AND s.kind='cloud_links' AND s.id LIKE 'candidate:%' AND d.candidate_id::text=s.payload->>'value'
 UNION SELECT s.kind||':'||s.id FROM ar_import_stage s JOIN candidate_deletion_keys d ON d.tenant_id=$1 AND ((d.kind='source' AND d.identity_hash=s.source_hash) OR (d.kind='phone' AND d.identity_hash=s.phone_hash))
 WHERE d.replacement_candidate_id IS NULL OR NOT EXISTS(SELECT 1 FROM ar_import_stage l JOIN candidate_profiles p ON p.tenant_id=$1 AND p.id=d.replacement_candidate_id AND p.id::text=l.payload->>'value' WHERE l.kind='cloud_links' AND l.id='candidate:'||s.candidate_key)
 UNION SELECT 'candidates:'||s.candidate_key FROM ar_import_stage s JOIN candidate_deletion_keys d ON d.tenant_id=$1 AND d.kind='source' AND d.identity_hash=s.source_hash WHERE s.candidate_key<>'' AND (d.replacement_candidate_id IS NULL OR NOT EXISTS(SELECT 1 FROM ar_import_stage l JOIN candidate_profiles p ON p.id=d.replacement_candidate_id AND p.tenant_id=$1 AND p.id::text=l.payload->>'value' WHERE l.kind='cloud_links' AND l.id='candidate:'||s.candidate_key))
 UNION SELECT s.kind||':'||s.id FROM ar_import_stage s CROSS JOIN LATERAL unnest(s.parents) parent JOIN auto_reply_deleted_entity_keys d ON d.tenant_id=$1 AND d.user_id=$2 AND d.key_hash=encode(sha256(convert_to(parent,'UTF8')),'hex')
 ), tree(k) AS (SELECT k FROM roots UNION SELECT s.kind||':'||s.id FROM ar_import_stage s JOIN tree t ON s.parents @> ARRAY[t.k])
 INSERT INTO ar_import_blocked SELECT k FROM tree ON CONFLICT DO NOTHING`, tenant, user)
	if err != nil {
		return rec, err
	}
	// 老设备记录只带用户编号。遇到一个仍然有效但不属于本次用户/团队的岗位或简历，整包保持原样，不污染当前团队。
	var wrongScope bool
	err = tx.QueryRowContext(ctx, `WITH fresh AS (SELECT s.*,coalesce(nullif(s.payload->>'position_id',''),nullif(s.payload#>>'{conversation,position_id}',''),nullif(s.payload#>>'{position,position,id}','')) AS pos FROM ar_import_stage s WHERE NOT EXISTS(SELECT 1 FROM ar_import_blocked b WHERE b.k=s.kind||':'||s.id)) SELECT EXISTS(
 SELECT 1 FROM fresh s LEFT JOIN positions p ON p.id::text=s.pos LEFT JOIN users u ON u.id=p.user_id WHERE s.pos IS NOT NULL AND (p.user_id IS DISTINCT FROM $2::uuid OR u.tenant_id IS DISTINCT FROM $1::uuid)
 UNION ALL SELECT 1 FROM fresh s LEFT JOIN candidate_profiles cp ON cp.id::text=s.payload->>'value' WHERE s.kind='cloud_links' AND left(s.id,10)='candidate:' AND cp.tenant_id IS DISTINCT FROM $1::uuid)`, tenant, user).Scan(&wrongScope)
	if err != nil {
		return rec, err
	}
	if wrongScope {
		return rec, ErrAutoReplyForbidden
	}

	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE b.k IS NOT NULL),count(*) FILTER(WHERE b.k IS NULL AND e.id IS NOT NULL) FROM ar_import_stage s LEFT JOIN ar_import_blocked b ON b.k=s.kind||':'||s.id LEFT JOIN auto_reply_cloud_entities e ON e.tenant_id=$1 AND e.user_id=$2 AND e.kind=s.kind AND e.id=s.id`, tenant, user).Scan(&rec.Suppressed, &rec.Reconciled)
	if err != nil {
		return rec, err
	}
	// 如果已有同类型记录，为新增记录分配该类型尚未使用的顺序，避免另一台设备的SQLite自增主键碰撞。
	result, err := tx.ExecContext(ctx, `WITH mx AS (SELECT kind,max(sequence) n FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 GROUP BY kind), fresh AS (
 SELECT s.*,mx.n FROM ar_import_stage s LEFT JOIN mx ON mx.kind=s.kind WHERE NOT EXISTS(SELECT 1 FROM ar_import_blocked b WHERE b.k=s.kind||':'||s.id) AND NOT EXISTS(SELECT 1 FROM auto_reply_cloud_entities e WHERE e.tenant_id=$1 AND e.user_id=$2 AND e.kind=s.kind AND e.id=s.id))
 INSERT INTO auto_reply_cloud_entities(tenant_id,user_id,kind,id,payload,revision,sequence,updated_at,message_order,processed_status,parents)
 SELECT $1,$2,kind,id,payload,revision,CASE WHEN sequence>0 AND n IS NOT NULL THEN n+row_number() OVER(PARTITION BY kind ORDER BY sequence,id) ELSE sequence END,updated_at,message_order,processed_status,parents FROM fresh`, tenant, user)
	if err != nil {
		return rec, err
	}
	added, err := result.RowsAffected()
	if err != nil {
		return rec, err
	}
	rec.Accepted = int(added)
	if rec.Accepted+rec.Reconciled+rec.Suppressed != len(changes) {
		return rec, newAutoReplyValidationError("迁移的记录数未能核对，请保留原资料")
	}
	if len(install) > 0 && install[0] != nil {
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT payload->>'relative_path' FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='attachments' AND nullif(payload->>'relative_path','') IS NOT NULL", tenant, user)
		if err != nil {
			return rec, err
		}
		live := map[string]bool{}
		for rows.Next() {
			var path string
			if err = rows.Scan(&path); err != nil {
				rows.Close()
				return rec, err
			}
			live[path] = true
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return rec, err
		}
		rows.Close()
		if err = install[0](live); err != nil {
			return rec, err
		}
	}
	if rec.Accepted > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE auto_reply_state_owners SET version=version+1 WHERE tenant_id=$1 AND user_id=$2", tenant, user); err != nil {
			return rec, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO auto_reply_import_receipts(tenant_id,user_id,digest,accepted,reconciled,suppressed,file_count) VALUES($1,$2,$3,$4,$5,$6,$7)", tenant, user, digest, rec.Accepted, rec.Reconciled, rec.Suppressed, fileCount)
	if err != nil {
		return rec, err
	}
	return rec, tx.Commit()
}

// stageAutoReplyImport 使用COPY逐行装入本事务的临时表，解析业务父键和经过规范化的删除身份摘要。
func stageAutoReplyImport(ctx context.Context, tx *sql.Tx, tenant string, changes []AutoReplyStateChange) error {
	_, err := tx.ExecContext(ctx, `CREATE TEMP TABLE ar_import_stage(kind text,id text,payload jsonb,revision bigint,sequence bigint,updated_at text,message_order double precision,processed_status text,parents text[],source_hash text,phone_hash text,candidate_key text,PRIMARY KEY(kind,id)) ON COMMIT DROP`)
	if err != nil {
		return err
	}
	ins, err := tx.PrepareContext(ctx, pq.CopyIn("ar_import_stage", "kind", "id", "payload", "revision", "sequence", "updated_at", "message_order", "processed_status", "parents", "source_hash", "phone_hash", "candidate_key"))
	if err != nil {
		return err
	}
	defer ins.Close()
	for _, c := range changes {
		if c.Deleted || strings.HasPrefix(c.Kind, "runtime_") {
			return newAutoReplyValidationError("旧设备档案不能提交删除或服务器控制指令")
		}
		parents, err := stateParents(c)
		if err != nil {
			return err
		}
		var source, phone, cand string
		switch c.Kind {
		case "candidates":
			var p struct{ Phone string }
			if err = json.Unmarshal(c.Payload, &p); err != nil {
				return err
			}
			cand = c.ID
			if n := normalizeCandidatePhone(p.Phone); n != "" {
				phone = purgeDigest(tenant, n)
			}
		case "identities":
			var p struct {
				P string `json:"platform_id"`
				K string `json:"platform_candidate_id"`
				C string `json:"candidate_id"`
			}
			if err = json.Unmarshal(c.Payload, &p); err != nil {
				return err
			}
			source = purgeSourceHash(p.P, p.K)
			cand = p.C
		}
		// 旧设备可提供历史，待投递的邮件、推荐、动作及设备文件路径不能直接进入服务器执行队列。
		if c.Kind == "purge_files" {
			c.Kind = "legacy_purge_files"
		} else if c.Kind == "outbox" || c.Kind == "proposal_receipts" || c.Kind == "recommendations" {
			var state struct {
				Status string `json:"status"`
			}
			if err = json.Unmarshal(c.Payload, &state); err != nil {
				return err
			}
			if state.Status != "completed" {
				c.Kind = "legacy_" + c.Kind
			}
		}

		if _, err = ins.ExecContext(ctx, c.Kind, c.ID, string(c.Payload), c.Revision, c.Sequence, c.UpdatedAt, c.MessageOrder, c.ProcessedStatus, pq.Array(parents), source, phone, cand); err != nil {
			return err
		}
	}
	if _, err = ins.ExecContext(ctx); err != nil {
		return err
	}
	if err = ins.Close(); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "CREATE INDEX ON ar_import_stage USING gin(parents); ANALYZE ar_import_stage")
	return err
}
