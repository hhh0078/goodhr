// 本文件在已有自动入库事务中复用团队手机号身份，并在来源冲突时拒绝盲目合并。
package httpapi

import (
	"context"
	"database/sql"
	"strings"
)

// structuredCandidateInput 判断本次是否为经过授权结构化接口校验的自动回复入库。
func structuredCandidateInput(input CandidateProfileInput) bool {
	_, ok := input.Ext["structured_fields"].(map[string]any)
	return ok
}

// automaticCandidatePhoneID 在持有手机号锁后确认唯一身份；历史重复或来源冲突交给人工处理。
func automaticCandidatePhoneID(ctx context.Context, tx *sql.Tx, tenantID, sourceID, phone string) (string, error) {
	if normalizeCandidatePhone(phone) == "" {
		return sourceID, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM candidate_profiles WHERE tenant_id=$1 AND (normalized_phone=$2 OR phone=$2) ORDER BY created_at,id LIMIT 2 FOR UPDATE`, tenantID, normalizeCandidatePhone(phone))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(ids) > 1 || (len(ids) == 1 && sourceID != "" && ids[0] != sourceID) {
		return "", ErrCandidateDuplicate
	}
	if len(ids) == 1 {
		return ids[0], nil
	}
	return sourceID, nil
}

// registerAutomaticCandidatePhone 在原事务登记实际保存的手机号，不改变已经归属其他简历的身份。
func registerAutomaticCandidatePhone(ctx context.Context, tx *sql.Tx, tenantID, candidateID, phone string) error {
	phone = normalizeCandidatePhone(phone)
	if phone == "" {
		return nil
	}
	var saved string
	err := tx.QueryRowContext(ctx, `INSERT INTO candidate_phone_identities(tenant_id,normalized_phone,candidate_id) VALUES($1,$2,$3) ON CONFLICT(tenant_id,normalized_phone) DO UPDATE SET updated_at=now() WHERE candidate_phone_identities.candidate_id=EXCLUDED.candidate_id RETURNING candidate_id::text`, tenantID, phone, candidateID).Scan(&saved)
	if err == sql.ErrNoRows || (err == nil && saved != strings.TrimSpace(candidateID)) {
		return ErrCandidateDuplicate
	}
	return err
}
