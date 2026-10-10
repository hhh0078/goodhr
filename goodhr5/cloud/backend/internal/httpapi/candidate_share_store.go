// 本文件负责保存简历分享令牌摘要和候选人映射，取消分享后立即撤销全部链接。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// CandidateShare 保存不可逆令牌摘要及其所属简历，不保存简历快照或明文令牌。
type CandidateShare struct {
	TokenHash   string
	TenantID    string
	CandidateID string
	UserEmail   string
	CreatedAt   time.Time
}

// CandidateShareStore 定义简历长期分享链接的创建、读取和撤销能力。
type CandidateShareStore interface {
	SaveCandidateShare(CandidateShare) error
	LoadCandidateShare(string) (CandidateShare, error)
	RevokeCandidateShares(string, string) error
}

// MemoryCandidateShareStore 在无数据库环境保存分享映射，访问时仍检查简历是否存在。
type MemoryCandidateShareStore struct {
	mu    sync.Mutex
	items map[string]CandidateShare
}

// NewCandidateShareStore 按已初始化的数据库选择持久化或内存分享存储。
func NewCandidateShareStore(db *sql.DB) CandidateShareStore {
	if db != nil {
		return &PostgresCandidateShareStore{db: db}
	}
	return &MemoryCandidateShareStore{items: make(map[string]CandidateShare)}
}

// SaveCandidateShare 保存新链接，已分享的其它链接继续有效直到明确撤销。
func (s *MemoryCandidateShareStore) SaveCandidateShare(item CandidateShare) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[item.TokenHash] = item
	return nil
}

// LoadCandidateShare 按令牌摘要读取映射，已撤销链接统一返回不存在。
func (s *MemoryCandidateShareStore) LoadCandidateShare(hash string) (CandidateShare, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[hash]
	if !ok {
		return CandidateShare{}, ErrNotFound
	}
	return item, nil
}

// RevokeCandidateShares 撤销指定团队简历的全部链接，重复取消保持成功。
func (s *MemoryCandidateShareStore) RevokeCandidateShares(tenantID, candidateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash, item := range s.items {
		if item.TenantID == tenantID && item.CandidateID == candidateID {
			delete(s.items, hash)
		}
	}
	return nil
}

// PostgresCandidateShareStore 保存长期链接，删除简历时由外键级联撤销。
type PostgresCandidateShareStore struct{ db *sql.DB }

// SaveCandidateShare 仅持久化 SHA256 摘要，明文令牌只返回给创建者。
func (s *PostgresCandidateShareStore) SaveCandidateShare(item CandidateShare) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `INSERT INTO candidate_shares(token_hash,tenant_id,candidate_id,user_email,created_at) VALUES($1,$2,$3,$4,$5)`, item.TokenHash, item.TenantID, item.CandidateID, item.UserEmail, item.CreatedAt)
	return err
}

// LoadCandidateShare 读取仍存在的分享映射，不接受候选人编号替代令牌。
func (s *PostgresCandidateShareStore) LoadCandidateShare(hash string) (CandidateShare, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var item CandidateShare
	err := s.db.QueryRowContext(ctx, `SELECT token_hash,tenant_id,candidate_id,user_email,created_at FROM candidate_shares WHERE token_hash=$1`, hash).Scan(&item.TokenHash, &item.TenantID, &item.CandidateID, &item.UserEmail, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CandidateShare{}, ErrNotFound
	}
	return item, err
}

// RevokeCandidateShares 按团队和简历编号撤销全部分享映射。
func (s *PostgresCandidateShareStore) RevokeCandidateShares(tenantID, candidateID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `DELETE FROM candidate_shares WHERE tenant_id=$1 AND candidate_id=$2`, tenantID, candidateID)
	return err
}
