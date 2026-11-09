// 本文件根据云端真实来信、附件及设备任务收据，核对旧系统迁移后的候选人重新建档证明。
package httpapi

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// canonicalReplyAccountKey 对已绑定的稳定标识保持原值，为显示名采用与现有设备一致的账号摘要。
func canonicalReplyAccountKey(platform, label string) string {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(label, "bound:") && validPurgeHash(strings.TrimPrefix(label, "bound:")) {
		return label
	}
	return "bound:" + replyImportHash(strings.TrimSpace(platform)+"\n"+label)
}

// verifiedReplyReopenEvidence 只匹配同一成员、岗位、账号、会话和平台来源；导入行不能制造云编排专属采集收据。
func verifiedReplyReopenEvidence(ctx context.Context, tx *sql.Tx, tenant, source, candidate string, cutoff time.Time, in CandidateProfileInput) (bool, error) {
	p := in.ReopenProof
	var ok bool
	err := tx.QueryRowContext(ctx, `WITH entities AS NOT MATERIALIZED (
	 SELECT e.* FROM auto_reply_cloud_entities e JOIN users u ON u.id=e.user_id AND u.tenant_id=e.tenant_id
	 WHERE e.tenant_id=$1 AND lower(u.email)=lower($2))
	 SELECT EXISTS(SELECT 1 FROM entities c
	 JOIN entities i ON i.kind='identities' AND i.id=c.payload->>'platform_identity_id'
	 JOIN entities m ON m.kind='messages' AND m.payload->>'conversation_id'=c.id
	 JOIN entities r ON r.kind='messages' AND r.payload->>'conversation_id'=c.id
	 JOIN entities a ON a.kind='attachments' AND a.payload->>'conversation_id'=c.id AND a.payload->>'source_message_id'=r.id
	 JOIN entities cmd ON cmd.kind='runtime_commands' AND cmd.payload->>'conversation_id'=c.id
	 JOIN candidate_deletion_keys d ON d.tenant_id=c.tenant_id AND d.kind='source' AND d.identity_hash=$3 AND d.replacement_candidate_id=$4
	 WHERE c.kind='conversations' AND c.id=$5 AND c.payload->>'candidate_id'=$6 AND i.payload->>'candidate_id'=$6
	 AND c.payload->>'position_id'=$7 AND c.payload->>'platform_id'=$8 AND $9 IN (c.payload->>'platform_account_id','bound:'||encode(sha256(convert_to($8||chr(10)||(c.payload->>'platform_account_id'),'UTF8')),'hex'))
	 AND i.payload->>'platform_id'=$8 AND i.payload->>'platform_account_id' IN (c.payload->>'platform_account_id',$9) AND i.payload->>'platform_candidate_id'=$10
	 AND c.payload#>>'{deletion_boundary,source_hash}'=$3 AND (c.payload#>>'{deletion_boundary,sequence}')::bigint=$11
	 AND (c.payload#>>'{deletion_boundary,deleted_at}')::timestamptz >= $12
	 AND (c.payload#>>'{deletion_boundary,deleted_at}')::timestamptz < LEAST($13::timestamptz,$14::timestamptz)
	 AND c.payload->>'reopen_inbound_hash'=$15 AND (c.payload->>'reopen_inbound_at')::timestamptz=$13
	 AND m.payload->>'direction'='candidate' AND (m.payload->>'platform_sent_at')::timestamptz=$13
	 AND encode(sha256(convert_to(coalesce(nullif(m.payload->>'platform_message_id',''),m.payload->>'fingerprint'),'UTF8')),'hex')=$15
	 AND r.payload->>'direction'='candidate' AND r.payload->>'message_type'='resume' AND (r.payload->>'platform_sent_at')::timestamptz=$14
	 AND encode(sha256(convert_to(coalesce(nullif(r.payload->>'platform_message_id',''),r.payload->>'fingerprint'),'UTF8')),'hex')=$16
	 AND nullif(a.payload->>'relative_path','') IS NOT NULL AND nullif(a.payload->>'sha256','') IS NOT NULL
	 AND cmd.payload->>'action'='collect_resume' AND cmd.payload->>'status'='completed' AND cmd.payload->>'position_id'=$7
	 AND nullif(cmd.payload->>'platform_thread_id','')=c.payload->>'platform_thread_id' AND $9 IN (cmd.payload->>'platform_account_id','bound:'||encode(sha256(convert_to($8||chr(10)||(cmd.payload->>'platform_account_id'),'UTF8')),'hex'))
	 AND cmd.payload->>'source_key'=coalesce(nullif(r.payload->>'platform_message_id',''),r.payload->>'fingerprint')
	 AND cmd.payload->'attachment_ids' ? a.id)`, tenant, in.UserEmail, source, candidate, p.ConversationID, p.LocalCandidateID, in.AutoReplyPositionID, in.PlatformID, canonicalReplyAccountKey(in.PlatformID, in.ReopenPlatformAccount), in.PlatformCandidateID, p.DeletionSequence, cutoff, p.InboundAt, p.AttachmentAt, p.InboundMessageHash, p.AttachmentMessageHash).Scan(&ok)
	return ok, err
}
