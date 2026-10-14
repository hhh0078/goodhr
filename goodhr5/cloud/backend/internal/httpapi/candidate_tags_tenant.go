// 本文件在个人账号加入团队时同步迁移标签和关联，保留个人范围并安全合并同名标签。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
)

// mergeCandidateTagTenantTx 在现有团队合并事务中迁移标签，先保留关联再合并重复候选人。
func mergeCandidateTagTenantTx(ctx context.Context, tx *sql.Tx, sourceTenantID, targetTenantID string, candidatePairs [][2]string) error {
	mappedCandidates := map[string]string{}
	for _, pair := range candidatePairs {
		mappedCandidates[pair[0]] = pair[1]
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,name,scope,created_by_email FROM candidate_tags WHERE tenant_id=$1 ORDER BY id FOR UPDATE`, sourceTenantID)
	if err != nil {
		return err
	}
	tags := []CandidateTag{}
	for rows.Next() {
		var tag CandidateTag
		if err = rows.Scan(&tag.ID, &tag.Name, &tag.Scope, &tag.CreatedByEmail); err != nil {
			rows.Close()
			return err
		}
		tags = append(tags, tag)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, tag := range tags {
		var targetID string
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM candidate_tags WHERE tenant_id=$1 AND lower(name)=lower($2) AND scope=$3 AND (scope='team' OR created_by_email=$4) FOR UPDATE`, targetTenantID, tag.Name, tag.Scope, tag.CreatedByEmail).Scan(&targetID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		merged := targetID != ""
		if !merged {
			targetID = tag.ID
			if _, err = tx.ExecContext(ctx, `UPDATE candidate_tags SET tenant_id=$1 WHERE id=$2`, targetTenantID, tag.ID); err != nil {
				return err
			}
		}
		links, err := tx.QueryContext(ctx, `SELECT candidate_id::text FROM candidate_tag_links WHERE tenant_id=$1 AND tag_id=$2`, sourceTenantID, tag.ID)
		if err != nil {
			return err
		}
		candidateIDs := []string{}
		for links.Next() {
			var id string
			if err = links.Scan(&id); err != nil {
				links.Close()
				return err
			}
			candidateIDs = append(candidateIDs, id)
		}
		if err = links.Err(); err != nil {
			links.Close()
			return err
		}
		links.Close()
		for _, candidateID := range candidateIDs {
			if mapped := mappedCandidates[candidateID]; mapped != "" {
				candidateID = mapped
			}
			// 新复合外键延期到事务提交，允许同一事务随后迁移候选人团队。
			if _, err = tx.ExecContext(ctx, `INSERT INTO candidate_tag_links(tenant_id,tag_id,candidate_id) VALUES($1,$2,$3) ON CONFLICT(tag_id,candidate_id) DO UPDATE SET tenant_id=EXCLUDED.tenant_id`, targetTenantID, targetID, candidateID); err != nil {
				return err
			}
		}
		if merged {
			if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_tags WHERE id=$1`, tag.ID); err != nil {
				return err
			}
		} else {
			if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_tag_links WHERE tenant_id=$1 AND tag_id=$2`, sourceTenantID, tag.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
