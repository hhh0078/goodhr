// 本文件保存按候选人当前版本计算的向量回执，数量始终按简历去重而不是按片段计数。
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const resumeVectorProfile = "qwen37-flash-1024-chunks-v1"

// resumeVectorChunk 保存脱敏经历原文、可追溯字段路径与独立向量。
type resumeVectorChunk struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Path   string    `json:"path"`
	Text   string    `json:"text"`
	Start  int       `json:"start"`
	End    int       `json:"end"`
	Vector []float64 `json:"vector,omitempty"`
}

// resumeVectorRecord 保存索引版本及每个分片的成功回执，搜索失败不重复调用已生成的向量。
type resumeVectorRecord struct {
	CandidateID   string              `json:"candidate_id"`
	TenantID      string              `json:"tenant_id"`
	OwnerEmail    string              `json:"owner_email"`
	ResumeVersion int64               `json:"resume_version"`
	Profile       string              `json:"profile"`
	Complete      bool                `json:"complete"`
	Facts         map[string]any      `json:"facts"`
	Chunks        []resumeVectorChunk `json:"chunks"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// resumeVectorStore 支持持久化生成进度及当前权限下的准确计数。
type resumeVectorStore interface {
	Get(context.Context, CandidateLibraryScope, string) (resumeVectorRecord, error)
	Save(context.Context, CandidateLibraryScope, resumeVectorRecord) error
	Counts(context.Context, CandidateLibraryScope) (int, int, error)
	Pending(context.Context, CandidateLibraryScope, int) ([]string, error)
}

// memoryResumeVectorStore 仅在无数据库的开发环境使用。
type memoryResumeVectorStore struct {
	mu         sync.Mutex
	records    map[string]resumeVectorRecord
	candidates CandidateStore
}

// newResumeVectorStore 生产复用业务数据库，不将所有向量加载到应用内存。
func newResumeVectorStore(db *sql.DB, candidates CandidateStore) resumeVectorStore {
	if db != nil {
		return &postgresResumeVectorStore{db: db}
	}
	return &memoryResumeVectorStore{records: map[string]resumeVectorRecord{}, candidates: candidates}
}

// vectorScopeQuery 构造与真实简历库一致的可见范围，排除内置虚构示例。
func vectorScopeQuery(scope CandidateLibraryScope, page int) PositionCandidateQuery {
	query := PositionCandidateQuery{Page: page, PageSize: 100, ActorEmail: scope.UserEmail}
	if !scope.IsAdmin {
		query.UserEmail = scope.UserEmail
	}
	return query
}

// Get 先验证当前简历权限，删除或撤权后的向量回执不能被读出。
func (s *memoryResumeVectorStore) Get(_ context.Context, scope CandidateLibraryScope, id string) (resumeVectorRecord, error) {
	if _, err := s.candidates.GetPositionCandidate(scope.TenantID, id, "", scope.UserEmail, scope.IsAdmin); err != nil {
		return resumeVectorRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.records[scope.TenantID+":"+id]
	if !ok {
		return item, ErrNotFound
	}
	raw, _ := json.Marshal(item)
	_ = json.Unmarshal(raw, &item)
	return item, nil
}

// Save 只允许当前版本写入，迟到结果不能覆盖已经更新的简历。
func (s *memoryResumeVectorStore) Save(_ context.Context, scope CandidateLibraryScope, item resumeVectorRecord) error {
	candidate, err := s.candidates.GetPositionCandidate(scope.TenantID, item.CandidateID, "", scope.UserEmail, scope.IsAdmin)
	if err != nil {
		return err
	}
	if candidate.RowVersion != item.ResumeVersion {
		return ErrCandidateVersion
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(item)
	var copy resumeVectorRecord
	_ = json.Unmarshal(raw, &copy)
	s.records[scope.TenantID+":"+item.CandidateID] = copy
	return nil
}

// Counts 按当前可见真实简历及当前行版本计算完成数，不计旧向量或示例。
func (s *memoryResumeVectorStore) Counts(ctx context.Context, scope CandidateLibraryScope) (int, int, error) {
	total, indexed := 0, 0
	for page := 1; ; page++ {
		result, err := s.candidates.ListPositionCandidates(scope.TenantID, vectorScopeQuery(scope, page))
		if err != nil {
			return 0, 0, err
		}
		total = result.Total
		for _, candidate := range result.Items {
			item, err := s.Get(ctx, scope, candidate.ID)
			if err == nil && item.Complete && item.Profile == resumeVectorProfile && item.ResumeVersion == candidate.RowVersion {
				indexed++
			}
		}
		if page*100 >= total {
			break
		}
	}
	return total, indexed, nil
}

// Pending 在开发模式分页枚举缺失或过期回执，返回有界批次。
func (s *memoryResumeVectorStore) Pending(ctx context.Context, scope CandidateLibraryScope, limit int) ([]string, error) {
	ids := []string{}
	for page := 1; ; page++ {
		result, err := s.candidates.ListPositionCandidates(scope.TenantID, vectorScopeQuery(scope, page))
		if err != nil {
			return nil, err
		}
		for _, candidate := range result.Items {
			item, err := s.Get(ctx, scope, candidate.ID)
			if errors.Is(err, ErrNotFound) || err == nil && (!item.Complete || item.Profile != resumeVectorProfile || item.ResumeVersion != candidate.RowVersion) {
				ids = append(ids, candidate.ID)
				if len(ids) >= limit {
					return ids, nil
				}
			} else if err != nil {
				return nil, err
			}
		}
		if page*100 >= result.Total {
			break
		}
	}
	return ids, nil
}

// postgresResumeVectorStore 使用关联权限查询及版本条件写入，删除简历时数据库级联清理回执。
type postgresResumeVectorStore struct{ db *sql.DB }

// Get 在 SQL 中强制租户及创建人权限，不信任索引中的旧权限标记。
func (s *postgresResumeVectorStore) Get(ctx context.Context, scope CandidateLibraryScope, id string) (resumeVectorRecord, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT v.payload FROM candidate_resume_vectors v JOIN candidate_profiles cp ON cp.id=v.candidate_id AND cp.tenant_id=v.tenant_id WHERE v.tenant_id=$1 AND v.candidate_id::text=$2 AND ($3 OR EXISTS(SELECT 1 FROM users u WHERE u.id=cp.created_by_user_id AND u.tenant_id=$1 AND u.email=$4))`, scope.TenantID, id, scope.IsAdmin, scope.UserEmail).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return resumeVectorRecord{}, ErrNotFound
	}
	var item resumeVectorRecord
	if err == nil {
		err = json.Unmarshal(raw, &item)
	}
	return item, err
}

