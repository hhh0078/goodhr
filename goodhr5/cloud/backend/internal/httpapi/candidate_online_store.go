// 本文件在既有简历事务内保存在线快照；匹配只用完整且唯一的证据，自动采集仅补空字段。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// onlineProfileSourceSnapshot 独立记录每个平台的可见资料与本轮服务端证明，不覆盖其它来源。
type onlineProfileSourceSnapshot struct {
	onlineProfileReceipt
	ActorHash        string `json:"actor_hash"`
	ConversationID   string `json:"conversation_id"`
	CommandID        string `json:"command_id"`
	DeletionSequence int64  `json:"deletion_sequence,omitempty"`
}

// saveOnlineProfile 校验真实会话后先查原生来源，再按完整资料唯一匹配，并原子保存来源快照。
func (s *PostgresCandidateStore) saveOnlineProfile(ctx context.Context, in onlineProfileStoreInput) (autoReplyOnlineProfileResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var result autoReplyOnlineProfileResult
	p := in.Request.Profile
	if p.Age != nil && (*p.Age < 1 || *p.Age > 120) {
		return result, newAutoReplyValidationError("在线资料年龄还没核实，请保留未知值后再试")
	}
	if len(p.CandidateName) > 200 || len(p.RawText) > 1<<20 || len(p.SourceKey) > 256 || len(p.WorkExperiences) > 100 || len(p.Educations) > 100 {
		return result, newAutoReplyValidationError("在线资料内容过长，请减少后再试")
	}
	profileKey := onlineProfileFingerprint(p)
	result.ProfileKey = profileKey
	result.Candidate = autoReplyOnlineCandidate{CandidateName: p.CandidateName, EducationLevel: p.EducationLevel, RawText: p.RawText, WorkExperiences: p.WorkExperiences, Educations: p.Educations}
	result.MatchStatus = "temporary"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = lockCandidateDeletion(ctx, tx, in.Scope.TenantID, false); err != nil {
		return result, err
	}
	in.cachedProof, in.cachedCandidateID, err = existingOnlineCacheProof(ctx, tx, in)
	if err != nil {
		return result, err
	}
	evidence, err := verifyOnlineReplyEvidence(ctx, tx, in)
	if err != nil {
		return result, err
	}
	if !in.Structured {
		return result, tx.Commit()
	}
	for _, lock := range []string{"online-match:" + onlineProfileMatchKey(p), "source:" + in.Request.PlatformID + ":" + in.Request.PlatformCandidateID} {
		if strings.HasSuffix(lock, ":") {
			continue
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, in.Scope.TenantID+":"+lock); err != nil {
			return result, err
		}
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT cp.id::text FROM candidate_profiles cp WHERE cp.tenant_id=$1 AND ((cp.source_platform_id=$2 AND cp.source_platform_candidate_id=$3) OR EXISTS(SELECT 1 FROM candidate_profile_sources cs WHERE cs.tenant_id=cp.tenant_id AND cs.candidate_id=cp.id AND cs.platform_id=$2 AND cs.platform_candidate_id=$3)) LIMIT 1`, in.Scope.TenantID, in.Request.PlatformID, in.Request.PlatformCandidateID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	if in.cachedProof && in.cachedCandidateID != "" && id != in.cachedCandidateID {
		return result, errCandidateDeleted
	}
	var old PositionCandidate
	if id != "" {
		old, err = candidateReadTx(ctx, tx, in.Scope, id, true)
		if errors.Is(err, ErrNotFound) {
			return result, ErrAutoReplyForbidden
		}
		if err != nil {
			return result, err
		}
		result.MatchStatus = "matched"
	} else {
		ids, err := strictOnlineCandidates(ctx, tx, in.Scope, p)
		if err != nil {
			return result, err
		}
		if len(ids) == 1 {
			id = ids[0]
			old, err = candidateReadTx(ctx, tx, in.Scope, id, true)
			if err != nil {
				return result, err
			}
			result.MatchStatus = "matched"
		} else if len(ids) > 1 {
			result.MatchStatus = "ambiguous"
		}
	}
	if evidence.OldCandidateID != "" && id == evidence.OldCandidateID {
		return result, errReopenTargetConflict
	}
	var previous onlineProfileSourceSnapshot
	if id != "" {
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT online_profile FROM candidate_profile_sources WHERE tenant_id=$1 AND candidate_id=$2 AND platform_id=$3 AND platform_candidate_id=$4`, in.Scope.TenantID, id, in.Request.PlatformID, in.Request.PlatformCandidateID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if len(raw) > 0 && json.Unmarshal(raw, &previous) != nil {
			return result, newAutoReplyValidationError("历史在线资料格式尚未核实")
		}
	}
	if id == "" {
		if err = tx.QueryRowContext(ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
			return result, err
		}
	}
	fields := onlineProfileCandidateFields(p)
	fields["platform_id"], fields["platform_candidate_id"], fields["source_type"] = in.Request.PlatformID, in.Request.PlatformCandidateID, "automatic"
	if previous.ProfileKey != profileKey {
		if old.ID != "" {
			fields = onlyEmptyOnlineCandidateFields(old, fields)
		}
		if old.ID == "" || len(fields) > 3 {
			if err = candidateWriteTx(ctx, tx, in.Scope, in.UserID, id, old, fields, false); err != nil {
				return result, err
			}
		} else {
			// 主体字段受保护仍保存来源的新资料时间；完全相同的指纹不会进入此分支。
			if _, err = tx.ExecContext(ctx, `UPDATE candidate_profiles SET updated_at=now(),row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, in.Scope.TenantID, id); err != nil {
				return result, err
			}
		}
	}
	if previous.ProfileKey != profileKey {
		if err = candidateSaveSourceTx(ctx, tx, in.Scope.TenantID, id, fields); err != nil {
			return result, err
		}
		snapshot := onlineProfileSourceSnapshot{onlineProfileReceipt: onlineProfileReceipt{Profile: p, ProfileKey: profileKey, MatchStatus: result.MatchStatus, CapturedAt: evidence.CapturedAt}, ActorHash: purgeDigest(strings.ToLower(in.Scope.UserEmail), canonicalReplyAccountKey(in.Request.PlatformID, in.Request.PlatformAccountID)), ConversationID: in.Request.ConversationID, CommandID: evidence.CommandID, DeletionSequence: evidence.DeletionSequence}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE candidate_profile_sources SET online_profile=$5::jsonb,updated_at=now() WHERE tenant_id=$1 AND candidate_id=$2 AND platform_id=$3 AND platform_candidate_id=$4`, in.Scope.TenantID, id, in.Request.PlatformID, in.Request.PlatformCandidateID, string(raw)); err != nil {
			return result, err
		}
	}
	if err = linkAutoReplyCandidatePosition(ctx, tx, in.Scope.TenantID, id, in.Request.PositionID, in.Request.PlatformID); err != nil {
		return result, err
	}
	if !evidence.InboundAt.IsZero() {
		// 首次聊天已先于主档同步，复用已有沟通投影补齐真实来信时间；这里只更新投影，不新增消息或按保存时刻续期。
		message, marshalErr := json.Marshal(map[string]any{"conversation_id": in.Request.ConversationID, "direction": "candidate", "platform_sent_at": evidence.InboundAt})
		if marshalErr != nil {
			return result, marshalErr
		}
		if err = touchCandidateCommunicationsTx(ctx, tx, in.Scope.TenantID, in.UserID, []AutoReplyStateChange{{Kind: "messages", Payload: message}}); err != nil {
			return result, err
		}
	}
	if evidence.DeletionSequence > 0 {
		if err = saveOnlineReopenReceipt(ctx, tx, in, id, profileKey, evidence); err != nil {
			return result, err
		}
	}
	saved, err := candidateReadTx(ctx, tx, in.Scope, id, false)
	if err != nil {
		return result, err
	}
	result.CandidateID, result.Candidate = id, onlineCandidateDTO(saved)
	if result.MatchStatus == "matched" && normalizeCandidatePhone(saved.Phone) == "" {
		result.MatchStatus = "temporary"
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// onlineProfileCandidateFields 仅把已知在线字段送入既有白名单，不设置附件或简历解析完成标记。
func onlineProfileCandidateFields(p OnlineCandidateProfile) map[string]any {
	fields := map[string]any{"candidate_name": p.CandidateName, "education_level": p.EducationLevel, "gender": p.Gender, "basic_info": p.RawText, "raw_text": p.RawText}
	if p.Age != nil {
		fields["age"] = fmt.Sprint(*p.Age)
	}
	if len(p.WorkExperiences) > 0 {
		fields["work_experiences"] = p.WorkExperiences
	}
	if len(p.Educations) > 0 {
		fields["educations"] = p.Educations
	}
	return fields
}

// onlyEmptyOnlineCandidateFields 保护人工以及已有正式非空内容，在线快照变化也不能覆盖它们。
func onlyEmptyOnlineCandidateFields(old PositionCandidate, fields map[string]any) map[string]any {
	existing := candidateProfileFields(old)
	for key, value := range old.LibraryFields {
		existing[key] = value
	}
	manual := make(map[string]bool, len(old.ManualFields))
	for _, key := range old.ManualFields {
		manual[key] = true
	}
	filtered := map[string]any{}
	for key, value := range fields {
		if key == "platform_id" || key == "platform_candidate_id" || key == "source_type" {
			filtered[key] = value
			continue
		}
		if !manual[key] && candidateEmptyValue(existing[key]) && !candidateEmptyValue(value) {
			filtered[key] = value
		}
	}
	return filtered
}

// strictOnlineCandidates 只检查当前成员可见的同姓候选集，并同时比较完整字段与正负3岁。
func strictOnlineCandidates(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, p OnlineCandidateProfile) ([]string, error) {
	if onlineProfileMatchKey(p) == "" {
		return nil, nil
	}
	args := []any{scope.TenantID, onlineProfileSurname(p.CandidateName) + "%"}
	where := `cp.tenant_id=$1 AND btrim(cp.candidate_name) LIKE $2`
	if !scope.IsAdmin {
		args = append(args, scope.UserEmail)
		where += ` AND u.email=$3`
	}
	rows, err := tx.QueryContext(ctx, `SELECT cp.id::text,cp.candidate_name,cp.birth_ym,cp.education_level,cp.work_experiences,cp.educations,cp.library_fields,COALESCE((SELECT jsonb_agg(cs.online_profile->'profile') FROM candidate_profile_sources cs WHERE cs.tenant_id=cp.tenant_id AND cs.candidate_id=cp.id AND cs.online_profile ? 'profile'),'[]'::jsonb) FROM candidate_profiles cp LEFT JOIN users u ON u.id=cp.created_by_user_id WHERE `+where+` ORDER BY cp.id LIMIT 5001`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	count := 0
	for rows.Next() {
		count++
		if count > 5000 {
			return nil, newAutoReplyValidationError("同姓资料过多，先保留独立临时档案，不自动关联")
		}
		var item PositionCandidate
		var snapshots []OnlineCandidateProfile
		if err = rows.Scan(&item.ID, &item.CandidateName, &item.BirthYM, &item.EducationLevel, jsonScanner(&item.WorkExperiences), jsonScanner(&item.Educations), jsonScanner(&item.LibraryFields), jsonScanner(&snapshots)); err != nil {
			return nil, err
		}
		current := onlineProfileFromCandidate(item)
		// 已有经历但起点不可靠时不能再用来源别名补成可关联，空经历数组才表示尚无此类主档证据。
		if len(item.WorkExperiences) > 0 && (current.EarliestCompanyName == "" || current.EarliestCompanyYear == nil) || len(item.Educations) > 0 && (current.EarliestSchoolName == "" || current.EarliestSchoolYear == nil) {
			continue
		}
		if !onlineProfileKnownFieldsAgree(p, current) {
			continue
		}
		matched := onlineProfilesMatch(p, current)
		// 主体身份齐全时只按主体判断，来源别名不能放宽主体的年龄或经历基准。
		if onlineProfileMatchKey(current) == "" {
			for _, snapshot := range snapshots {
				if onlineProfileMatchKey(snapshot) == "" {
					continue
				}
				if !onlineProfilesMatch(p, snapshot) {
					matched = false
					break
				}
				matched = true
			}
		}
		if matched {
			ids = append(ids, item.ID)
			if len(ids) > 1 {
				return ids, nil
			}
		}
	}
	return ids, rows.Err()
}

// saveOnlineReopenReceipt 登记本代新来信和真实在线读取证明，附件历史收据仍按原类型保留。
func saveOnlineReopenReceipt(ctx context.Context, tx *sql.Tx, in onlineProfileStoreInput, id, key string, e onlineProfileEvidence) error {
	source := purgeSourceHash(in.Request.PlatformID, in.Request.PlatformCandidateID)
	actor := purgeDigest(strings.ToLower(in.Scope.UserEmail), canonicalReplyAccountKey(in.Request.PlatformID, in.Request.PlatformAccountID))
	proof := purgeDigest("online_profile", e.InboundHash, e.InboundAt.UTC().Format(time.RFC3339Nano), e.CommandID, key)
	result, err := tx.ExecContext(ctx, `INSERT INTO candidate_reopen_receipts(deletion_sequence,source_hash,actor_hash,proof_hash,attachment_at,candidate_id,proof_type,profile_key) VALUES($1,$2,$3,$4,$5,$6,'online_profile',$7) ON CONFLICT(deletion_sequence,source_hash) DO UPDATE SET profile_key=CASE WHEN candidate_reopen_receipts.proof_type='online_profile' THEN excluded.profile_key ELSE candidate_reopen_receipts.profile_key END WHERE candidate_reopen_receipts.candidate_id=excluded.candidate_id`, e.DeletionSequence, source, actor, proof, e.CapturedAt, id, key)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errCandidateDeleted
	}
	result, err = tx.ExecContext(ctx, `UPDATE candidate_deletion_keys SET replacement_candidate_id=$3 WHERE tenant_id=$1 AND kind='source' AND identity_hash=$2 AND (replacement_candidate_id IS NULL OR replacement_candidate_id=$3)`, in.Scope.TenantID, source, id)
	if err == nil {
		if count, _ := result.RowsAffected(); count != 1 {
			return errCandidateDeleted
		}
	}
	return err
}

// onlineProfileKnownFieldsAgree 不以旧来源快照掩盖当前人工资料的已知冲突。
func onlineProfileKnownFieldsAgree(in, current OnlineCandidateProfile) bool {
	if current.EducationLevel != "" && onlineProfileEducation(in.EducationLevel) != onlineProfileEducation(current.EducationLevel) {
		return false
	}
	if current.Age != nil && (in.Age == nil || *in.Age < *current.Age-3 || *in.Age > *current.Age+3) {
		return false
	}
	for _, pair := range [][2]string{{in.EarliestCompanyName, current.EarliestCompanyName}, {in.EarliestSchoolName, current.EarliestSchoolName}} {
		if pair[1] != "" && candidateDuplicateText(pair[0]) != candidateDuplicateText(pair[1]) {
			return false
		}
	}
	for _, pair := range [][2]*int{{in.EarliestCompanyYear, current.EarliestCompanyYear}, {in.EarliestSchoolYear, current.EarliestSchoolYear}} {
		if pair[1] != nil && (pair[0] == nil || *pair[0] != *pair[1]) {
			return false
		}
	}
	return true
}
