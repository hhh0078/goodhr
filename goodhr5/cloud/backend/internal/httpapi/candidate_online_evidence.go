// 本文件把在线资料绑定到同一成员、设备、岗位、会话和真实采集命令；删除后的旧消息不能作为新档依据。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// onlineReplyConversation 只读取核验所需路由及删除边界，不接收调用方自报的入站证据。
type onlineReplyConversation struct {
	CandidateID       string    `json:"candidate_id"`
	IdentityID        string    `json:"platform_identity_id"`
	PositionID        string    `json:"position_id"`
	PlatformID        string    `json:"platform_id"`
	AccountID         string    `json:"platform_account_id"`
	ThreadID          string    `json:"platform_thread_id"`
	CandidateName     string    `json:"candidate_name"`
	ReopenInboundHash string    `json:"reopen_inbound_hash"`
	ReopenInboundAt   time.Time `json:"reopen_inbound_at"`
	DeletionBoundary  *struct {
		SourceHash string    `json:"source_hash"`
		Sequence   int64     `json:"sequence"`
		DeletedAt  time.Time `json:"deleted_at"`
	} `json:"deletion_boundary"`
}

// onlineProfileEvidence 保存数据库已核实的新消息与采集时间，供同一事务登记新代次收据。
type onlineProfileEvidence struct {
	Conversation     onlineReplyConversation
	CommandID        string
	CapturedAt       time.Time
	InboundAt        time.Time
	InboundHash      string
	DeletionSequence int64
	DeletedAt        time.Time
	OldCandidateID   string
}

// readOnlineEntity 仅在已认证成员自己的权威命名空间读取固定业务实体。
func readOnlineEntity(ctx context.Context, tx *sql.Tx, tenant, user, kind, id string, target any) error {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind=$3 AND id=$4`, tenant, user, kind, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAutoReplyForbidden
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// onlineDeletionEvidence 锁住最新删除代次并验证受众，不接受旧流水号或其他团队的删除事件。
func onlineDeletionEvidence(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, source string) (onlineProfileEvidence, error) {
	var evidence onlineProfileEvidence
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT sequence,deleted_at,candidate_id::text,($4 OR audiences ? $3) FROM candidate_deletions WHERE tenant_id=$1 AND source_hashes ? $2 ORDER BY sequence DESC LIMIT 1 FOR UPDATE`, scope.TenantID, source, purgeDigest(strings.ToLower(scope.UserEmail)), scope.IsAdmin).Scan(&evidence.DeletionSequence, &evidence.DeletedAt, &evidence.OldCandidateID, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		if err := rejectPurgedKey(ctx, tx, scope.TenantID, "source", source); err != nil {
			return evidence, err
		}
		return evidence, nil
	}
	if err != nil {
		return evidence, err
	}
	if !allowed {
		return evidence, errCandidateDeleted
	}
	return evidence, nil
}

