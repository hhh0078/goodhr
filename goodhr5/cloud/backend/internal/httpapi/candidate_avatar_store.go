// Package httpapi 本文件负责以最窄存储能力更新候选人自托管头像，不改动其他简历字段。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// candidateAvatarStore 定义头像上传接口需要的候选人头像原子更新能力。
type candidateAvatarStore interface {
	UpdateCandidateAvatar(context.Context, string, string, string) (string, error)
	CandidateAvatarPubliclyReferenced(context.Context, string) (bool, error)
}

// UpdateCandidateAvatar 只更新 PostgreSQL 中指定团队候选人的头像，并返回旧头像地址。
func (s *PostgresCandidateStore) UpdateCandidateAvatar(ctx context.Context, tenantID string, candidateID string, avatarURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var previous string
	if err = tx.QueryRowContext(ctx, `
		SELECT avatar_url FROM candidate_profiles
		WHERE tenant_id=$1 AND id=$2
		FOR UPDATE
	`, strings.TrimSpace(tenantID), strings.TrimSpace(candidateID)).Scan(&previous); errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	} else if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE candidate_profiles SET avatar_url=$3, updated_at=now()
		WHERE tenant_id=$1 AND id=$2
	`, strings.TrimSpace(tenantID), strings.TrimSpace(candidateID), strings.TrimSpace(avatarURL)); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return strings.TrimSpace(previous), nil
}

// CandidateAvatarPubliclyReferenced 判断头像是否仍属于候选人或可访问的公开推荐报告。
// avatarURL 为公开请求中的随机头像路径；查询失败时调用方必须拒绝访问。
func (s *PostgresCandidateStore) CandidateAvatarPubliclyReferenced(ctx context.Context, avatarURL string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var referenced bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM candidate_profiles WHERE avatar_url=$1
			UNION ALL
			SELECT 1 FROM candidate_recommendations recommendation
			WHERE `+publicRecommendationAccessSQL+`
				AND report_data #>> '{candidate,avatar_url}'=$1
		)
	`, strings.TrimSpace(avatarURL)).Scan(&referenced)
	return referenced, err
}

// UpdateCandidateAvatar 只更新内存存储中的候选人头像，并返回旧头像地址。
func (s *MemoryCandidateStore) UpdateCandidateAvatar(_ context.Context, tenantID string, candidateID string, avatarURL string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantID = strings.TrimSpace(tenantID)
	candidateID = strings.TrimSpace(candidateID)
	item, exists := s.profiles[candidateID]
	if tenantID == "" || !exists || strings.TrimSpace(s.profileTenantIDs[candidateID]) != tenantID {
		return "", ErrNotFound
	}
	previous := strings.TrimSpace(item.AvatarURL)
	item.AvatarURL = strings.TrimSpace(avatarURL)
	item.UpdatedAt = s.now()
	s.profiles[candidateID] = item
	return previous, nil
}

// CandidateAvatarPubliclyReferenced 判断内存候选人中是否仍保存指定头像。
func (s *MemoryCandidateStore) CandidateAvatarPubliclyReferenced(_ context.Context, avatarURL string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	avatarURL = strings.TrimSpace(avatarURL)
	if avatarURL == "" {
		return false, nil
	}
	for candidateID, candidate := range s.profiles {
		if strings.TrimSpace(s.profileTenantIDs[candidateID]) != "" && strings.TrimSpace(candidate.AvatarURL) == avatarURL {
			return true, nil
		}
	}
	return false, nil
}
