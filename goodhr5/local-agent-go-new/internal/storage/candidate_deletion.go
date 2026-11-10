// Package storage 本文件用云端来源摘要核对筛选记录，并在同一事务清理对应的本机操作记录。
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
)

// PurgeCandidateScreenings 按已验证账号和平台稳定身份匹配历史快照，姓名相同的其他候选人不会被选中。
func (s *Store) PurgeCandidateScreenings(ctx context.Context, owner string, sourceHashes []string) error {
	if len(sourceHashes) == 0 {
		return nil
	}
	hashes := make(map[string]bool, len(sourceHashes))
	for _, h := range sourceHashes {
		hashes[h] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT attempt_id,COALESCE(json_extract(payload_json,'$.platform_id'),''),
		COALESCE(json_extract(payload_json,'$.platform_candidate_id'),'')
		FROM screening_records WHERE owner_id=?`, owner)
	if err != nil {
		return err
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var attempt, platform, id string
		if err = rows.Scan(&attempt, &platform, &id); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(platform) + "\x00" + strings.TrimSpace(id)))
		if id != "" && platform != "" && hashes[hex.EncodeToString(sum[:])] {
			matches = append(matches, attempt)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, attempt := range matches {
		if err = deleteScreeningAttempt(ctx, tx, owner, attempt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PurgeRejectedScreening 清理设备离线时创建、重传后被云端明确拒绝的旧筛选，不重复补传。
func (s *Store) PurgeRejectedScreening(ctx context.Context, owner, attempt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = deleteScreeningAttempt(ctx, tx, owner, attempt); err != nil {
		return err
	}
	return tx.Commit()
}

// deleteScreeningAttempt 先用该次筛选的账号、任务与指纹确认关联动作，再移除筛选正文。
func deleteScreeningAttempt(ctx context.Context, tx *sql.Tx, owner, attempt string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM candidate_records WHERE fingerprint<>'' AND (task_id,platform_id,fingerprint) IN
		(SELECT task_id,json_extract(payload_json,'$.platform_id'),json_extract(payload_json,'$.candidate_key') FROM screening_records WHERE owner_id=? AND attempt_id=?)`, owner, attempt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM screening_records WHERE owner_id=? AND attempt_id=?`, owner, attempt)
	return err
}
