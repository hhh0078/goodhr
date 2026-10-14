// 本文件实现 PostgreSQL 标签管理和关联事务，严格隔离个人标签并保留旧简历主体。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"time"
)

// candidateTagDB 统一数据库和事务的标签查询能力，所有简历写入可复用同一事务。
type candidateTagDB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// candidateTagSelectSQL 按用户可访问简历去重计数，个人可见性不依赖管理员身份。
const candidateTagSelectSQL = `SELECT t.id::text,t.tenant_id::text,t.name,t.scope,t.created_by_email,t.created_at,t.updated_at,
 (SELECT COUNT(DISTINCT l.candidate_id) FROM candidate_tag_links l JOIN candidate_profiles cp ON cp.tenant_id=l.tenant_id AND cp.id=l.candidate_id JOIN users u ON u.id=cp.created_by_user_id
  WHERE l.tenant_id=t.tenant_id AND l.tag_id=t.id AND ($3 OR u.email=$2))
 FROM candidate_tags t WHERE t.tenant_id=$1 AND (t.scope='team' OR t.created_by_email=$2)`

// scanCandidateTag 读取标签字段并按当前身份计算可管理权限。
func scanCandidateTag(scanner candidateScanner, scope CandidateLibraryScope) (CandidateTag, error) {
	var tag CandidateTag
	err := scanner.Scan(&tag.ID, &tag.TenantID, &tag.Name, &tag.Scope, &tag.CreatedByEmail, &tag.CreatedAt, &tag.UpdatedAt, &tag.CandidateCount)
	tag.CanEdit = candidateTagEditable(scope, tag)
	return tag, err
}

// ListCandidateTags 读取当前团队标签和本人个人标签，不统计内置示例或隐藏候选人。
func (s *PostgresCandidateStore) ListCandidateTags(scope CandidateLibraryScope) ([]CandidateTag, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, candidateTagSelectSQL+` ORDER BY t.name,t.id`, scope.TenantID, scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CandidateTag{}
	for rows.Next() {
		tag, err := scanCandidateTag(rows, scope)
		if err != nil {
			return nil, err
		}
		items = append(items, tag)
	}
	return items, rows.Err()
}

// readCandidateTagTx 在标签修改前锁定记录，避免关联过程中标签被并发改名或转为不可见。
func readCandidateTagTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string) (CandidateTag, error) {
	tag, err := scanCandidateTag(tx.QueryRowContext(ctx, candidateTagSelectSQL+` AND t.id::text=$4 FOR UPDATE OF t`, scope.TenantID, scope.UserEmail, scope.IsAdmin, id), scope)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateTag{}, ErrNotFound
	}
	return tag, err
}

