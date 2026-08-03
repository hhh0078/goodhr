// 本文件负责提供自动回复 PostgreSQL 存储的公共连接、身份校验和公司岗位配置能力。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrAutoReplyForbidden 表示当前用户不属于目标团队或没有自动回复数据权限。
var ErrAutoReplyForbidden = errors.New("auto reply forbidden")

// PostgresAutoReplyStore 使用 PostgreSQL 持久化自动回复配置、会话和审计数据。
type PostgresAutoReplyStore struct {
	db *sql.DB
}

// NewPostgresAutoReplyStore 创建自动回复 PostgreSQL 存储。
func NewPostgresAutoReplyStore(db *sql.DB) *PostgresAutoReplyStore {
	return &PostgresAutoReplyStore{db: db}
}

// SaveCompanyProfile 新增或更新一份团队共享公司档案。
func (s *PostgresAutoReplyStore) SaveCompanyProfile(ctx context.Context, tenantID, userEmail string, item CompanyProfile) (CompanyProfile, error) {
	if err := validateCompanyProfile(item); err != nil {
		return CompanyProfile{}, err
	}
	userID, err := s.activeTenantUserID(ctx, tenantID, userEmail)
	if err != nil {
		return CompanyProfile{}, err
	}
	var saved CompanyProfile
	if strings.TrimSpace(item.ID) == "" {
		err = s.db.QueryRowContext(ctx, `
			INSERT INTO tenant_company_profiles (
				tenant_id, name, address, contact, overview, extra_info,
				created_by_user_id, updated_by_user_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
			RETURNING id, tenant_id, name, address, contact, overview, extra_info,
				COALESCE(created_by_user_id::text,''), COALESCE(updated_by_user_id::text,''), created_at, updated_at
		`, tenantID, strings.TrimSpace(item.Name), strings.TrimSpace(item.Address), strings.TrimSpace(item.Contact),
			strings.TrimSpace(item.Overview), strings.TrimSpace(item.ExtraInfo), userID).Scan(
			&saved.ID, &saved.TenantID, &saved.Name, &saved.Address, &saved.Contact,
			&saved.Overview, &saved.ExtraInfo, &saved.CreatedByUserID, &saved.UpdatedByUserID,
			&saved.CreatedAt, &saved.UpdatedAt,
		)
	} else {
		err = s.db.QueryRowContext(ctx, `
			UPDATE tenant_company_profiles
			SET name=$3, address=$4, contact=$5, overview=$6, extra_info=$7,
				updated_by_user_id=$8, updated_at=now()
			WHERE id=$1 AND tenant_id=$2
			RETURNING id, tenant_id, name, address, contact, overview, extra_info,
				COALESCE(created_by_user_id::text,''), COALESCE(updated_by_user_id::text,''), created_at, updated_at
		`, item.ID, tenantID, strings.TrimSpace(item.Name), strings.TrimSpace(item.Address),
			strings.TrimSpace(item.Contact), strings.TrimSpace(item.Overview), strings.TrimSpace(item.ExtraInfo), userID).Scan(
			&saved.ID, &saved.TenantID, &saved.Name, &saved.Address, &saved.Contact,
			&saved.Overview, &saved.ExtraInfo, &saved.CreatedByUserID, &saved.UpdatedByUserID,
			&saved.CreatedAt, &saved.UpdatedAt,
		)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return CompanyProfile{}, ErrNotFound
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return CompanyProfile{}, errors.New("这个公司档案名称已经存在")
		}
		return CompanyProfile{}, err
	}
	return saved, nil
}

