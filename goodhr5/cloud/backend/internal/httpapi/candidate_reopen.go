// 本文件验证绑定设备提供的删后入站与附件证据，并在原建档事务中限制来源和迟到重放。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"
)

// candidateReopenProof 仅接收稳定消息键摘要与平台发送时间；不接收入站正文或附件内容。
type candidateReopenProof struct {
	ConversationID        string    `json:"conversation_id,omitempty"`
	LocalCandidateID      string    `json:"local_candidate_id,omitempty"`
	DeletionSequence      int64     `json:"deletion_sequence"`
	InboundMessageHash    string    `json:"inbound_message_hash"`
	InboundAt             time.Time `json:"inbound_at"`
	AttachmentMessageHash string    `json:"attachment_message_hash"`
	AttachmentAt          time.Time `json:"attachment_at"`
}

// candidateReopenReceipt 记录此删除代次和原平台来源实际绑定的新云编号、最近附件及账号归属。
type candidateReopenReceipt struct {
	sequence                          int64
	candidateID, actorHash, proofHash string
	attachmentAt                      time.Time
}

var errReopenTargetConflict = errors.New("设备曾永久删除这份云简历，云端仍保留同一编号，请先核对简历库中的旧档。新来信和附件已保留")

// validPurgeHash 只接受小写 SHA-256 十六进制摘要，不把任意明文或非规范键存入收据。
func validPurgeHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// candidateReopenHashes 在云端重新校验证据格式和事件上下限，以规范 UTC 字符串生成可重放摘要。
func candidateReopenHashes(in CandidateProfileInput, seq int64, cutoff, now time.Time) (string, string, error) {
	p := in.ReopenProof
	if p == nil || seq <= 0 || seq != p.DeletionSequence || !structuredCandidateInput(in) || in.AutoReplyPositionID == "" || strings.TrimSpace(in.ReopenPlatformAccount) == "" || normalizeCandidatePhone(in.Phone) == "" || cutoff.IsZero() ||
		!p.InboundAt.After(cutoff) || !p.AttachmentAt.After(cutoff) || p.InboundAt.After(now) || p.AttachmentAt.After(now) ||
		!validPurgeHash(p.InboundMessageHash) || !validPurgeHash(p.AttachmentMessageHash) {
		return "", "", errCandidateDeleted
	}
	actor := purgeDigest(strings.ToLower(in.UserEmail), in.ReopenPlatformAccount)
	hash := purgeDigest(p.InboundMessageHash, p.InboundAt.UTC().Format(time.RFC3339Nano), p.AttachmentMessageHash, p.AttachmentAt.UTC().Format(time.RFC3339Nano))
	return actor, hash, nil
}

// checkReopenReceipt 只复用同代同来源、同招聘账号已建的新档；相同证据重试不重复更新资料版本。
func checkReopenReceipt(old candidateReopenReceipt, currentID, actor, hash string, attachmentAt time.Time) (bool, error) {
	if old.candidateID != currentID || (currentID != "" && old.actorHash != actor) {
		return false, errCandidateDeleted
	}
	if currentID == "" {
		return false, nil
	}
	if old.proofHash == hash {
		return true, nil
	}
	if !attachmentAt.After(old.attachmentAt) {
		return false, errCandidateDeleted
	}
	return false, nil
}