// SaveCandidateTag 创建或修改受管理标签，使用数据库唯一索引处理并发同名请求。
func (s *PostgresCandidateStore) SaveCandidateTag(scope CandidateLibraryScope, id, name, visibility string) (CandidateTag, error) {
	name, visibility, err := normalizeCandidateTag(name, visibility)
	if err != nil {
		return CandidateTag{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateTag{}, err
	}
	defer tx.Rollback()
	if id == "" {
		err = tx.QueryRowContext(ctx, `INSERT INTO candidate_tags(tenant_id,name,scope,created_by_email) VALUES($1,$2,$3,$4) RETURNING id::text`, scope.TenantID, name, visibility, scope.UserEmail).Scan(&id)
	} else {
		tag, readErr := readCandidateTagTx(ctx, tx, scope, id)
		if readErr != nil {
			return CandidateTag{}, readErr
		}
		if !tag.CanEdit {
			return CandidateTag{}, ErrCandidateTagForbidden
		}
		if visibility == "personal" && tag.Scope == "team" && tag.CreatedByEmail != scope.UserEmail {
			return CandidateTag{}, ErrCandidateTagScopeForbidden
		}
		_, err = tx.ExecContext(ctx, `UPDATE candidate_tags SET name=$1,scope=$2,updated_at=now() WHERE id::text=$3 AND tenant_id=$4`, name, visibility, id, scope.TenantID)
	}
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return CandidateTag{}, ErrCandidateTagConflict
	}
	if err != nil {
		return CandidateTag{}, err
	}
	// 范围由团队改为创建者个人后，管理员本人可能不再可见，仍返回这次明确修改的结果。
	tag, err := scanCandidateTag(tx.QueryRowContext(ctx, `SELECT t.id::text,t.tenant_id::text,t.name,t.scope,t.created_by_email,t.created_at,t.updated_at,0 FROM candidate_tags t WHERE t.tenant_id=$1 AND t.id::text=$2`, scope.TenantID, id), scope)
	if err != nil {
		return CandidateTag{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateTag{}, err
	}
	items, err := s.ListCandidateTags(scope)
	if err != nil {
		return CandidateTag{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return tag, nil
}

// DeleteCandidateTag 仅删除标签主体，外键只级联删除标签关联，不触碰候选人主体。
func (s *PostgresCandidateStore) DeleteCandidateTag(scope CandidateLibraryScope, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tag, err := readCandidateTagTx(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	if !tag.CanEdit {
		return ErrCandidateTagForbidden
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_tags WHERE tenant_id=$1 AND id::text=$2`, scope.TenantID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// hydrateCandidateTags 批量读取候选人的可见标签，覆盖旧文本字段防止个人标签泄露。
func hydrateCandidateTags(ctx context.Context, db candidateTagDB, scope CandidateLibraryScope, items []PositionCandidate) ([]PositionCandidate, error) {
	if len(items) == 0 {
		return items, nil
	}
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	rows, err := db.QueryContext(ctx, `SELECT l.candidate_id::text,t.id::text,t.tenant_id::text,t.name,t.scope,t.created_by_email,t.created_at,t.updated_at
 FROM candidate_tag_links l JOIN candidate_tags t ON t.tenant_id=l.tenant_id AND t.id=l.tag_id
 WHERE l.tenant_id=$1 AND l.candidate_id::text IN (SELECT jsonb_array_elements_text($2::jsonb)) AND (t.scope='team' OR t.created_by_email=$3) ORDER BY t.name,t.id`, scope.TenantID, string(toJSONB(ids)), scope.UserEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCandidate := map[string][]CandidateTag{}
	for rows.Next() {
		var candidateID string
		var tag CandidateTag
		if err = rows.Scan(&candidateID, &tag.ID, &tag.TenantID, &tag.Name, &tag.Scope, &tag.CreatedByEmail, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		tag.CanEdit = candidateTagEditable(scope, tag)
		byCandidate[candidateID] = append(byCandidate[candidateID], tag)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i, item := range items {
		items[i] = applyVisibleCandidateTags(item, byCandidate[item.ID])
	}
	return items, nil
}

// candidateResolveTagsTx 将已有ID或旧名称解析为可见标签，并锁定标签避免并发权限变化。
func candidateResolveTagsTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, fields map[string]any) ([]string, bool, error) {
	if raw, present := fields["tag_ids"]; present {
		ids, err := candidateTagIDs(raw)
		if err != nil {
			return nil, true, err
		}
		for _, id := range ids {
			if _, err = readCandidateTagTx(ctx, tx, scope, id); err != nil {
				if errors.Is(err, ErrNotFound) {
					return nil, true, ErrCandidateTagInvalid
				}
				return nil, true, err
			}
		}
		return ids, true, nil
	}
	raw, present := fields["tags"]
	if !present {
		return nil, false, nil
	}
	names, err := candidateTagIDs(raw)
	if err != nil {
		return nil, true, err
	}
	ids := []string{}
	for _, name := range names {
		var id string
		err = tx.QueryRowContext(ctx, `SELECT id::text FROM candidate_tags WHERE tenant_id=$1 AND lower(name)=lower($2) AND (scope='team' OR created_by_email=$3) ORDER BY (scope='personal') DESC,id LIMIT 1 FOR UPDATE`, scope.TenantID, name, scope.UserEmail).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			if scope.IsExtension {
				return nil, true, ErrCandidateTagInvalid
			}
			// 同名个人标签可并发创建，通过索引冲突复用原标签，不影响其他成员。
			err = tx.QueryRowContext(ctx, `INSERT INTO candidate_tags(tenant_id,name,scope,created_by_email) VALUES($1,$2,'personal',$3) ON CONFLICT(tenant_id,created_by_email,lower(name)) WHERE scope='personal' DO UPDATE SET name=candidate_tags.name RETURNING id::text`, scope.TenantID, name, scope.UserEmail).Scan(&id)
		}
		if err != nil {
			return nil, true, err
		}
		ids = append(ids, id)
	}
	return ids, true, nil
}

// candidateSaveTagsTx 只替换当前操作者可见关联，保留其他成员的个人标签。
func candidateSaveTagsTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidate_tag_links l USING candidate_tags t WHERE l.tenant_id=$1 AND l.candidate_id::text=$2 AND t.tenant_id=l.tenant_id AND t.id=l.tag_id AND (t.scope='team' OR t.created_by_email=$3)`, scope.TenantID, id, scope.UserEmail); err != nil {
		return err
	}
	for _, tagID := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO candidate_tag_links(tenant_id,tag_id,candidate_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, scope.TenantID, tagID, id); err != nil {
			return fmt.Errorf("关联简历标签: %w", err)
		}
	}
	return nil
}
