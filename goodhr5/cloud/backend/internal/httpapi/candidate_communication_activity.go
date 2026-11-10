// 本文件只按真实平台消息时间延长临时简历的保留期；轮询、处理状态修改和历史重放不按当前时间续期。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// candidateCommunicationTouch 是不含正文和联系方式的沟通时间投影。
type candidateCommunicationTouch struct {
	ConversationID string    `json:"conversation_id"`
	SentAt         time.Time `json:"sent_at"`
}

// touchCandidateCommunicationsTx 在原状态事务提交前更新同来源简历的最后沟通时间，和完整删除共用团队锁。
func touchCandidateCommunicationsTx(ctx context.Context, tx *sql.Tx, tenant, user string, changes []AutoReplyStateChange) error {
	touches := []candidateCommunicationTouch{}
	now := time.Now().UTC()
	for _, change := range changes {
		if change.Kind != "messages" || change.Deleted {
			continue
		}
		var m struct {
			ConversationID string     `json:"conversation_id"`
			Direction      string     `json:"direction"`
			PlatformSentAt *time.Time `json:"platform_sent_at"`
		}
		if err := json.Unmarshal(change.Payload, &m); err != nil {
			return err
		}
		if m.ConversationID == "" || m.PlatformSentAt == nil || m.PlatformSentAt.IsZero() || m.PlatformSentAt.After(now) || (m.Direction != "candidate" && m.Direction != "self") {
			continue
		}
		touches = append(touches, candidateCommunicationTouch{m.ConversationID, m.PlatformSentAt.UTC()})
	}
	if len(touches) == 0 {
		return nil
	}
	raw, err := json.Marshal(touches)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH touched AS (
 SELECT DISTINCT c.payload->>'platform_id' AS platform_id,i.payload->>'platform_candidate_id' AS source_id,t.sent_at
 FROM jsonb_to_recordset($3::jsonb) AS t(conversation_id text,sent_at timestamptz)
 JOIN auto_reply_cloud_entities c ON c.tenant_id=$1 AND c.user_id=$2 AND c.kind='conversations' AND c.id=t.conversation_id
 JOIN auto_reply_cloud_entities i ON i.tenant_id=c.tenant_id AND i.user_id=c.user_id AND i.kind='identities' AND i.id=c.payload->>'platform_identity_id'
 ), targets AS (
 SELECT cp.id,max(t.sent_at) AS at FROM candidate_profiles cp JOIN touched t ON
 (cp.source_platform_id=t.platform_id AND cp.source_platform_candidate_id=t.source_id)
 OR EXISTS(SELECT 1 FROM candidate_profile_sources s WHERE s.tenant_id=cp.tenant_id AND s.candidate_id=cp.id AND s.platform_id=t.platform_id AND s.platform_candidate_id=t.source_id)
 WHERE cp.tenant_id=$1 GROUP BY cp.id
 ) UPDATE candidate_profiles cp SET last_communication_at=targets.at FROM targets
 WHERE cp.id=targets.id AND (cp.last_communication_at IS NULL OR cp.last_communication_at<targets.at)`, tenant, user, string(raw))
	return err
}
