// 本文件提供猎头客户、甲方业务岗位和平台绑定的PostgreSQL存储，写入均验证团队与修订号。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// PostgresHeadhunterStore 复用业务数据库保存团队隔离的客户与完整JD。
type PostgresHeadhunterStore struct{ db *sql.DB }

// NewPostgresHeadhunterStore 返回使用既有数据库连接的猎头存储，不执行迁移。
func NewPostgresHeadhunterStore(db *sql.DB) *PostgresHeadhunterStore {
	return &PostgresHeadhunterStore{db: db}
}

// headhunterPGError 将唯一约束和关联约束转换为可读业务错误。
func headhunterPGError(err error) error {
	var pg *pq.Error
	if errors.As(err, &pg) {
		if pg.Code == "23505" {
			return ErrHeadhunterDuplicate
		}
		if pg.Code == "23503" {
			return ErrHeadhunterLinked
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// headhunterWhere 只组装预先允许的搜索字段，调用方不能提交任意SQL条件。
func headhunterWhere(scope CandidateLibraryScope, query HeadhunterQuery, jobs bool) (string, []any) {
	args := []any{scope.TenantID}
	clauses := []string{"h.tenant_id=$1"}
	if query.Status != "" {
		args = append(args, query.Status)
		clauses = append(clauses, fmt.Sprintf("h.status=$%d", len(args)))
	}
	if query.OwnerEmail != "" {
		args = append(args, query.OwnerEmail)
		clauses = append(clauses, fmt.Sprintf("h.owner_email=$%d", len(args)))
	}
	if query.Keyword != "" {
		args = append(args, candidateLike(query.Keyword))
		index := len(args)
		if jobs {
			clauses = append(clauses, fmt.Sprintf("(h.name ILIKE $%[1]d OR c.name ILIKE $%[1]d OR h.payload->>'city' ILIKE $%[1]d OR h.payload->>'department' ILIKE $%[1]d OR h.payload->>'description' ILIKE $%[1]d OR h.payload->>'requirements' ILIKE $%[1]d)", index))
		} else {
			clauses = append(clauses, fmt.Sprintf("(h.name ILIKE $%[1]d OR h.code ILIKE $%[1]d OR h.payload->>'short_name' ILIKE $%[1]d OR h.payload->>'industry' ILIKE $%[1]d OR h.payload->>'city' ILIKE $%[1]d OR h.payload->>'source' ILIKE $%[1]d)", index))
		}
	}
	if jobs && query.CustomerID != "" {
		args = append(args, query.CustomerID)
		clauses = append(clauses, fmt.Sprintf("h.customer_id=$%d", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// ListCustomers 使用数据库筛选分页，查询不检索其他负责人敏感联系人字段。
func (s *PostgresHeadhunterStore) ListCustomers(scope CandidateLibraryScope, query HeadhunterQuery) (HeadhunterCustomerList, error) {
	if !headhunterScopeValid(scope) {
		return HeadhunterCustomerList{}, ErrRecruitmentAccess
	}
	query, err := headhunterNormalizeQuery(query)
	if err != nil {
		return HeadhunterCustomerList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	where, args := headhunterWhere(scope, query, false)
	from := ` FROM headhunter_customers h WHERE ` + where
	result := HeadhunterCustomerList{Items: []HeadhunterCustomer{}, Page: query.Page, PageSize: query.PageSize}
	if err = s.db.QueryRowContext(ctx, `SELECT count(*)`+from, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT h.payload,(SELECT count(*) FROM headhunter_jobs j WHERE j.tenant_id=h.tenant_id AND j.customer_id=h.id)`+from+fmt.Sprintf(` ORDER BY h.updated_at DESC,h.id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item HeadhunterCustomer
		if err = rows.Scan(jsonScanner(&item), &item.Jobs); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// CustomerByID 始终显式限制团队编号，原始联系人仅交给可信服务层处理。
func (s *PostgresHeadhunterStore) CustomerByID(scope CandidateLibraryScope, id string) (HeadhunterCustomer, error) {
	var item HeadhunterCustomer
	if !headhunterScopeValid(scope) {
		return item, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM headhunter_customers WHERE tenant_id=$1 AND id=$2`, scope.TenantID, id).Scan(jsonScanner(&item))
	return item, headhunterPGError(err)
}

// headhunterPGLockOwner 确认负责人是当前团队已加入且未停用的成员。
func headhunterPGLockOwner(ctx context.Context, tx *sql.Tx, tenantID, email string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE tenant_id=$1 AND lower(email)=lower($2) AND status='active' FOR SHARE`, tenantID, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return headhunterInvalid("负责人需选择已加入团队的有效成员")
	}
	return err
}

// SaveCustomer 在事务内检查旧负责人、版本和有效新负责人，保存后才返回新修订。
func (s *PostgresHeadhunterStore) SaveCustomer(scope CandidateLibraryScope, item HeadhunterCustomer) (HeadhunterCustomer, error) {
	item = recruitmentClone(item)
	if !headhunterScopeValid(scope) {
		return HeadhunterCustomer{}, ErrRecruitmentAccess
	}
	if err := validateHeadhunterCustomer(&item); err != nil {
		return HeadhunterCustomer{}, err
	}
	item.OwnerEmail = strings.ToLower(strings.TrimSpace(item.OwnerEmail))
	if item.OwnerEmail == "" {
		item.OwnerEmail = scope.UserEmail
	}
	if !scope.IsAdmin && !strings.EqualFold(item.OwnerEmail, scope.UserEmail) {
		return HeadhunterCustomer{}, ErrRecruitmentAccess
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HeadhunterCustomer{}, err
	}
	defer tx.Rollback()
	creating := item.ID == ""
	if !creating {
		var old HeadhunterCustomer
		err = tx.QueryRowContext(ctx, `SELECT payload FROM headhunter_customers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.TenantID, item.ID).Scan(jsonScanner(&old))
		if err != nil {
			return HeadhunterCustomer{}, headhunterPGError(err)
		}
		if !headhunterCanManage(scope, old.OwnerEmail) {
			return HeadhunterCustomer{}, ErrRecruitmentAccess
		}
		if old.Revision != item.Revision {
			return HeadhunterCustomer{}, ErrRecruitmentVersion
		}
		item.CreatedAt = old.CreatedAt
	} else {
		item.ID = "hhc_" + recruitmentID()
		item.CreatedAt = time.Now().UTC()
		item.Revision = 0
	}
	if err = headhunterPGLockOwner(ctx, tx, scope.TenantID, item.OwnerEmail); err != nil {
		return HeadhunterCustomer{}, err
	}
	item.TenantID = scope.TenantID
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	item.CanManage = false
	item.Jobs = 0
	data, err := json.Marshal(item)
	if err != nil {
		return HeadhunterCustomer{}, err
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO headhunter_customers(id,tenant_id,name,code,owner_email,status,revision,payload,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)`, item.ID, item.TenantID, item.Name, item.Code, item.OwnerEmail, item.Status, item.Revision, string(data), item.CreatedAt, item.UpdatedAt)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE headhunter_customers SET name=$3,code=$4,owner_email=$5,status=$6,revision=$7,payload=$8::jsonb,updated_at=$9 WHERE tenant_id=$1 AND id=$2`, item.TenantID, item.ID, item.Name, item.Code, item.OwnerEmail, item.Status, item.Revision, string(data), item.UpdatedAt)
	}
	if err != nil {
		return HeadhunterCustomer{}, headhunterPGError(err)
	}
	return item, tx.Commit()
}

// DeleteCustomer 持锁确认没有甲方岗位后删除，不级联删除任何业务记录。
func (s *PostgresHeadhunterStore) DeleteCustomer(scope CandidateLibraryScope, id string, revision int64) error {
	if !headhunterScopeValid(scope) {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	var current int64
	if err = tx.QueryRowContext(ctx, `SELECT owner_email,revision FROM headhunter_customers WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.TenantID, id).Scan(&owner, &current); err != nil {
		return headhunterPGError(err)
	}
	if !headhunterCanManage(scope, owner) {
		return ErrRecruitmentAccess
	}
	if current != revision {
		return ErrRecruitmentVersion
	}
	var linked bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM headhunter_jobs WHERE tenant_id=$1 AND customer_id=$2)`, scope.TenantID, id).Scan(&linked); err != nil {
		return err
	}
	if linked {
		return ErrHeadhunterLinked
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM headhunter_customers WHERE tenant_id=$1 AND id=$2`, scope.TenantID, id); err != nil {
		return headhunterPGError(err)
	}
	return tx.Commit()
}

// headhunterJobSelect 提供规范JD、当前客户名和实际绑定编号，避免JSON快照的关联信息过期。
func headhunterJobSelect() string {
	return `SELECT h.payload,c.name,COALESCE((SELECT jsonb_agg(b.position_id::text ORDER BY b.position_id) FROM headhunter_position_bindings b WHERE b.tenant_id=h.tenant_id AND b.job_id=h.id),'[]'::jsonb) FROM headhunter_jobs h JOIN headhunter_customers c ON c.tenant_id=h.tenant_id AND c.id=h.customer_id`
}

// ListJobs 在数据库中搜索完整JD并分页，不把全库岗位读回进程筛选。
func (s *PostgresHeadhunterStore) ListJobs(scope CandidateLibraryScope, query HeadhunterQuery) (HeadhunterJobList, error) {
	if !headhunterScopeValid(scope) {
		return HeadhunterJobList{}, ErrRecruitmentAccess
	}
	query, err := headhunterNormalizeQuery(query)
	if err != nil {
		return HeadhunterJobList{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	where, args := headhunterWhere(scope, query, true)
	result := HeadhunterJobList{Items: []HeadhunterJob{}, Page: query.Page, PageSize: query.PageSize}
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM headhunter_jobs h JOIN headhunter_customers c ON c.tenant_id=h.tenant_id AND c.id=h.customer_id WHERE `+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, query.PageSize, (query.Page-1)*query.PageSize)
	rows, err := s.db.QueryContext(ctx, headhunterJobSelect()+` WHERE `+where+fmt.Sprintf(` ORDER BY h.updated_at DESC,h.id LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item HeadhunterJob
		if err = rows.Scan(jsonScanner(&item), &item.CustomerName, jsonScanner(&item.PositionIDs)); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// JobByID 向已授权团队返回完整甲方岗位及绑定编号，不读取平台敏感配置。
func (s *PostgresHeadhunterStore) JobByID(scope CandidateLibraryScope, id string) (HeadhunterJob, error) {
	var item HeadhunterJob
	if !headhunterScopeValid(scope) {
		return item, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.db.QueryRowContext(ctx, headhunterJobSelect()+` WHERE h.tenant_id=$1 AND h.id=$2`, scope.TenantID, id).Scan(jsonScanner(&item), &item.CustomerName, jsonScanner(&item.PositionIDs))
	return item, headhunterPGError(err)
}

// headhunterPGValidateBindings 一次核对所有平台岗位的团队及有效归属人，并持共享锁防止事务中被搬离。
func headhunterPGValidateBindings(ctx context.Context, tx *sql.Tx, tenantID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id::text FROM positions p JOIN users u ON u.id=p.user_id WHERE u.tenant_id=$1 AND u.status='active' AND p.id::text=ANY($2) FOR SHARE OF p,u`, tenantID, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != len(ids) {
		return headhunterInvalid("有些绑定岗位不属于当前团队，或负责人已离开，请重新选择")
	}
	return nil
}

// SaveJob 持锁校验客户、负责人和全部平台绑定，事务提交前不会暴露半套关联关系。
func (s *PostgresHeadhunterStore) SaveJob(scope CandidateLibraryScope, item HeadhunterJob) (HeadhunterJob, error) {
	item = recruitmentClone(item)
	if !headhunterScopeValid(scope) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	if err := validateHeadhunterJob(&item); err != nil {
		return HeadhunterJob{}, err
	}
	item.OwnerEmail = strings.ToLower(strings.TrimSpace(item.OwnerEmail))
	if item.OwnerEmail == "" {
		item.OwnerEmail = scope.UserEmail
	}
	if !scope.IsAdmin && !strings.EqualFold(item.OwnerEmail, scope.UserEmail) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HeadhunterJob{}, err
	}
	defer tx.Rollback()
	creating := item.ID == ""
	var old HeadhunterJob
	history := []string{}
	if !creating {
		err = tx.QueryRowContext(ctx, `SELECT payload,position_history FROM headhunter_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.TenantID, item.ID).Scan(jsonScanner(&old), jsonScanner(&history))
		if err != nil {
			return HeadhunterJob{}, headhunterPGError(err)
		}
		if !headhunterCanManage(scope, old.OwnerEmail) {
			return HeadhunterJob{}, ErrRecruitmentAccess
		}
		if old.Revision != item.Revision {
			return HeadhunterJob{}, ErrRecruitmentVersion
		}
		item.CreatedAt = old.CreatedAt
	} else {
		item.ID = "hhj_" + recruitmentID()
		item.Revision = 0
		item.CreatedAt = time.Now().UTC()
	}
	var customerOwner string
	err = tx.QueryRowContext(ctx, `SELECT name,owner_email FROM headhunter_customers WHERE tenant_id=$1 AND id=$2 FOR SHARE`, scope.TenantID, item.CustomerID).Scan(&item.CustomerName, &customerOwner)
	if err != nil {
		return HeadhunterJob{}, headhunterPGError(err)
	}
	if (creating || old.CustomerID != item.CustomerID) && !headhunterCanManage(scope, customerOwner) {
		return HeadhunterJob{}, ErrRecruitmentAccess
	}
	if err = headhunterPGLockOwner(ctx, tx, scope.TenantID, item.OwnerEmail); err != nil {
		return HeadhunterJob{}, err
	}
	if err = headhunterPGValidateBindings(ctx, tx, scope.TenantID, item.PositionIDs); err != nil {
		return HeadhunterJob{}, err
	}
	item.TenantID = scope.TenantID
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	item.CanManage = false
	data, err := json.Marshal(item)
	if err != nil {
		return HeadhunterJob{}, err
	}
	historyData, err := json.Marshal(headhunterMergeIDs(history, item.PositionIDs))
	if err != nil {
		return HeadhunterJob{}, err
	}
	if creating {
		_, err = tx.ExecContext(ctx, `INSERT INTO headhunter_jobs(id,tenant_id,customer_id,name,owner_email,status,revision,payload,position_history,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11)`, item.ID, item.TenantID, item.CustomerID, item.Name, item.OwnerEmail, item.Status, item.Revision, string(data), string(historyData), item.CreatedAt, item.UpdatedAt)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE headhunter_jobs SET customer_id=$3,name=$4,owner_email=$5,status=$6,revision=$7,payload=$8::jsonb,position_history=$9::jsonb,updated_at=$10 WHERE tenant_id=$1 AND id=$2`, item.TenantID, item.ID, item.CustomerID, item.Name, item.OwnerEmail, item.Status, item.Revision, string(data), string(historyData), item.UpdatedAt)
	}
	if err != nil {
		return HeadhunterJob{}, headhunterPGError(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM headhunter_position_bindings WHERE tenant_id=$1 AND job_id=$2`, item.TenantID, item.ID); err != nil {
		return HeadhunterJob{}, err
	}
	for _, id := range item.PositionIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO headhunter_position_bindings(tenant_id,job_id,position_id) VALUES($1,$2,$3::uuid)`, item.TenantID, item.ID, id); err != nil {
			return HeadhunterJob{}, headhunterPGError(err)
		}
	}
	return item, tx.Commit()
}

// DeleteJob 拒绝现存绑定及历次绑定关联的推荐或企业招聘记录，不级联删除平台岗位。
func (s *PostgresHeadhunterStore) DeleteJob(scope CandidateLibraryScope, id string, revision int64) error {
	if !headhunterScopeValid(scope) {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	var current int64
	var history []string
	err = tx.QueryRowContext(ctx, `SELECT owner_email,revision,position_history FROM headhunter_jobs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.TenantID, id).Scan(&owner, &current, jsonScanner(&history))
	if err != nil {
		return headhunterPGError(err)
	}
	if !headhunterCanManage(scope, owner) {
		return ErrRecruitmentAccess
	}
	if current != revision {
		return ErrRecruitmentVersion
	}
	keys := append(history, id)
	var linked bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM headhunter_position_bindings WHERE tenant_id=$1 AND job_id=$2) OR EXISTS(SELECT 1 FROM candidate_recommendations WHERE tenant_id=$1 AND position_key=ANY($3)) OR EXISTS(SELECT 1 FROM recruitment_applications WHERE tenant_id=$1 AND position_key=ANY($3))`, scope.TenantID, id, pq.Array(keys)).Scan(&linked)
	if err != nil {
		return err
	}
	if linked {
		return ErrHeadhunterLinked
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM headhunter_jobs WHERE tenant_id=$1 AND id=$2`, scope.TenantID, id); err != nil {
		return headhunterPGError(err)
	}
	return tx.Commit()
}