// verifyOnlineReplyEvidence 核对真实双方路由、设备采集命令与候选人来信，在线资料不能伪装附件证明。
func verifyOnlineReplyEvidence(ctx context.Context, tx *sql.Tx, in onlineProfileStoreInput) (onlineProfileEvidence, error) {
	scope, request := in.Scope, in.Request
	source := purgeSourceHash(request.PlatformID, request.PlatformCandidateID)
	evidence, err := onlineDeletionEvidence(ctx, tx, scope, source)
	if err != nil {
		return evidence, err
	}
	if err = readOnlineEntity(ctx, tx, scope.TenantID, in.UserID, "conversations", request.ConversationID, &evidence.Conversation); err != nil {
		return evidence, err
	}
	c := evidence.Conversation
	if c.CandidateID == "" || c.IdentityID == "" || c.ThreadID == "" || c.PositionID != request.PositionID || c.PlatformID != request.PlatformID || canonicalReplyAccountKey(c.PlatformID, c.AccountID) != canonicalReplyAccountKey(request.PlatformID, request.PlatformAccountID) || candidateDuplicateText(c.CandidateName) != candidateDuplicateText(request.Profile.CandidateName) {
		return evidence, ErrAutoReplyForbidden
	}
	var identity struct {
		CandidateID         string `json:"candidate_id"`
		PlatformID          string `json:"platform_id"`
		AccountID           string `json:"platform_account_id"`
		PlatformCandidateID string `json:"platform_candidate_id"`
	}
	if err = readOnlineEntity(ctx, tx, scope.TenantID, in.UserID, "identities", c.IdentityID, &identity); err != nil {
		return evidence, err
	}
	if identity.CandidateID != c.CandidateID || identity.PlatformID != request.PlatformID || identity.PlatformCandidateID != request.PlatformCandidateID || canonicalReplyAccountKey(identity.PlatformID, identity.AccountID) != canonicalReplyAccountKey(request.PlatformID, request.PlatformAccountID) {
		return evidence, ErrAutoReplyForbidden
	}
	var candidate map[string]json.RawMessage
	if err = readOnlineEntity(ctx, tx, scope.TenantID, in.UserID, "candidates", c.CandidateID, &candidate); err != nil {
		return evidence, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return evidence, err
	}
	if evidence.DeletionSequence > 0 {
		if c.DeletionBoundary == nil || c.DeletionBoundary.SourceHash != source || c.DeletionBoundary.Sequence < evidence.DeletionSequence || c.DeletionBoundary.DeletedAt.Before(evidence.DeletedAt) || c.ReopenInboundHash == "" || !c.ReopenInboundAt.After(c.DeletionBoundary.DeletedAt) {
			return evidence, errCandidateDeleted
		}
		evidence.DeletedAt = c.DeletionBoundary.DeletedAt
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='messages' AND payload->>'conversation_id'=$3 AND payload->>'direction'='candidate' ORDER BY message_order DESC,sequence DESC LIMIT 5000`, scope.TenantID, in.UserID, request.ConversationID)
	if err != nil {
		return evidence, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return evidence, err
		}
		var message struct {
			At          *time.Time `json:"platform_sent_at"`
			ID          string     `json:"platform_message_id"`
			Fingerprint string     `json:"fingerprint"`
		}
		if json.Unmarshal(raw, &message) != nil || message.At == nil || message.At.After(now) || (!evidence.DeletedAt.IsZero() && !message.At.After(evidence.DeletedAt)) {
			continue
		}
		key := firstNonEmpty(message.ID, message.Fingerprint)
		if key == "" {
			continue
		}
		hash := purgeDigest(key)
		if evidence.DeletionSequence > 0 && (hash != c.ReopenInboundHash || !message.At.Equal(c.ReopenInboundAt)) {
			continue
		}
		evidence.InboundAt, evidence.InboundHash = *message.At, hash
		break
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return evidence, err
	}
	if evidence.InboundAt.IsZero() {
		return evidence, errCandidateDeleted
	}
	commands, err := tx.QueryContext(ctx, `SELECT id,payload FROM auto_reply_cloud_entities WHERE tenant_id=$1 AND user_id=$2 AND kind='runtime_commands' AND payload->>'conversation_id'=$3 AND payload->>'action'='read_online_profile' AND payload->>'status'='completed' AND payload->>'source_key'=$4 ORDER BY sequence DESC LIMIT 100`, scope.TenantID, in.UserID, request.ConversationID, request.Profile.SourceKey)
	if err != nil {
		return evidence, err
	}
	defer commands.Close()
	for commands.Next() {
		var id string
		var raw []byte
		if err = commands.Scan(&id, &raw); err != nil {
			return evidence, err
		}
		var command struct {
			PositionID string    `json:"position_id"`
			MachineID  string    `json:"machine_id"`
			ThreadID   string    `json:"platform_thread_id"`
			AccountID  string    `json:"platform_account_id"`
			At         time.Time `json:"updated_at"`
			ResultCode string    `json:"result_code"`
		}
		if json.Unmarshal(raw, &command) != nil || command.PositionID != request.PositionID || command.MachineID == "" || (command.MachineID != in.MachineID && !in.cachedProof) || command.ThreadID != c.ThreadID || canonicalReplyAccountKey(request.PlatformID, command.AccountID) != canonicalReplyAccountKey(request.PlatformID, request.PlatformAccountID) || command.ResultCode != "" || command.At.IsZero() || command.At.After(now) || command.At.Before(evidence.InboundAt) || (!evidence.DeletedAt.IsZero() && !command.At.After(evidence.DeletedAt)) {
			continue
		}
		if command.At.After(evidence.CapturedAt) {
			evidence.CommandID, evidence.CapturedAt = id, command.At
		}
	}
	if err = commands.Err(); err != nil {
		return evidence, err
	}
	if evidence.CommandID == "" {
		return evidence, ErrAutoReplyForbidden
	}
	return evidence, nil
}

// existingOnlineCacheProof 只复用当前成员同会话已保存的精确资料缓存，首次请求和自报指纹均不能绕过本机采集证明。
func existingOnlineCacheProof(ctx context.Context, tx *sql.Tx, in onlineProfileStoreInput) (bool, string, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT lc.payload->'online_profile' FROM auto_reply_cloud_entities c JOIN auto_reply_cloud_entities lc ON lc.tenant_id=c.tenant_id AND lc.user_id=c.user_id AND lc.kind='candidates' AND lc.id=c.payload->>'candidate_id' WHERE c.tenant_id=$1 AND c.user_id=$2 AND c.kind='conversations' AND c.id=$3`, in.Scope.TenantID, in.UserID, in.Request.ConversationID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	var cached struct {
		CandidateID string                 `json:"candidate_id"`
		ProfileKey  string                 `json:"profile_key"`
		Profile     OnlineCandidateProfile `json:"profile"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &cached) != nil {
		return false, "", nil
	}
	key := onlineProfileFingerprint(in.Request.Profile)
	return validPurgeHash(cached.ProfileKey) && cached.ProfileKey == key && onlineProfileFingerprint(cached.Profile) == key && cached.Profile.SourceKey == in.Request.Profile.SourceKey, cached.CandidateID, nil
}
