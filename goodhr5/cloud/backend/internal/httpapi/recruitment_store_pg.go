// 本文件实现招聘流程PostgreSQL持久化，使用事务行锁和修订号保证多人动作不互相覆盖。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/lib/pq"
	"time"
)

// PostgresRecruitmentStore 保存团队模板、实例快照和邀请哈希。
type PostgresRecruitmentStore struct{ db *sql.DB }

// NewPostgresRecruitmentStore 复用应用已有数据库连接，不自行建立外部连接。
func NewPostgresRecruitmentStore(db *sql.DB) *PostgresRecruitmentStore {
	return &PostgresRecruitmentStore{db: db}
}

// recruitmentDBError 把唯一关系冲突转换为用户可识别的招聘业务错误。
func recruitmentDBError(err error) error {
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrRecruitmentDuplicate
	}
	return err
}

// EnsureDefault 使用团队事务锁幂等初始化默认流程，避免首次访问并发重复创建。
func (s *PostgresRecruitmentStore) EnsureDefault(tenantID string, item RecruitmentWorkflow) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "recruitment-workflow:"+tenantID); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM recruitment_workflows WHERE tenant_id=$1)`, tenantID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		item.TenantID = tenantID
		item.IsDefault = true
		data, _ := json.Marshal(item)
		_, err = tx.ExecContext(ctx, `INSERT INTO recruitment_workflows(id,tenant_id,is_default,revision,payload) VALUES($1,$2,true,$3,$4::jsonb)`, item.ID, tenantID, item.Revision, string(data))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListWorkflows 根据团队读取模板，并以独立列修正默认标记和修订号。
func (s *PostgresRecruitmentStore) ListWorkflows(tenantID string) ([]RecruitmentWorkflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT payload,is_default,revision FROM recruitment_workflows WHERE tenant_id=$1 ORDER BY created_at,id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecruitmentWorkflow{}
	for rows.Next() {
		var data []byte
		var item RecruitmentWorkflow
		var def bool
		var rev int64
		if err = rows.Scan(&data, &def, &rev); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		item.IsDefault = def
		item.Revision = rev
		items = append(items, item)
	}
	return items, rows.Err()
}

// SaveWorkflow 锁定团队模板后保存版本，默认切换在同一事务中完成。
func (s *PostgresRecruitmentStore) SaveWorkflow(tenantID string, item RecruitmentWorkflow, revision int64) (RecruitmentWorkflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecruitmentWorkflow{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "recruitment-workflow:"+tenantID); err != nil {
		return RecruitmentWorkflow{}, err
	}
	var raw []byte
	var oldDef bool
	var oldRev int64
	err = tx.QueryRowContext(ctx, `SELECT payload,is_default,revision FROM recruitment_workflows WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, item.ID).Scan(&raw, &oldDef, &oldRev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RecruitmentWorkflow{}, err
	}
	exists := err == nil
	var old RecruitmentWorkflow
	if exists {
		if err = json.Unmarshal(raw, &old); err != nil {
			return RecruitmentWorkflow{}, err
		}
		if oldRev != revision {
			return RecruitmentWorkflow{}, ErrRecruitmentVersion
		}
		item.CreatedAt = old.CreatedAt
		item.Revision = oldRev + 1
		item.Version = old.Version + 1
		if oldDef {
			item.IsDefault = true
		}
	} else {
		if revision != 0 {
			return RecruitmentWorkflow{}, ErrNotFound
		}
		item.CreatedAt = time.Now().UTC()
		item.Revision = 1
		item.Version = 1
	}
	item.TenantID = tenantID
	item.UpdatedAt = time.Now().UTC()
	if item.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE recruitment_workflows SET is_default=false,revision=revision+1,updated_at=now() WHERE tenant_id=$1 AND is_default AND id<>$2`, tenantID, item.ID); err != nil {
			return RecruitmentWorkflow{}, err
		}
	}
	data, _ := json.Marshal(item)
	if exists {
		_, err = tx.ExecContext(ctx, `UPDATE recruitment_workflows SET payload=$3::jsonb,is_default=$4,revision=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, item.ID, string(data), item.IsDefault, item.Revision)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO recruitment_workflows(id,tenant_id,is_default,revision,payload) VALUES($1,$2,$3,$4,$5::jsonb)`, item.ID, tenantID, item.IsDefault, item.Revision, string(data))
	}
	if err != nil {
		return RecruitmentWorkflow{}, recruitmentDBError(err)
	}
	if err = tx.Commit(); err != nil {
		return RecruitmentWorkflow{}, err
	}
	return item, nil
}

// DeleteWorkflow 不允许删除默认模板，并以版本防止删掉刚更新的配置。
func (s *PostgresRecruitmentStore) DeleteWorkflow(tenantID, id string, revision int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "recruitment-workflow:"+tenantID); err != nil {
		return err
	}
	var def bool
	var rev int64
	err = tx.QueryRowContext(ctx, `SELECT is_default,revision FROM recruitment_workflows WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&def, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if rev != revision {
		return ErrRecruitmentVersion
	}
	if def {
		return ErrRecruitmentInvalid
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recruitment_workflows WHERE tenant_id=$1 AND id=$2`, tenantID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ListApplications 查询指定团队的实例，参与者可见范围由服务层进一步过滤。
func (s *PostgresRecruitmentStore) ListApplications(tenantID string) ([]RecruitmentApplication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM recruitment_applications WHERE tenant_id=$1 ORDER BY updated_at DESC,id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RecruitmentApplication{}
	for rows.Next() {
		var raw []byte
		var item RecruitmentApplication
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CreateApplication 依靠进行中关系唯一索引阻止重复启用，并保存不可变模板快照。
func (s *PostgresRecruitmentStore) CreateApplication(item RecruitmentApplication) (RecruitmentApplication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	data, _ := json.Marshal(item)
	_, err := s.db.ExecContext(ctx, `INSERT INTO recruitment_applications(id,tenant_id,candidate_id,position_key,status,revision,payload) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, item.ID, item.TenantID, item.CandidateID, item.PositionID, item.Status, item.Revision, string(data))
	return item, recruitmentDBError(err)
}

