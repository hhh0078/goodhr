// 本文件为已通过招聘流程授权的候选人批量读取头像，避免列表逐条加载完整简历；不提供独立公开接口。
package httpapi

import (
	"context"
	"time"

	"github.com/lib/pq"
)

// CandidateAvatarSummaryStore 仅供服务层读取已授权候选人ID的头像，调用前必须完成招聘参与者与团队校验。
type CandidateAvatarSummaryStore interface {
	CandidateAvatarSummaries(tenantID string, candidateIDs []string) (map[string]string, error)
}

// CandidateAvatarSummaries 在团队边界内批量读取必要头像字段，不加载或返回其他简历资料。
func (s *PostgresCandidateStore) CandidateAvatarSummaries(tenantID string, candidateIDs []string) (map[string]string, error) {
	result := map[string]string{}
	if tenantID == "" || len(candidateIDs) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,COALESCE(library_fields->>'avatar_data_url','') FROM candidate_profiles WHERE tenant_id=$1 AND id::text=ANY($2::text[])`, tenantID, pq.Array(candidateIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, avatar string
		if err = rows.Scan(&id, &avatar); err != nil {
			return nil, err
		}
		result[id] = avatar
	}
	return result, rows.Err()
}

// CandidateAvatarSummaries 为内存开发与测试环境复用相同的团队隔离和仅头像投影。
func (s *MemoryCandidateStore) CandidateAvatarSummaries(tenantID string, candidateIDs []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := map[string]string{}
	if tenantID == "" {
		return result, nil
	}
	for _, id := range candidateIDs {
		if candidate, exists := s.profiles[id]; exists && candidate.TenantID == tenantID {
			result[id] = candidateString(candidate.LibraryFields, "avatar_data_url")
		}
	}
	return result, nil
}
