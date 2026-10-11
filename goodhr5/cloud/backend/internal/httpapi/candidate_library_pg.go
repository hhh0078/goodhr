// 本文件实现 PostgreSQL 简历人工入库、安全更新、查重和独立岗位推荐，所有写入使用事务。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SaveLibraryCandidate 持锁查重并写入人工简历，幂等凭据与结果在同一事务提交。
func (s *PostgresCandidateStore) SaveLibraryCandidate(scope CandidateLibraryScope, id string, fields map[string]any, version int64, key string) (PositionCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PositionCandidate{}, err
	}
	defer tx.Rollback()
	operation := "candidate:" + id
	fingerprint := candidateFingerprint(fields)
	locks := []string{"request:" + scope.UserEmail + ":" + operation + ":" + key}
	if phone := candidateString(fields, "phone"); phone != "" {
		locks = append(locks, "phone:"+phone)
	}
	if platformID := candidateString(fields, "platform_candidate_id"); platformID != "" {
		locks = append(locks, "source:"+candidateString(fields, "platform_id")+":"+platformID)
	}
	sort.Strings(locks)
	for _, lock := range locks {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope.TenantID+":"+lock); err != nil {
			return PositionCandidate{}, err
		}
	}
	receipt, found, err := candidateRequestLookup(ctx, tx, scope, operation, key)
	if err != nil {
		return PositionCandidate{}, err
	}
	if found {
		if receipt.Fingerprint != fingerprint {
			return PositionCandidate{}, ErrCandidateRequest
		}
		item, err := candidateReadTx(ctx, tx, scope, receipt.ResultID, false)
		return item, err
	}
	var old PositionCandidate
	if id != "" {
		old, err = candidateReadTx(ctx, tx, scope, id, true)
		if err != nil {
			return PositionCandidate{}, err
		}
		if old.RowVersion != version {
			return PositionCandidate{}, ErrCandidateVersion
		}
	}
	phone := candidateString(fields, "phone")
	platform := candidateString(fields, "platform_id")
	sourceID := candidateString(fields, "platform_candidate_id")
	if (phone != "" && (id == "" || phone != normalizeCandidatePhone(old.Phone))) || (platform != "" && sourceID != "") {
		var duplicate bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM candidate_profiles cp WHERE cp.tenant_id=$1 AND cp.id::text<>$2 AND (($3<>'' AND (cp.normalized_phone=$3 OR cp.phone=$3)) OR ($4<>'' AND $5<>'' AND ((cp.source_platform_id=$4 AND cp.source_platform_candidate_id=$5) OR EXISTS(SELECT 1 FROM candidate_profile_sources cs WHERE cs.candidate_id=cp.id AND cs.platform_id=$4 AND cs.platform_candidate_id=$5)))))`, scope.TenantID, id, phone, platform, sourceID).Scan(&duplicate)
		if err != nil {
			return PositionCandidate{}, err
		}
		if duplicate {
			return PositionCandidate{}, ErrCandidateDuplicate
		}
	}
	userID, err := ensureUserID(ctx, s.db, scope.UserEmail)
	if err != nil {
		return PositionCandidate{}, err
	}
	actualTenant, err := userTenantID(ctx, s.db, userID)
	if err != nil || actualTenant != scope.TenantID {
		return PositionCandidate{}, ErrNotFound
	}
	if id == "" {
		if err = tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			return PositionCandidate{}, err
		}
	}
	if err = candidateWriteTx(ctx, tx, scope, userID, id, old, fields, true); err != nil {
		return PositionCandidate{}, err
	}
	if phone != "" && (old.ID == "" || normalizeCandidatePhone(old.Phone) != phone) {
		var registered string
		err = tx.QueryRowContext(ctx, `INSERT INTO candidate_phone_identities(tenant_id,normalized_phone,candidate_id) VALUES($1,$2,$3) ON CONFLICT(tenant_id,normalized_phone) DO UPDATE SET candidate_id=EXCLUDED.candidate_id WHERE candidate_phone_identities.candidate_id=EXCLUDED.candidate_id RETURNING candidate_id::text`, scope.TenantID, phone, id).Scan(&registered)
		if errors.Is(err, sql.ErrNoRows) {
			return PositionCandidate{}, ErrCandidateDuplicate
		}
		if err != nil {
			return PositionCandidate{}, err
		}
		if old.Phone != "" {
			_, err = tx.ExecContext(ctx, `DELETE FROM candidate_phone_identities WHERE tenant_id=$1 AND candidate_id=$2 AND normalized_phone<>$3`, scope.TenantID, id, phone)
			if err != nil {
				return PositionCandidate{}, err
			}
		}
	}
	if err = candidateSaveSourceTx(ctx, tx, scope.TenantID, id, fields); err != nil {
		return PositionCandidate{}, err
	}
	if err = candidateRequestSave(ctx, tx, scope, operation, key, candidateRequestReceipt{fingerprint, id}); err != nil {
		return PositionCandidate{}, err
	}
	item, err := candidateReadTx(ctx, tx, scope, id, false)
	if err != nil {
		return PositionCandidate{}, err
	}
	if err = tx.Commit(); err != nil {
		return PositionCandidate{}, err
	}
	return item, nil
}

// candidateReadTx 在事务内检查团队和创建人权限后读取主体，可选行锁防止并发覆盖。
func candidateReadTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string, lock bool) (PositionCandidate, error) {
	args := []any{scope.TenantID, id}
	where := "WHERE cp.tenant_id=$1 AND cp.id::text=$2"
	if !scope.IsAdmin {
		args = append(args, scope.UserEmail)
		where += " AND u.email=$3"
	}
	query := candidateSelectSQL(where, "")
	if lock {
		query += " FOR UPDATE OF cp"
	}
	item, err := scanCandidateRow(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return PositionCandidate{}, ErrNotFound
	}
	return item, err
}

// candidateWriteTx 只处理白名单字段，保护自动采集不覆盖人工维护和有效历史值。
func candidateWriteTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, userID, id string, old PositionCandidate, fields map[string]any, manual bool) error {
	library := map[string]any{}
	for key, value := range old.LibraryFields {
		library[key] = value
	}
	protected := map[string]bool{}
	for _, key := range old.ManualFields {
		protected[key] = true
	}
	standard := []string{}
	values := []any{}
	for _, key := range append(append(append([]string{}, candidateTextColumns...), candidateArrayColumns...), "expected_salary_min", "expected_salary_max") {
		value, ok := fields[key]
		if !ok {
			continue
		}
		if !manual && (protected[key] || candidateEmptyValue(value)) {
			continue
		}
		standard = append(standard, key)
		values = append(values, value)
		if manual {
			protected[key] = true
		}
	}
	for _, key := range candidateExtraFields {
		if value, ok := fields[key]; ok && (manual || (!protected[key] && !candidateEmptyValue(value))) {
			library[key] = value
			if manual {
				protected[key] = true
			}
		}
	}
	protectedNames := []string{}
	for key := range protected {
		protectedNames = append(protectedNames, key)
	}
	sort.Strings(protectedNames)
	if old.ID == "" {
		columns := []string{"id", "tenant_id", "created_by_user_id", "source_platform_id", "source_platform_candidate_id"}
		args := []any{id, scope.TenantID, userID, firstNonEmpty(candidateString(fields, "platform_id"), "manual"), firstNonEmpty(candidateString(fields, "platform_candidate_id"), id)}
		for i, key := range standard {
			columns = append(columns, key)
			args = append(args, candidateSQLValue(key, values[i]))
		}
		columns = append(columns, "library_fields", "manual_fields", "normalized_phone")
		args = append(args, string(toJSONB(library)), string(toJSONB(protectedNames)), normalizeCandidatePhone(candidateString(fields, "phone")))
		placeholders := []string{}
		for i := range args {
			placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO candidate_profiles ("+strings.Join(columns, ",")+") VALUES ("+strings.Join(placeholders, ",")+")", args...)
		return err
	}
	args := []any{scope.TenantID, id}
	sets := []string{}
	for i, key := range standard {
		args = append(args, candidateSQLValue(key, values[i]))
		sets = append(sets, fmt.Sprintf("%s=$%d", key, len(args)))
		if key == "phone" {
			args = append(args, normalizeCandidatePhone(candidateString(fields, "phone")))
			sets = append(sets, fmt.Sprintf("normalized_phone=$%d", len(args)))
		}
	}
	args = append(args, string(toJSONB(library)))
	sets = append(sets, fmt.Sprintf("library_fields=$%d::jsonb", len(args)))
	args = append(args, string(toJSONB(protectedNames)))
	sets = append(sets, fmt.Sprintf("manual_fields=$%d::jsonb", len(args)))
	sets = append(sets, "row_version=row_version+1", "updated_at=now()")
	_, err := tx.ExecContext(ctx, "UPDATE candidate_profiles SET "+strings.Join(sets, ",")+" WHERE tenant_id=$1 AND id=$2", args...)
	return err
}

// candidateSQLValue 把经历数组编码为数据库可接收的 JSON，不改变标量类型。
func candidateSQLValue(key string, value any) any {
	for _, array := range candidateArrayColumns {
		if key == array {
			return string(toJSONB(value))
		}
	}
	return value
}

// candidateSaveSourceTx 新增平台身份映射，同一个身份不能被静默搬到另一份简历。
func candidateSaveSourceTx(ctx context.Context, tx *sql.Tx, tenantID, id string, fields map[string]any) error {
	platform := candidateString(fields, "platform_id")
	sourceID := candidateString(fields, "platform_candidate_id")
	sourceType := candidateString(fields, "source_type")
	url := candidateString(fields, "source_url")
	if platform == "" && sourceType == "" {
		return nil
	}
	if platform == "" || sourceID == "" {
		_, err := tx.ExecContext(ctx, `INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type,source_url) SELECT $1,$2,$3,$4,$5,$6 WHERE NOT EXISTS(SELECT 1 FROM candidate_profile_sources WHERE tenant_id=$1 AND candidate_id=$2 AND platform_id=$3 AND platform_candidate_id=$4 AND source_type=$5)`, tenantID, id, platform, sourceID, sourceType, url)
		return err
	}
	var saved string
	err := tx.QueryRowContext(ctx, `INSERT INTO candidate_profile_sources(tenant_id,candidate_id,platform_id,platform_candidate_id,source_type,source_url) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,platform_id,platform_candidate_id) WHERE platform_id<>'' AND platform_candidate_id<>'' DO UPDATE SET source_url=COALESCE(NULLIF(EXCLUDED.source_url,''),candidate_profile_sources.source_url),updated_at=now() WHERE candidate_profile_sources.candidate_id=EXCLUDED.candidate_id RETURNING candidate_id::text`, tenantID, id, platform, sourceID, sourceType, url).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCandidateDuplicate
	}
	return err
}

// candidateRequestLookup 在当前事务读取已提交的幂等凭据。
func candidateRequestLookup(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, operation, key string) (candidateRequestReceipt, bool, error) {
	if key == "" {
		return candidateRequestReceipt{}, false, nil
	}
	var receipt candidateRequestReceipt
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,result_id FROM candidate_library_requests WHERE tenant_id=$1 AND actor_email=$2 AND operation=$3 AND request_key=$4`, scope.TenantID, scope.UserEmail, operation, key).Scan(&receipt.Fingerprint, &receipt.ResultID)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, false, nil
	}
	return receipt, err == nil, err
}