// GetApplication 从团队边界内读取实例，不允许跨团队编号访问。
func (s *PostgresRecruitmentStore) GetApplication(tenantID, id string) (RecruitmentApplication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM recruitment_applications WHERE tenant_id=$1 AND id=$2`, tenantID, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return RecruitmentApplication{}, ErrNotFound
	}
	if err != nil {
		return RecruitmentApplication{}, err
	}
	var item RecruitmentApplication
	err = json.Unmarshal(data, &item)
	return item, err
}

// MutateApplication 锁住实例并校验修订号，在同一事务里保存完整动作和历史。
func (s *PostgresRecruitmentStore) MutateApplication(tenantID, id string, revision int64, change func(*RecruitmentApplication) error) (RecruitmentApplication, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecruitmentApplication{}, err
	}
	defer tx.Rollback()
	var data []byte
	var rev int64
	err = tx.QueryRowContext(ctx, `SELECT payload,revision FROM recruitment_applications WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&data, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return RecruitmentApplication{}, ErrNotFound
	}
	if err != nil {
		return RecruitmentApplication{}, err
	}
	if revision != rev {
		return RecruitmentApplication{}, ErrRecruitmentVersion
	}
	var item RecruitmentApplication
	if err = json.Unmarshal(data, &item); err != nil {
		return item, err
	}
	if err = change(&item); err != nil {
		return RecruitmentApplication{}, err
	}
	item.Revision = rev + 1
	item.UpdatedAt = time.Now().UTC()
	item.Permissions = RecruitmentPermissions{}
	data, _ = json.Marshal(item)
	_, err = tx.ExecContext(ctx, `UPDATE recruitment_applications SET payload=$3::jsonb,status=$4,revision=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2`, tenantID, id, string(data), item.Status, item.Revision)
	if err != nil {
		return RecruitmentApplication{}, err
	}
	if err = tx.Commit(); err != nil {
		return RecruitmentApplication{}, err
	}
	return item, nil
}

// SaveInvitation 锁定实例检查场次版本，避免排期变更后仍生成旧链接。
func (s *PostgresRecruitmentStore) SaveInvitation(inv RecruitmentInvitation) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM recruitment_applications WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, inv.TenantID, inv.ApplicationID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var app RecruitmentApplication
	if err = json.Unmarshal(raw, &app); err != nil {
		return err
	}
	valid := false
	for _, interview := range app.Interviews {
		if interview.ID == inv.InterviewID && interview.Status == "scheduled" && interview.Revision == inv.InterviewRevision && app.Status == "active" {
			valid = true
		}
	}
	if !valid {
		return ErrRecruitmentVersion
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO recruitment_invitations(token_hash,tenant_id,application_id,interview_id,interview_revision,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, inv.TokenHash, inv.TenantID, inv.ApplicationID, inv.InterviewID, inv.InterviewRevision, inv.ExpiresAt)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// FindInvitation 仅以不可逆哈希获取一场面试的受限确认上下文。
func (s *PostgresRecruitmentStore) FindInvitation(hash string) (RecruitmentInvitation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var item RecruitmentInvitation
	err := s.db.QueryRowContext(ctx, `SELECT token_hash,tenant_id::text,application_id,interview_id,interview_revision,expires_at FROM recruitment_invitations WHERE token_hash=$1`, hash).Scan(&item.TokenHash, &item.TenantID, &item.ApplicationID, &item.InterviewID, &item.InterviewRevision, &item.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	return item, err
}
