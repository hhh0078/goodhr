// 本文件通过 PostgreSQL 分页查询猎头推荐及候选人摘要，安全删除只解除推荐关系和阶段历史。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// candidateRecommendationWhere 复用候选人创建者权限，生成服务端分页筛选而不全扫简历。
func candidateRecommendationWhere(scope CandidateLibraryScope, query CandidateRecommendationQuery) (string, []any) {
	args := []any{scope.TenantID}
	clauses := []string{"cr.tenant_id=$1", "cp.tenant_id=$1"}
	if !scope.IsAdmin {
		args = append(args, scope.UserEmail)
		clauses = append(clauses, fmt.Sprintf("u.email=$%d", len(args)))
	}
	if query.Keyword != "" {
		args = append(args, candidateLike(query.Keyword))
		clauses = append(clauses, fmt.Sprintf("(cp.candidate_name ILIKE $%[1]d OR cp.phone ILIKE $%[1]d OR COALESCE(NULLIF(p.name,''),cr.position_name) ILIKE $%[1]d)", len(args)))
	}
	if query.PositionID != "" {
		args = append(args, query.PositionID)
		clauses = append(clauses, fmt.Sprintf("cr.position_key=$%d", len(args)))
	}
	if query.Stage != "" {
		args = append(args, query.Stage)
		clauses = append(clauses, fmt.Sprintf("cr.stage=$%d", len(args)))
	}
	if query.Author != "" {
		args = append(args, candidateLike(query.Author))
		clauses = append(clauses, fmt.Sprintf("(cr.author_email ILIKE $%[1]d OR cr.author_name ILIKE $%[1]d)", len(args)))
	}
	if query.CreatedFrom != "" {
		args = append(args, query.CreatedFrom)
		clauses = append(clauses, fmt.Sprintf("cr.created_at >= ($%d::date::timestamp AT TIME ZONE 'Asia/Shanghai')", len(args)))
	}
	if query.CreatedTo != "" {
		args = append(args, query.CreatedTo)
		clauses = append(clauses, fmt.Sprintf("cr.created_at < (($%d::date + interval '1 day') AT TIME ZONE 'Asia/Shanghai')", len(args)))
	}
	return strings.Join(clauses, " AND "), args
}

// ListCandidateRecommendationRecords 按推荐关系分页读取真实摘要，历史包含已关闭或已删除的岗位快照。
func (s *PostgresCandidateStore) ListCandidateRecommendationRecords(scope CandidateLibraryScope, query CandidateRecommendationQuery) (CandidateRecommendationList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	where, args := candidateRecommendationWhere(scope, query)
	from := ` FROM candidate_recommendations cr JOIN candidate_profiles cp ON cp.id=cr.candidate_id JOIN users u ON u.id=cp.created_by_user_id LEFT JOIN positions p ON p.id=cr.position_id WHERE ` + where
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return CandidateRecommendationList{}, err
	}
	listArgs := append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT cr.id::text,cr.candidate_id::text,COALESCE(cr.position_id::text,cr.position_key),COALESCE(NULLIF(p.name,''),cr.position_name),cr.stage,cr.author_email,cr.author_name,cr.created_at,cr.updated_at,
 cp.candidate_name,COALESCE(cp.library_fields->>'avatar_data_url',''),cp.phone,COALESCE(cp.library_fields->>'current_position',''),COALESCE(p.is_open,false),
 COALESCE((SELECT jsonb_agg(to_jsonb(ev) ORDER BY ev.occurred_at DESC,ev.created_at DESC,ev.id DESC) FROM candidate_recommendation_events ev WHERE ev.recommendation_id=cr.id),'[]'::jsonb)`+from+fmt.Sprintf(` ORDER BY cr.updated_at DESC,cr.id LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2), listArgs...)
	if err != nil {
		return CandidateRecommendationList{}, err
	}
	defer rows.Close()
	items := []CandidateRecommendationRecord{}
	for rows.Next() {
		var item CandidateRecommendationRecord
		if err = rows.Scan(&item.ID, &item.CandidateID, &item.PositionID, &item.PositionName, &item.Stage, &item.AuthorEmail, &item.AuthorName, &item.CreatedAt, &item.UpdatedAt, &item.CandidateName, &item.AvatarDataURL, &item.Phone, &item.CurrentPosition, &item.PositionIsOpen, jsonScanner(&item.Events)); err != nil {
			return CandidateRecommendationList{}, err
		}
		item.CanEdit, item.CanDelete = item.PositionIsOpen, scope.IsAdmin || item.AuthorEmail == scope.UserEmail
		items = append(items, item)
	}
	return CandidateRecommendationList{Items: items, Total: total, Page: page, PageSize: pageSize}, rows.Err()
}

// DeleteCandidateRecommendation 在事务内重新核对候选人可见范围和推荐作者，级联删除阶段历史。
func (s *PostgresCandidateStore) DeleteCandidateRecommendation(scope CandidateLibraryScope, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	where, args := candidateRecommendationWhere(scope, CandidateRecommendationQuery{})
	args = append(args, id)
	var author string
	err = tx.QueryRowContext(ctx, `SELECT cr.author_email FROM candidate_recommendations cr JOIN candidate_profiles cp ON cp.id=cr.candidate_id JOIN users u ON u.id=cp.created_by_user_id LEFT JOIN positions p ON p.id=cr.position_id WHERE `+where+fmt.Sprintf(` AND cr.id::text=$%d FOR UPDATE OF cr`, len(args)), args...).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !scope.IsAdmin && author != scope.UserEmail {
		return ErrCandidateRecommendationForbidden
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM candidate_recommendations WHERE tenant_id=$1 AND id::text=$2`, scope.TenantID, id); err != nil {
		return err
	}
	return tx.Commit()
}