// ListCompanyProfiles 返回当前团队的全部公司档案。
func (s *PostgresAutoReplyStore) ListCompanyProfiles(ctx context.Context, tenantID string) ([]CompanyProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, address, contact, overview, extra_info,
			COALESCE(created_by_user_id::text,''), COALESCE(updated_by_user_id::text,''), created_at, updated_at
		FROM tenant_company_profiles
		WHERE tenant_id=$1
		ORDER BY updated_at DESC, id
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CompanyProfile, 0)
	for rows.Next() {
		var item CompanyProfile
		if err = rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Address, &item.Contact,
			&item.Overview, &item.ExtraInfo, &item.CreatedByUserID, &item.UpdatedByUserID,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// DeleteCompanyProfile 删除没有被岗位引用的团队公司档案。
func (s *PostgresAutoReplyStore) DeleteCompanyProfile(ctx context.Context, tenantID, profileID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM tenant_company_profiles WHERE tenant_id=$1 AND id=$2`, tenantID, profileID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
			return errors.New("这个公司档案还有岗位在用，暂时不能删除")
		}
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SavePositionAutoReplyConfig 原子保存岗位自动回复配置并全量替换条件。
func (s *PostgresAutoReplyStore) SavePositionAutoReplyConfig(ctx context.Context, userEmail string, item PositionAutoReplyConfig) (PositionAutoReplyConfig, error) {
	applyAutoReplyConfigDefaults(&item)
	if err := validatePositionAutoReplyConfig(item); err != nil {
		return PositionAutoReplyConfig{}, err
	}
	userID, err := s.activeTenantUserID(ctx, item.TenantID, userEmail)
	if err != nil {
		return PositionAutoReplyConfig{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PositionAutoReplyConfig{}, err
	}
	defer tx.Rollback()
	if err = ensurePositionAndCompanyForAutoReply(ctx, tx, item); err != nil {
		return PositionAutoReplyConfig{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO position_auto_reply_configs (
			position_id, tenant_id, company_profile_id, enabled, position_description,
			resume_request_message, poll_interval_seconds, max_threads_per_checkpoint,
			version, updated_by_user_id
		) VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,1,$9)
		ON CONFLICT (position_id) DO UPDATE SET
			tenant_id=EXCLUDED.tenant_id,
			company_profile_id=EXCLUDED.company_profile_id,
			enabled=EXCLUDED.enabled,
			position_description=EXCLUDED.position_description,
			resume_request_message=EXCLUDED.resume_request_message,
			poll_interval_seconds=EXCLUDED.poll_interval_seconds,
			max_threads_per_checkpoint=EXCLUDED.max_threads_per_checkpoint,
			version=position_auto_reply_configs.version+1,
			updated_by_user_id=EXCLUDED.updated_by_user_id,
			updated_at=now()
	`, item.PositionID, item.TenantID, item.CompanyProfileID, item.Enabled,
		strings.TrimSpace(item.PositionDescription), strings.TrimSpace(item.ResumeRequestMessage),
		item.PollIntervalSeconds, item.MaxThreadsPerCheckpoint, userID)
	if err != nil {
		return PositionAutoReplyConfig{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM position_reply_conditions WHERE position_id=$1`, item.PositionID); err != nil {
		return PositionAutoReplyConfig{}, err
	}
	for index, condition := range item.Conditions {
		condition.SortOrder = index
		_, err = tx.ExecContext(ctx, `
			INSERT INTO position_reply_conditions (
				tenant_id, position_id, condition_type, content, dedupe_key,
				sort_order, enabled, created_by_user_id, updated_by_user_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		`, item.TenantID, item.PositionID, condition.Type, strings.TrimSpace(condition.Content),
			normalizeAutoReplyDedupeKey(condition.Content), index, condition.Enabled, userID)
		if err != nil {
			return PositionAutoReplyConfig{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return PositionAutoReplyConfig{}, err
	}
	return s.GetPositionAutoReplyConfig(ctx, item.TenantID, item.PositionID)
}

// GetPositionAutoReplyConfig 读取岗位自动回复配置和有序条件。
func (s *PostgresAutoReplyStore) GetPositionAutoReplyConfig(ctx context.Context, tenantID, positionID string) (PositionAutoReplyConfig, error) {
	var item PositionAutoReplyConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT position_id, tenant_id, COALESCE(company_profile_id::text,''), enabled,
			position_description, resume_request_message, poll_interval_seconds,
			max_threads_per_checkpoint, version, COALESCE(updated_by_user_id::text,''), created_at, updated_at
		FROM position_auto_reply_configs
		WHERE tenant_id=$1 AND position_id=$2
	`, tenantID, positionID).Scan(
		&item.PositionID, &item.TenantID, &item.CompanyProfileID, &item.Enabled,
		&item.PositionDescription, &item.ResumeRequestMessage, &item.PollIntervalSeconds,
		&item.MaxThreadsPerCheckpoint, &item.Version, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PositionAutoReplyConfig{}, ErrNotFound
	}
	if err != nil {
		return PositionAutoReplyConfig{}, err
	}
	item.Conditions, err = s.listPositionReplyConditions(ctx, tenantID, positionID)
	return item, err
}

// listPositionReplyConditions 读取岗位有序自动回复条件。
func (s *PostgresAutoReplyStore) listPositionReplyConditions(ctx context.Context, tenantID, positionID string) ([]PositionReplyCondition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, position_id, condition_type, content, dedupe_key,
			sort_order, enabled, COALESCE(created_by_user_id::text,''), COALESCE(updated_by_user_id::text,''), created_at, updated_at
		FROM position_reply_conditions
		WHERE tenant_id=$1 AND position_id=$2
		ORDER BY sort_order, created_at, id
	`, tenantID, positionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PositionReplyCondition, 0)
	for rows.Next() {
		var item PositionReplyCondition
		if err = rows.Scan(&item.ID, &item.TenantID, &item.PositionID, &item.Type, &item.Content,
			&item.DedupeKey, &item.SortOrder, &item.Enabled, &item.CreatedByUserID,
			&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// activeTenantUserID 确认当前邮箱是团队有效成员并返回用户标识。
func (s *PostgresAutoReplyStore) activeTenantUserID(ctx context.Context, tenantID, userEmail string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM users
		WHERE tenant_id=$1 AND LOWER(email)=LOWER($2) AND status='active'
	`, tenantID, strings.TrimSpace(userEmail)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrAutoReplyForbidden
	}
	return userID, err
}

// ensurePositionAndCompanyForAutoReply 确认岗位和公司档案都属于配置中的团队。
func ensurePositionAndCompanyForAutoReply(ctx context.Context, tx *sql.Tx, item PositionAutoReplyConfig) error {
	var positionExists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM positions p
			JOIN users u ON u.id=p.user_id
			WHERE p.id=$1 AND u.tenant_id=$2 AND u.status='active'
		)
	`, item.PositionID, item.TenantID).Scan(&positionExists)
	if err != nil {
		return err
	}
	if !positionExists {
		return ErrNotFound
	}
	if strings.TrimSpace(item.CompanyProfileID) == "" {
		return nil
	}
	var companyExists bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM tenant_company_profiles WHERE id=$1 AND tenant_id=$2)
	`, item.CompanyProfileID, item.TenantID).Scan(&companyExists)
	if err != nil {
		return err
	}
	if !companyExists {
		return fmt.Errorf("选择的公司档案不在当前团队")
	}
	return nil
}

// applyAutoReplyConfigDefaults 为旧数据和空请求补齐安全默认值。
func applyAutoReplyConfigDefaults(item *PositionAutoReplyConfig) {
	if strings.TrimSpace(item.ResumeRequestMessage) == "" {
		item.ResumeRequestMessage = AutoReplyDefaultResumeRequestMessage
	}
	if item.PollIntervalSeconds == 0 {
		item.PollIntervalSeconds = 5
	}
	if item.MaxThreadsPerCheckpoint == 0 {
		item.MaxThreadsPerCheckpoint = 3
	}
}
