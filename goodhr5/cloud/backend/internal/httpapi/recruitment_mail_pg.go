// 本文件实现招聘邮件的PostgreSQL团队隔离存储、预置模板初始化及原子发送抢占。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PostgresRecruitmentMailStore 使用数据库唯一约束保证跨进程的发送幂等。
type PostgresRecruitmentMailStore struct{ db *sql.DB }

// NewPostgresRecruitmentMailStore 创建招聘邮件存储，不运行迁移或发送邮件。
func NewPostgresRecruitmentMailStore(db *sql.DB) *PostgresRecruitmentMailStore {
	return &PostgresRecruitmentMailStore{db: db}
}

// ensureTenant 事务初始化团队配置及预置模板，仅首次访问插入默认内容。
func (s *PostgresRecruitmentMailStore) ensureTenant(tenantID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var created string
	err = tx.QueryRowContext(ctx, `INSERT INTO recruitment_company_settings(tenant_id) VALUES($1) ON CONFLICT(tenant_id) DO NOTHING RETURNING tenant_id::text`, tenantID).Scan(&created)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if created != "" {
		for _, item := range defaultRecruitmentMailTemplates() {
			if _, err = tx.ExecContext(ctx, `INSERT INTO recruitment_mail_templates(tenant_id,name,category,subject,body,is_builtin) VALUES($1,$2,$3,$4,$5,true)`, tenantID, item.Name, item.Category, item.Subject, item.Body); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// GetCompany 读取团队招聘署名，未配置时返回空字段供用户填写。
func (s *PostgresRecruitmentMailStore) GetCompany(tenantID string) (RecruitmentCompany, error) {
	if err := s.ensureTenant(tenantID); err != nil {
		return RecruitmentCompany{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var item RecruitmentCompany
	err := s.db.QueryRowContext(ctx, `SELECT company_name,contact_name,contact_phone,contact_email,company_address FROM recruitment_company_settings WHERE tenant_id=$1`, tenantID).Scan(&item.CompanyName, &item.ContactName, &item.ContactPhone, &item.ContactEmail, &item.CompanyAddress)
	return item, err
}

// SaveCompany 更新当前团队配置，不接收发件SMTP凭据。
func (s *PostgresRecruitmentMailStore) SaveCompany(tenantID string, item RecruitmentCompany) error {
	if err := s.ensureTenant(tenantID); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE recruitment_company_settings SET company_name=$2,contact_name=$3,contact_phone=$4,contact_email=$5,company_address=$6,updated_at=now() WHERE tenant_id=$1`, tenantID, item.CompanyName, item.ContactName, item.ContactPhone, item.ContactEmail, item.CompanyAddress)
	return err
}

// scanRecruitmentMailTemplate 将固定查询列转换为团队模板。
func scanRecruitmentMailTemplate(row interface{ Scan(...any) error }) (RecruitmentMailTemplate, error) {
	var item RecruitmentMailTemplate
	err := row.Scan(&item.ID, &item.Name, &item.Category, &item.Subject, &item.Body, &item.IsBuiltin, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return item, err
}

// ListTemplates 仅列出当前团队模板，删除的预置模板不自动复建。
func (s *PostgresRecruitmentMailStore) ListTemplates(tenantID string) ([]RecruitmentMailTemplate, error) {
	if err := s.ensureTenant(tenantID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,name,category,subject,body,is_builtin,updated_at FROM recruitment_mail_templates WHERE tenant_id=$1 ORDER BY name,id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecruitmentMailTemplate{}
	for rows.Next() {
		item, err := scanRecruitmentMailTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetTemplate 按团队和编号读取模板，不能跨团队引用模板。
func (s *PostgresRecruitmentMailStore) GetTemplate(tenantID, id string) (RecruitmentMailTemplate, error) {
	if err := s.ensureTenant(tenantID); err != nil {
		return RecruitmentMailTemplate{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return scanRecruitmentMailTemplate(s.db.QueryRowContext(ctx, `SELECT id::text,name,category,subject,body,is_builtin,updated_at FROM recruitment_mail_templates WHERE tenant_id=$1 AND id::text=$2`, tenantID, id))
}

// SaveTemplate 创建或更新团队自己的模板，更新不会改变来源标记。
func (s *PostgresRecruitmentMailStore) SaveTemplate(tenantID string, item RecruitmentMailTemplate, create bool) (RecruitmentMailTemplate, error) {
	if err := s.ensureTenant(tenantID); err != nil {
		return RecruitmentMailTemplate{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if create {
		return scanRecruitmentMailTemplate(s.db.QueryRowContext(ctx, `INSERT INTO recruitment_mail_templates(tenant_id,name,category,subject,body) VALUES($1,$2,$3,$4,$5) RETURNING id::text,name,category,subject,body,is_builtin,updated_at`, tenantID, item.Name, item.Category, item.Subject, item.Body))
	}
	return scanRecruitmentMailTemplate(s.db.QueryRowContext(ctx, `UPDATE recruitment_mail_templates SET name=$3,category=$4,subject=$5,body=$6,updated_at=now() WHERE tenant_id=$1 AND id::text=$2 RETURNING id::text,name,category,subject,body,is_builtin,updated_at`, tenantID, item.ID, item.Name, item.Category, item.Subject, item.Body))
}

// DeleteTemplate 删除当前团队模板，历史邮件正文快照保持不变。
func (s *PostgresRecruitmentMailStore) DeleteTemplate(tenantID, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := s.db.ExecContext(ctx, `DELETE FROM recruitment_mail_templates WHERE tenant_id=$1 AND id::text=$2`, tenantID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrNotFound
	}
	return err
}

// scanRecruitmentMailDelivery 读取不可变快照并覆盖数据库保存的发送状态。
func scanRecruitmentMailDelivery(row interface{ Scan(...any) error }) (RecruitmentMailDelivery, error) {
	var item RecruitmentMailDelivery
	var raw []byte
	var id, tenant, actor, key, fingerprint, status, message string
	var created time.Time
	var finished *time.Time
	err := row.Scan(&id, &tenant, &actor, &key, &fingerprint, &raw, &status, &message, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal(raw, &item); err != nil {
		return item, err
	}
	item.ID = id
	item.TenantID = tenant
	item.ActorEmail = actor
	item.IdempotencyKey = key
	item.Fingerprint = fingerprint
	item.Status = status
	item.ErrorMessage = message
	item.CreatedAt = created
	item.FinishedAt = finished
	return item, nil
}

const recruitmentMailDeliveryColumns = `id::text,tenant_id::text,actor_email,idempotency_key,fingerprint,snapshot,status,error_message,created_at,finished_at`

// FindDelivery 检查当前操作者已使用的幂等编号。
func (s *PostgresRecruitmentMailStore) FindDelivery(tenantID, actor, key string) (RecruitmentMailDelivery, bool, error) {
	if err := s.recoverInterrupted(tenantID); err != nil {
		return RecruitmentMailDelivery{}, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	item, err := scanRecruitmentMailDelivery(s.db.QueryRowContext(ctx, `SELECT `+recruitmentMailDeliveryColumns+` FROM recruitment_mail_deliveries WHERE tenant_id=$1 AND actor_email=$2 AND idempotency_key=$3`, tenantID, actor, key))
	if errors.Is(err, ErrNotFound) {
		return item, false, nil
	}
	return item, err == nil, err
}

// recoverInterrupted 保守隔离崩溃后留下的sending，不推断已接收邮件为可安全重发。
func (s *PostgresRecruitmentMailStore) recoverInterrupted(tenantID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE recruitment_mail_deliveries SET status='uncertain',error_message=$2,finished_at=now() WHERE tenant_id=$1 AND status='sending' AND created_at <= $3`, tenantID, recruitmentMailInterruptedMessage, time.Now().UTC().Add(-recruitmentMailSendingLease))
	return err
}

// ClaimDelivery 在数据库中先记录sending再允许SMTP，唯一约束阻止并发重复发送。
func (s *PostgresRecruitmentMailStore) ClaimDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, bool, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return item, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	saved, err := scanRecruitmentMailDelivery(s.db.QueryRowContext(ctx, `INSERT INTO recruitment_mail_deliveries(tenant_id,actor_email,application_id,interview_id,template_id,idempotency_key,fingerprint,snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(tenant_id,actor_email,idempotency_key) DO NOTHING RETURNING `+recruitmentMailDeliveryColumns, item.TenantID, item.ActorEmail, item.ApplicationID, item.InterviewID, item.TemplateID, item.IdempotencyKey, item.Fingerprint, string(raw)))
	if err == nil {
		return saved, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return saved, false, err
	}
	saved, found, err := s.FindDelivery(item.TenantID, item.ActorEmail, item.IdempotencyKey)
	if err != nil {
		return saved, false, err
	}
	if !found {
		return saved, false, ErrNotFound
	}
	if saved.Fingerprint != item.Fingerprint {
		return saved, false, ErrRecruitmentMailConflict
	}
	return saved, false, nil
}

// PrepareDelivery 原子补全独占发送快照；重复准备和已结束发送均被拒绝。
func (s *PostgresRecruitmentMailStore) PrepareDelivery(item RecruitmentMailDelivery) (RecruitmentMailDelivery, error) {
	item.Prepared = true
	raw, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	prepared, err := scanRecruitmentMailDelivery(s.db.QueryRowContext(ctx, `UPDATE recruitment_mail_deliveries SET snapshot=$4::jsonb,snapshot_ready=true WHERE tenant_id=$1 AND id::text=$2 AND fingerprint=$3 AND status='sending' AND snapshot_ready=false RETURNING `+recruitmentMailDeliveryColumns, item.TenantID, item.ID, item.Fingerprint, string(raw)))
	if errors.Is(err, ErrNotFound) {
		err = ErrRecruitmentMailConflict
	}
	return prepared, err
}

// FinishDelivery 仅将sending更新为明确终态，不把失败或不确定任务重新排队。
func (s *PostgresRecruitmentMailStore) FinishDelivery(tenantID, id, status, message string) (RecruitmentMailDelivery, error) {
	if status != "sent" && status != "failed" && status != "uncertain" {
		return RecruitmentMailDelivery{}, errors.New("邮件结果不正确")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE recruitment_mail_deliveries SET status=$3,error_message=$4,finished_at=now() WHERE tenant_id=$1 AND id::text=$2 AND status='sending'`, tenantID, id, status, message)
	if err != nil {
		return RecruitmentMailDelivery{}, err
	}
	return scanRecruitmentMailDelivery(s.db.QueryRowContext(ctx, `SELECT `+recruitmentMailDeliveryColumns+` FROM recruitment_mail_deliveries WHERE tenant_id=$1 AND id::text=$2`, tenantID, id))
}

// ListDeliveries 列出团队指定应聘记录的最近100封邮件快照。
func (s *PostgresRecruitmentMailStore) ListDeliveries(tenantID, applicationID string) ([]RecruitmentMailDelivery, error) {
	if err := s.recoverInterrupted(tenantID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT `+recruitmentMailDeliveryColumns+` FROM recruitment_mail_deliveries WHERE tenant_id=$1 AND application_id=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, tenantID, applicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecruitmentMailDelivery{}
	for rows.Next() {
		item, err := scanRecruitmentMailDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