// verifyReopenTx 在租户共享删除锁与来源锁内取得该来源的最新事件，禁止挑选旧代次或其他人的删除事件。
func verifyReopenTx(ctx context.Context, tx *sql.Tx, tenant, source, currentID string, in CandidateProfileInput) (candidateReopenReceipt, bool, error) {
	var seq int64
	var cutoff, now time.Time
	var authorized bool
	actor := purgeDigest(strings.ToLower(in.UserEmail))
	err := tx.QueryRowContext(ctx, `SELECT sequence,deleted_at,clock_timestamp(),($4 OR audiences ? $3) FROM candidate_deletions
 WHERE tenant_id=$1 AND source_hashes ? $2 ORDER BY sequence DESC LIMIT 1 FOR UPDATE`, tenant, source, actor, in.ReopenIsAdmin).Scan(&seq, &cutoff, &now, &authorized)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !authorized) {
		return candidateReopenReceipt{}, false, errCandidateDeleted
	}
	if err != nil {
		return candidateReopenReceipt{}, false, err
	}
	var old candidateReopenReceipt
	var oldSource string
	var proofType string
	err = tx.QueryRowContext(ctx, `SELECT source_hash,candidate_id::text,actor_hash,proof_hash,attachment_at,proof_type FROM candidate_reopen_receipts WHERE deletion_sequence=$1 LIMIT 1`, seq).Scan(&oldSource, &old.candidateID, &old.actorHash, &old.proofHash, &old.attachmentAt, &proofType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return old, false, err
	}
	if oldSource != "" && oldSource != source {
		return old, false, errCandidateDeleted
	}
	if proofType == "online_profile" {
		// 在线新档必须走服务端双字段续写证明；旧客户端不能把它当成旧附件收据使用。
		return old, false, errCandidateDeleted
	}
	if in.ReopenServerWitness && currentID != "" {
		var locallyDeleted bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM auto_reply_cloud_entities e JOIN users u ON u.id=e.user_id AND u.tenant_id=e.tenant_id
		 WHERE e.tenant_id=$1 AND lower(u.email)=lower($2) AND e.kind='purge_keys' AND e.id=jsonb_build_array('cloud:'||$3::text)::text)`, tenant, in.UserEmail, currentID).Scan(&locallyDeleted)
		if err != nil {
			return old, false, err
		}
		if locallyDeleted {
			return old, false, errReopenTargetConflict
		}
	}
	// 同步数据可能带着另一份旧数据库的删除流水号。仅给同一有效替代简历对齐经服务器真实采集的全新证据。
	actorHash := purgeDigest(strings.ToLower(in.UserEmail), in.ReopenPlatformAccount)
	legacyActor := purgeDigest(strings.ToLower(in.UserEmail), canonicalReplyAccountKey(in.PlatformID, in.ReopenPlatformAccount))
	p := in.ReopenProof
	resolveGeneration := p != nil && p.DeletionSequence != seq
	resolveActor := old.candidateID != "" && old.actorHash != actorHash
	if resolveGeneration || resolveActor {
		if p == nil || !in.ReopenServerWitness || p.ConversationID == "" || p.LocalCandidateID == "" || p.DeletionSequence < seq || old.candidateID == "" || old.candidateID != currentID || (resolveActor && old.actorHash != legacyActor) {
			return old, false, errCandidateDeleted
		}
		valid, err := verifiedReplyReopenEvidence(ctx, tx, tenant, source, currentID, cutoff, in)
		if err != nil {
			return old, false, err
		}
		if !valid {
			return old, false, errCandidateDeleted
		}
		aligned := *p
		aligned.DeletionSequence = seq
		in.ReopenProof = &aligned
	}
	actorHash, proofHash, err := candidateReopenHashes(in, seq, cutoff, now)
	if err != nil {
		return old, false, err
	}
	if resolveActor {
		actorHash = old.actorHash
	}
	replay, err := checkReopenReceipt(old, currentID, actorHash, proofHash, in.ReopenProof.AttachmentAt)
	return candidateReopenReceipt{sequence: seq, actorHash: actorHash, proofHash: proofHash, attachmentAt: in.ReopenProof.AttachmentAt}, replay, err
}

// saveReopenReceiptTx 在候选人、岗位和来源均保存后原子登记收据；新来源已重新绑定，旧事件不再按它清理新档。
func saveReopenReceiptTx(ctx context.Context, tx *sql.Tx, tenant, source, id string, seq int64, r candidateReopenReceipt) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO candidate_reopen_receipts(deletion_sequence,source_hash,actor_hash,proof_hash,attachment_at,candidate_id)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(deletion_sequence,source_hash) DO UPDATE SET proof_hash=excluded.proof_hash,attachment_at=excluded.attachment_at`, seq, source, r.actorHash, r.proofHash, r.attachmentAt, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE candidate_deletion_keys SET replacement_candidate_id=$3 WHERE tenant_id=$1 AND kind='source' AND identity_hash=$2`, tenant, source, id)
	return err
}

// verifyMemoryReopenLocked 在开发存储中按与 PostgreSQL 相同的证据、受众、手机号冲突和来源规则核验。
func (s *MemoryCandidateStore) verifyMemoryReopenLocked(in CandidateProfileInput, source string) (candidateReopenReceipt, bool, error) {
	for i := len(s.deletions) - 1; i >= 0; i-- {
		d := s.deletions[i]
		if d.tenant != in.TenantID || !slices.Contains(d.event.SourceHashes, source) {
			continue
		}
		if _, ok := d.recipients[strings.ToLower(in.UserEmail)]; !ok && !in.ReopenIsAdmin {
			return candidateReopenReceipt{}, false, errCandidateDeleted
		}
		actor, hash, err := candidateReopenHashes(in, d.event.Sequence, d.event.DeletedAt, s.now())
		if err != nil {
			return candidateReopenReceipt{}, false, err
		}
		currentID := ""
		for _, c := range s.profiles {
			if c.TenantID != in.TenantID {
				continue
			}
			same := purgeSourceHash(c.PlatformID, c.PlatformCandidateID) == source
			for _, cs := range c.Sources {
				same = same || purgeSourceHash(cs.PlatformID, cs.PlatformCandidateID) == source
			}
			if same {
				currentID = c.ID
			} else if normalizeCandidatePhone(c.Phone) == normalizeCandidatePhone(in.Phone) {
				return candidateReopenReceipt{}, false, ErrCandidateDuplicate
			}
		}
		key := in.TenantID + "|" + source
		for storedKey, r := range s.reopenReceipts {
			if r.sequence == d.event.Sequence && storedKey != key {
				return candidateReopenReceipt{}, false, errCandidateDeleted
			}
		}
		old := s.reopenReceipts[key]
		replay, err := checkReopenReceipt(old, currentID, actor, hash, in.ReopenProof.AttachmentAt)
		return candidateReopenReceipt{sequence: d.event.Sequence, candidateID: currentID, actorHash: actor, proofHash: hash, attachmentAt: in.ReopenProof.AttachmentAt}, replay, err
	}
	return candidateReopenReceipt{}, false, errCandidateDeleted
}

// inheritedReopenSourcesTx 取上一代仍无其他有效归属的来源；再次删除时让其旧回包一同失效。
func inheritedReopenSourcesTx(ctx context.Context, tx *sql.Tx, tenant, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT src.h
 FROM candidate_reopen_receipts r JOIN candidate_deletions d ON d.sequence=r.deletion_sequence
 CROSS JOIN LATERAL jsonb_array_elements_text(d.source_hashes) src(h)
 JOIN candidate_deletion_keys k ON k.tenant_id=d.tenant_id AND k.kind='source' AND k.identity_hash=src.h
 WHERE d.tenant_id=$1 AND r.candidate_id=$2 AND (k.replacement_candidate_id IS NULL OR k.replacement_candidate_id=$2)
 AND NOT EXISTS (SELECT 1 FROM candidate_deletions newer WHERE newer.tenant_id=d.tenant_id AND newer.sequence>d.sequence AND newer.source_hashes ? src.h)`, tenant, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var h string
		if err = rows.Scan(&h); err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// inheritedMemoryReopenSourcesLocked 将上一代统一截止扩展到其余未被认领的来源，保留已由他人新建的来源。
func (s *MemoryCandidateStore) inheritedMemoryReopenSourcesLocked(tenant, id string) []string {
	var result []string
	for _, r := range s.reopenReceipts {
		if r.candidateID != id {
			continue
		}
		for _, prior := range s.deletions {
			if prior.tenant != tenant || prior.event.Sequence != r.sequence {
				continue
			}
			for _, h := range prior.event.SourceHashes {
				if s.purgeKeys[tenant+"|reusedSource|"+h] {
					continue
				}
				newer := slices.ContainsFunc(s.deletions, func(d memoryCandidateDeletion) bool {
					return d.tenant == tenant && d.event.Sequence > r.sequence && slices.Contains(d.event.SourceHashes, h)
				})
				if !newer {
					result = append(result, h)
				}
			}
		}
	}
	return result
}