// Save 原子检查候选人当前版本，过时向量不能标记完成或覆盖新版本。
func (s *postgresResumeVectorStore) Save(ctx context.Context, scope CandidateLibraryScope, item resumeVectorRecord) error {
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO candidate_resume_vectors(tenant_id,candidate_id,resume_version,profile,complete,payload,updated_at) SELECT cp.tenant_id,cp.id,$3,$4,$5,$6::jsonb,now() FROM candidate_profiles cp WHERE cp.tenant_id=$1 AND cp.id::text=$2 AND cp.row_version=$3 AND ($7 OR EXISTS(SELECT 1 FROM users u WHERE u.id=cp.created_by_user_id AND u.tenant_id=$1 AND u.email=$8)) ON CONFLICT(tenant_id,candidate_id) DO UPDATE SET resume_version=EXCLUDED.resume_version,profile=EXCLUDED.profile,complete=EXCLUDED.complete,payload=EXCLUDED.payload,updated_at=now() WHERE candidate_resume_vectors.resume_version<=EXCLUDED.resume_version`, scope.TenantID, item.CandidateID, item.ResumeVersion, resumeVectorProfile, item.Complete, string(raw), scope.IsAdmin, scope.UserEmail)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrCandidateVersion
	}
	return err
}

// Counts 直接在数据库统计，避免为百万简历读回全部正文。
func (s *postgresResumeVectorStore) Counts(ctx context.Context, scope CandidateLibraryScope) (int, int, error) {
	var total, indexed int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE v.resume_version=cp.row_version AND v.profile=$4 AND v.complete) FROM candidate_profiles cp LEFT JOIN candidate_resume_vectors v ON v.tenant_id=cp.tenant_id AND v.candidate_id=cp.id WHERE cp.tenant_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM users u WHERE u.id=cp.created_by_user_id AND u.tenant_id=$1 AND u.email=$3))`, scope.TenantID, scope.IsAdmin, scope.UserEmail, resumeVectorProfile).Scan(&total, &indexed)
	return total, indexed, err
}

// Pending 利用持久回执排除已完成简历，稳定分页无须把整个库加载进进程。
func (s *postgresResumeVectorStore) Pending(ctx context.Context, scope CandidateLibraryScope, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT cp.id::text FROM candidate_profiles cp LEFT JOIN candidate_resume_vectors v ON v.tenant_id=cp.tenant_id AND v.candidate_id=cp.id WHERE cp.tenant_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM users u WHERE u.id=cp.created_by_user_id AND u.tenant_id=$1 AND u.email=$3)) AND (v.candidate_id IS NULL OR v.resume_version<>cp.row_version OR v.profile<>$4 OR NOT v.complete) ORDER BY cp.id LIMIT $5`, scope.TenantID, scope.IsAdmin, scope.UserEmail, resumeVectorProfile, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
