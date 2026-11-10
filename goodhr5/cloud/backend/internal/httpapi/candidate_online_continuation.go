// 本文件仅允许服务器以同一成员当前代在线资料证明补齐手机等空字段，旧异步上传仍受删除封印限制。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// hasOnlineCandidateFill 判断除来源路由外是否确有新空字段可补，重复回包不刷新资料时间。
func hasOnlineCandidateFill(fields map[string]any) bool {
	for key, value := range fields {
		if key != "platform_id" && key != "platform_candidate_id" && key != "source_type" && !candidateEmptyValue(value) {
			return true
		}
	}
	return false
}

// verifyOnlineCandidateContinuation 校验简历库编号、指纹、当前会话缓存及真实读取命令，不接受仅名字相同的续写。
func verifyOnlineCandidateContinuation(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, userID, currentID string, input CandidateProfileInput) error {
	if strings.TrimSpace(input.Phone) != "" && normalizeCandidatePhone(input.Phone) == "" {
		return newAutoReplyValidationError("手机号还没完整核实，暂不转为正式资料")
	}
	if !input.ReopenServerWitness || input.OnlineProfileCandidateID == "" || input.OnlineProfileCandidateID != currentID || !validPurgeHash(input.OnlineProfileKey) || input.OnlineProfileMachineID == "" || input.AutoReplyPositionID == "" || input.ReopenPlatformAccount == "" {
		return errCandidateDeleted
	}
	scope.IsAdmin = input.ReopenIsAdmin
	if _, err := candidateReadTx(ctx, tx, scope, currentID, false); err != nil {
		return errCandidateDeleted
	}
	var sourceRaw []byte
	err := tx.QueryRowContext(ctx, `SELECT online_profile FROM candidate_profile_sources WHERE tenant_id=$1 AND candidate_id=$2 AND platform_id=$3 AND platform_candidate_id=$4`, scope.TenantID, currentID, input.PlatformID, input.PlatformCandidateID).Scan(&sourceRaw)
	if err != nil {
		return errCandidateDeleted
	}
	var snapshot onlineProfileSourceSnapshot
	if json.Unmarshal(sourceRaw, &snapshot) != nil || snapshot.ProfileKey != input.OnlineProfileKey || onlineProfileFingerprint(snapshot.Profile) != input.OnlineProfileKey {
		return errCandidateDeleted
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,lc.payload->'online_profile' FROM auto_reply_cloud_entities c JOIN auto_reply_cloud_entities lc ON lc.tenant_id=c.tenant_id AND lc.user_id=c.user_id AND lc.kind='candidates' AND lc.id=c.payload->>'candidate_id' WHERE c.tenant_id=$1 AND c.user_id=$2 AND c.kind='conversations' AND c.payload->>'position_id'=$3 AND lc.payload#>>'{online_profile,candidate_id}'=$4 AND lc.payload#>>'{online_profile,profile_key}'=$5 LIMIT 2`, scope.TenantID, userID, input.AutoReplyPositionID, currentID, input.OnlineProfileKey)
	if err != nil {
		return err
	}
	var conversation string
	var cached struct {
		Profile OnlineCandidateProfile `json:"profile"`
	}
	count := 0
	for rows.Next() {
		count++
		var raw []byte
		if err = rows.Scan(&conversation, &raw); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(raw, &cached) != nil {
			rows.Close()
			return errCandidateDeleted
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if count != 1 || cached.Profile.SourceKey == "" || onlineProfileFingerprint(cached.Profile) != input.OnlineProfileKey {
		return errCandidateDeleted
	}
	witness, err := verifyOnlineReplyEvidence(ctx, tx, onlineProfileStoreInput{Scope: scope, UserID: userID, MachineID: input.OnlineProfileMachineID, Structured: true, cachedProof: true, Request: autoReplyOnlineProfileRequest{PositionID: input.AutoReplyPositionID, ConversationID: conversation, PlatformID: input.PlatformID, PlatformAccountID: input.ReopenPlatformAccount, PlatformCandidateID: input.PlatformCandidateID, Profile: cached.Profile}})
	if err != nil {
		return err
	}
	if witness.DeletionSequence != snapshot.DeletionSequence {
		return errCandidateDeleted
	}
	if witness.DeletionSequence > 0 {
		var replacement, kind, key, actor string
		err = tx.QueryRowContext(ctx, `SELECT candidate_id::text,proof_type,profile_key,actor_hash FROM candidate_reopen_receipts WHERE deletion_sequence=$1 AND source_hash=$2`, witness.DeletionSequence, purgeSourceHash(input.PlatformID, input.PlatformCandidateID)).Scan(&replacement, &kind, &key, &actor)
		if errors.Is(err, sql.ErrNoRows) {
			return errCandidateDeleted
		}
		if err != nil {
			return err
		}
		if replacement != currentID {
			return errCandidateDeleted
		}
		if kind == "online_profile" && (key != input.OnlineProfileKey || actor != purgeDigest(strings.ToLower(scope.UserEmail), canonicalReplyAccountKey(input.PlatformID, input.ReopenPlatformAccount))) {
			return errCandidateDeleted
		}
	}
	return nil
}
