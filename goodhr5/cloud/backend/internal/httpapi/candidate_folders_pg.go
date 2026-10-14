// 本文件实现 PostgreSQL 收藏夹管理和关联事务，严格隔离个人收藏夹并保留旧简历主体。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"time"
)

// candidateFolderDB 统一数据库和事务的收藏夹查询能力，所有简历写入可复用同一事务。
type candidateFolderDB interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// candidateFolderSelectSQL 按用户可访问简历去重计数，个人可见性不依赖管理员身份。
const candidateFolderSelectSQL = `SELECT t.id::text,t.tenant_id::text,t.name,t.scope,t.created_by_email,t.created_at,t.updated_at,
 (SELECT COUNT(DISTINCT l.candidate_id) FROM candidate_tag_links l JOIN candidate_profiles cp ON cp.tenant_id=l.tenant_id AND cp.id=l.candidate_id JOIN users u ON u.id=cp.created_by_user_id
  WHERE l.tenant_id=t.tenant_id AND l.tag_id=t.id AND ($3 OR u.email=$2))
 FROM candidate_tags t WHERE t.tenant_id=$1 AND (t.scope='team' OR t.created_by_email=$2)`

// scanCandidateFolder 读取收藏夹字段并按当前身份计算可管理权限。
func scanCandidateFolder(scanner candidateScanner, scope CandidateLibraryScope) (CandidateFolder, error) {
	var tag CandidateFolder
	err := scanner.Scan(&tag.ID, &tag.TenantID, &tag.Name, &tag.Scope, &tag.CreatedByEmail, &tag.CreatedAt, &tag.UpdatedAt, &tag.CandidateCount)
	tag.CanEdit = candidateFolderEditable(scope, tag)
	return tag, err
}

// ListCandidateFolders 读取当前团队收藏夹和本人个人收藏夹，不统计内置示例或隐藏候选人。
func (s *PostgresCandidateStore) ListCandidateFolders(scope CandidateLibraryScope) ([]CandidateFolder, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, candidateFolderSelectSQL+` ORDER BY t.name,t.id`, scope.TenantID, scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CandidateFolder{}
	for rows.Next() {
		tag, err := scanCandidateFolder(rows, scope)
		if err != nil {
			return nil, err
		}
		items = append(items, tag)
	}
	return items, rows.Err()
}

// readCandidateFolderTx 在收藏夹修改前锁定记录，避免关联过程中收藏夹被并发改名或转为不可见。
func readCandidateFolderTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string) (CandidateFolder, error) {
	tag, err := scanCandidateFolder(tx.QueryRowContext(ctx, candidateFolderSelectSQL+` AND t.id::text=$4 FOR UPDATE OF t`, scope.TenantID, scope.UserEmail, scope.IsAdmin, id), scope)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateFolder{}, ErrNotFound
	}
	return tag, err
}