// candidateRequestSave 把请求指纹和操作结果与业务记录一并提交。
func candidateRequestSave(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, operation, key string, receipt candidateRequestReceipt) error {
	if key == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO candidate_library_requests(tenant_id,actor_email,operation,request_key,fingerprint,result_id) VALUES($1,$2,$3,$4,$5,$6)`, scope.TenantID, scope.UserEmail, operation, key, receipt.Fingerprint, receipt.ResultID)
	return err
}

// FindCandidateDuplicates 内部查询整个团队，外部只返回成员可见记录与匿名重复提示。
func (s *PostgresCandidateStore) FindCandidateDuplicates(scope CandidateLibraryScope, query CandidateDuplicateQuery) ([]CandidateDuplicateMatch, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []any{scope.TenantID}
	conditions := []string{}
	add := func(value string, clause string) {
		if value != "" {
			args = append(args, value)
			conditions = append(conditions, fmt.Sprintf(clause, len(args)))
		}
	}
	phone := normalizeCandidatePhone(query.Phone)
	add(phone, "cp.normalized_phone=$%[1]d OR cp.phone=$%[1]d")
	if query.PlatformID != "" && query.PlatformCandidateID != "" {
		args = append(args, query.PlatformID, query.PlatformCandidateID)
		conditions = append(conditions, fmt.Sprintf("(cp.source_platform_id=$%d AND cp.source_platform_candidate_id=$%d) OR EXISTS(SELECT 1 FROM candidate_profile_sources cs WHERE cs.candidate_id=cp.id AND cs.platform_id=$%d AND cs.platform_candidate_id=$%d)", len(args)-1, len(args), len(args)-1, len(args)))
	}
	if phone == "" {
		if candidateDuplicateStructured(query) {
			conditions = append(conditions, candidateDuplicateSQLClauses(query, &args)...)
		} else {
			add(candidateLike(query.Name), "cp.candidate_name ILIKE $%d")
			add(candidateLike(query.Keyword), "(cp.work_experiences::text||' '||cp.educations::text||' '||cp.raw_text) ILIKE $%d")
		}
	}
	if len(conditions) == 0 {
		return []CandidateDuplicateMatch{}, false, nil
	}
	where := "WHERE cp.tenant_id=$1 AND (" + strings.Join(conditions, " OR ") + ")"
	rows, err := s.db.QueryContext(ctx, candidateSelectSQL(where, "")+" ORDER BY cp.created_at DESC", args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []PositionCandidate{}
	for rows.Next() {
		item, err := scanCandidateRow(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	matches, hidden := selectCandidateDuplicateMatches(scope, query, items)
	return matches, hidden, nil
}

// candidateLike 对通配符转义，搜索内容不被误解释成整库匹配。
func candidateLike(value string) string {
	if value == "" {
		return ""
	}
	return "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value) + "%"
}

// ListCandidateRecommendations 使用已有详情权限读取全部推荐与历史。
func (s *PostgresCandidateStore) ListCandidateRecommendations(scope CandidateLibraryScope, id string) ([]CandidateRecommendation, error) {
	item, err := s.GetPositionCandidate(scope.TenantID, id, "", scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return nil, err
	}
	return safeSlice(item.Recommendations), nil
}

// SaveCandidateRecommendation 创建唯一岗位关系并追加推进事件，不更新自动运行统计。
func (s *PostgresCandidateStore) SaveCandidateRecommendation(scope CandidateLibraryScope, id string, position Position, stage, content, key string, occurredAt ...time.Time) (CandidateRecommendation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	defer tx.Rollback()
	operation := "recommendation:" + id + ":" + position.ID
	fingerprint := candidateRecommendationFingerprint(stage, content, occurredAt)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope.TenantID+":"+operation); err != nil {
		return CandidateRecommendation{}, err
	}
	receipt, found, err := candidateRequestLookup(ctx, tx, scope, operation, key)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	if found && receipt.Fingerprint != fingerprint {
		return CandidateRecommendation{}, ErrCandidateRequest
	}
	candidate, err := candidateReadTx(ctx, tx, scope, id, true)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	if found {
		for _, item := range candidate.Recommendations {
			if item.ID == receipt.ResultID {
				return item, nil
			}
		}
		return CandidateRecommendation{}, ErrNotFound
	}
	var item CandidateRecommendation
	for _, existing := range candidate.Recommendations {
		if existing.PositionID == position.ID {
			item = existing
			break
		}
	}
	existed := item.ID != ""
	previous := item.Stage
	if !existed {
		err = tx.QueryRowContext(ctx, `INSERT INTO candidate_recommendations(tenant_id,candidate_id,position_id,position_key,position_name,stage,author_email,author_name) VALUES($1,$2,$3::uuid,$3::text,$4,$5,$6,$7) RETURNING id::text`, scope.TenantID, id, position.ID, position.Name, stage, scope.UserEmail, scope.AuthorName).Scan(&item.ID)
		if err != nil {
			return CandidateRecommendation{}, err
		}
	}
	if !existed || previous != stage || content != "" || (len(occurredAt) > 0 && !occurredAt[0].IsZero()) {
		_, err = tx.ExecContext(ctx, `INSERT INTO candidate_recommendation_events(recommendation_id,from_stage,to_stage,content,author_email,author_name,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.ID, previous, stage, content, scope.UserEmail, scope.AuthorName, candidateRecommendationTime(occurredAt, time.Now().UTC()))
		if err != nil {
			return CandidateRecommendation{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE candidate_recommendations SET stage=$2,updated_at=now() WHERE id=$1`, item.ID, stage)
		if err != nil {
			return CandidateRecommendation{}, err
		}
	}
	if err = candidateRequestSave(ctx, tx, scope, operation, key, candidateRequestReceipt{fingerprint, item.ID}); err != nil {
		return CandidateRecommendation{}, err
	}
	updated, err := candidateReadTx(ctx, tx, scope, id, false)
	if err != nil {
		return CandidateRecommendation{}, err
	}
	for _, rec := range updated.Recommendations {
		if rec.ID == item.ID {
			item = rec
			break
		}
	}
	if err = tx.Commit(); err != nil {
		return CandidateRecommendation{}, err
	}
	return item, nil
}

// saveAutomaticCandidate 保留旧岗位同步入口，增加来源映射和人工字段保护。
func (s *PostgresCandidateStore) saveAutomaticCandidate(input CandidateProfileInput) (PositionCandidate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	userID, err := ensureUserID(ctx, s.db, input.UserEmail)
	if err != nil {
		return PositionCandidate{}, err
	}
	tenantID, err := userTenantID(ctx, s.db, userID)
	if err != nil {
		return PositionCandidate{}, err
	}
	scope := CandidateLibraryScope{TenantID: tenantID, UserEmail: input.UserEmail, IsAdmin: true}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PositionCandidate{}, err
	}
	defer tx.Rollback()
	sourceID := candidateIdentityKey(input)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenantID+":source:"+input.PlatformID+":"+sourceID); err != nil {
		return PositionCandidate{}, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT cp.id::text FROM candidate_profiles cp WHERE cp.tenant_id=$1 AND ((cp.source_platform_id=$2 AND cp.source_platform_candidate_id=$3) OR EXISTS(SELECT 1 FROM candidate_profile_sources cs WHERE cs.candidate_id=cp.id AND cs.platform_id=$2 AND cs.platform_candidate_id=$3)) LIMIT 1`, tenantID, input.PlatformID, sourceID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PositionCandidate{}, err
	}
	var old PositionCandidate
	if id != "" {
		old, err = candidateReadTx(ctx, tx, scope, id, true)
		if err != nil {
			return PositionCandidate{}, err
		}
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
			return PositionCandidate{}, err
		}
	}
	fields := candidateProfileFields(PositionCandidate{CandidateName: input.CandidateName, BirthYM: input.BirthYM, Phone: input.Phone, Email: input.Email, WorkRegion: input.WorkRegion, WorkYears: input.WorkYears, ExpectedSalaryMin: input.ExpectedSalaryMin, ExpectedSalaryMax: input.ExpectedSalaryMax, BasicInfo: input.BasicInfo, EducationLevel: input.EducationLevel, ExpectedPosition: input.ExpectedPosition, OnlineStatus: input.OnlineStatus, PersonalDescription: input.PersonalDescription, WorkStatus: input.WorkStatus, RawText: input.RawText, WorkExperiences: input.WorkExperiences, Educations: input.Educations, Certificates: input.Certificates, Honors: input.Honors, ProjectExperiences: input.ProjectExperiences, Communications: input.Communications})
	// 自动同步的薪资指针转为 SQL 标量，JSON 数组始终为有效数组。
	for _, key := range []string{"expected_salary_min", "expected_salary_max"} {
		if ptr, ok := fields[key].(*float64); ok {
			if ptr != nil {
				fields[key] = *ptr
			} else {
				delete(fields, key)
			}
		}
	}
	fields["platform_id"] = input.PlatformID
	fields["platform_candidate_id"] = sourceID
	if old.ID == "" {
		fields["source_type"] = "automatic"
	}
	if err = candidateWriteTx(ctx, tx, scope, userID, id, old, fields, false); err != nil {
		return PositionCandidate{}, err
	}
	if err = candidateSaveSourceTx(ctx, tx, tenantID, id, fields); err != nil {
		return PositionCandidate{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE candidate_profiles SET ai_detail_reason=COALESCE(NULLIF($2,''),ai_detail_reason),ai_detail_score=COALESCE($3,ai_detail_score),ai_greet_reason=COALESCE(NULLIF($4,''),ai_greet_reason),ai_greet_score=COALESCE($5,ai_greet_score),first_seen_at=COALESCE(first_seen_at,$6) WHERE id=$1`, id, input.AIDetailReason, input.AIDetailScore, input.AIGreetReason, input.AIGreetScore, input.FirstSeenAt)
	if err != nil {
		return PositionCandidate{}, err
	}
	item, err := candidateReadTx(ctx, tx, scope, id, false)
	if err != nil {
		return PositionCandidate{}, err
	}
	if err = tx.Commit(); err != nil {
		return PositionCandidate{}, err
	}
	return item, nil
}

// candidateRecommendationsSQL 返回包含全部阶段历史的人工推荐 JSON 汇总。
func candidateRecommendationsSQL() string {
	return `COALESCE((SELECT jsonb_agg(jsonb_build_object('id',cr.id::text,'candidate_id',cr.candidate_id::text,'position_id',COALESCE(cr.position_id::text,cr.position_key),'position_name',cr.position_name,'stage',cr.stage,'author_email',cr.author_email,'author_name',cr.author_name,'created_at',cr.created_at,'updated_at',cr.updated_at,'events',COALESCE((SELECT jsonb_agg(to_jsonb(ev) ORDER BY ev.occurred_at DESC,ev.created_at DESC,ev.id DESC) FROM candidate_recommendation_events ev WHERE ev.recommendation_id=cr.id),'[]'::jsonb)) ORDER BY cr.updated_at DESC) FROM candidate_recommendations cr WHERE cr.candidate_id=cp.id),'[]'::jsonb)`
}

// candidateSourcesSQL 返回候选人的多个来源，保持原来源字段向后兼容。
func candidateSourcesSQL() string {
	return `COALESCE((SELECT jsonb_agg(jsonb_build_object('platform_id',cs.platform_id,'platform_candidate_id',cs.platform_candidate_id,'source_type',cs.source_type,'source_url',cs.source_url) ORDER BY cs.created_at DESC) FROM candidate_profile_sources cs WHERE cs.tenant_id=cp.tenant_id AND cs.candidate_id=cp.id),'[]'::jsonb)`
}

// decodeCandidateJSONArray 把数据库结构字段解码为稳定数组。
func decodeCandidateJSONArray(raw []byte, target any) error { return json.Unmarshal(raw, target) }