// SaveCandidateFolder 创建或修改受管理收藏夹，使用数据库唯一索引处理并发同名请求。
func (s *PostgresCandidateStore) SaveCandidateFolder(scope CandidateLibraryScope, id, name, visibility string) (CandidateFolder, error) {
	name, visibility, err := normalizeCandidateFolder(name, visibility)
	if err != nil {
		return CandidateFolder{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CandidateFolder{}, err
	}
	defer tx.Rollback()
	if id == "" {
		err = tx.QueryRowContext(ctx, `INSERT INTO candidate_tags(tenant_id,name,scope,created_by_email) VALUES($1,$2,$3,$4) RETURNING id::text`, scope.TenantID, name, visibility, scope.UserEmail).Scan(&id)
	} else {
		tag, readErr := readCandidateFolderTx(ctx, tx, scope, id)
		if readErr != nil {
			return CandidateFolder{}, readErr
		}
		if !tag.CanEdit {
			return CandidateFolder{}, ErrCandidateFolderForbidden
		}
		if visibility == "personal" && tag.Scope == "team" && tag.CreatedByEmail != scope.UserEmail {
			return CandidateFolder{}, ErrCandidateFolderScopeForbidden
		}
		_, err = tx.ExecContext(ctx, `UPDATE candidate_tags SET name=$1,scope=$2,updated_at=now() WHERE id::text=$3 AND tenant_id=$4`, name, visibility, id, scope.TenantID)
	}
	var pgErr *pq.Error
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return CandidateFolder{}, ErrCandidateFolderConflict
	}
	if err != nil {
		return CandidateFolder{}, err
	}
	// 返回这次明确修改的收藏夹，保存过程中不更新候选人原始标签。
	tag, err := scanCandidateFolder(tx.QueryRowContext(ctx, `SELECT t.id::text,t.tenant_id::text,t.name,t.scope,t.created_by_email,t.created_at,t.updated_at,0 FROM candidate_tags t WHERE t.tenant_id=$1 AND t.id::text=$2`, scope.TenantID, id), scope)
	if err != nil {
		return CandidateFolder{}, err
	}
	if err = tx.Commit(); err != nil {
		return CandidateFolder{}, err
	}
	items, err := s.ListCandidateFolders(scope)
	if err != nil {
		return CandidateFolder{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return tag, nil
}

// DeleteCandidateFolder 仅删除收藏夹主体，外键只级联删除收藏夹关联，不触碰候选人主体。
func (s *PostgresCandidateStore) DeleteCandidateFolder(scope CandidateLibraryScope, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tag, err := readCandidateFolderTx(ctx, tx, scope, id)
	if err != nil {
		return err
	}
	if !tag.CanEdit {
		return ErrCandidateFolderForbidden
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_tags WHERE tenant_id=$1 AND id::text=$2`, scope.TenantID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// hydrateCandidateFolders 批量读取候选人的可见收藏夹，只补充可见收藏夹字段，保留原始简历标签。
func hydrateCandidateFolders(ctx context.Context, db candidateFolderDB, scope CandidateLibraryScope, items []PositionCandidate) ([]PositionCandidate, error) {
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
	byCandidate := map[string][]CandidateFolder{}
	for rows.Next() {
		var candidateID string
		var tag CandidateFolder
		if err = rows.Scan(&candidateID, &tag.ID, &tag.TenantID, &tag.Name, &tag.Scope, &tag.CreatedByEmail, &tag.CreatedAt, &tag.UpdatedAt); err != nil {
			return nil, err
		}
		tag.CanEdit = candidateFolderEditable(scope, tag)
		byCandidate[candidateID] = append(byCandidate[candidateID], tag)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i, item := range items {
		items[i] = applyVisibleCandidateFolders(item, byCandidate[item.ID])
	}
	return items, nil
}

// candidateResolveFoldersTx 只解析明确提交的已有收藏夹编号，原始标签不参与任何收藏夹关联。
func candidateResolveFoldersTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, fields map[string]any) ([]string, bool, error) {
	raw, present := fields["folder_ids"]
	if !present {
		return nil, false, nil
	}
	ids, err := candidateFolderIDs(raw)
	if err != nil {
		return nil, true, err
	}
	for _, id := range ids {
		if _, err = readCandidateFolderTx(ctx, tx, scope, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, true, ErrCandidateFolderInvalid
			}
			return nil, true, err
		}
	}
	return ids, true, nil
}

// candidateSaveFoldersTx 只替换当前操作者可见关联，保留其他成员的个人收藏夹。
func candidateSaveFoldersTx(ctx context.Context, tx *sql.Tx, scope CandidateLibraryScope, id string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidate_tag_links l USING candidate_tags t WHERE l.tenant_id=$1 AND l.candidate_id::text=$2 AND t.tenant_id=l.tenant_id AND t.id=l.tag_id AND (t.scope='team' OR t.created_by_email=$3)`, scope.TenantID, id, scope.UserEmail); err != nil {
		return err
	}
	for _, tagID := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO candidate_tag_links(tenant_id,tag_id,candidate_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, scope.TenantID, tagID, id); err != nil {
			return fmt.Errorf("关联简历收藏夹: %w", err)
		}
	}
	return nil
}
