// 本文件实现开发环境猎头推荐分页和删除权限，复用候选人可见范围并保留简历及岗位。
package httpapi

import (
	"sort"
	"strings"
)

// ListCandidateRecommendationRecords 只扫描推荐关系并分页，避免前端遍历简历再拼推荐列表。
func (s *MemoryCandidateStore) ListCandidateRecommendationRecords(scope CandidateLibraryScope, query CandidateRecommendationQuery) (CandidateRecommendationList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page, pageSize := normalizeCandidatePage(query.Page, query.PageSize)
	items := []CandidateRecommendationRecord{}
	for _, recommendation := range s.recommendations {
		candidate, exists := s.profiles[recommendation.CandidateID]
		if !exists || !candidateVisible(scope, candidate) {
			continue
		}
		position, positionExists := query.Positions[recommendation.PositionID]
		name := recommendation.PositionName
		if positionExists && position.Name != "" {
			name = position.Name
		}
		date := recommendation.CreatedAt.In(chinaLocation()).Format("2006-01-02")
		if (query.PositionID != "" && query.PositionID != recommendation.PositionID) || (query.Stage != "" && query.Stage != recommendation.Stage) || (query.Author != "" && !strings.Contains(strings.ToLower(recommendation.AuthorEmail+" "+recommendation.AuthorName), strings.ToLower(query.Author))) || (query.CreatedFrom != "" && date < query.CreatedFrom) || (query.CreatedTo != "" && date > query.CreatedTo) {
			continue
		}
		if query.Keyword != "" && !strings.Contains(strings.ToLower(candidate.CandidateName+" "+candidate.Phone+" "+name), strings.ToLower(query.Keyword)) {
			continue
		}
		recommendation.PositionName = name
		recommendation.Events = append([]CandidateRecommendationEvent{}, recommendation.Events...)
		open := positionExists && position.IsOpen
		items = append(items, CandidateRecommendationRecord{CandidateRecommendation: recommendation, CandidateName: candidate.CandidateName, Phone: candidate.Phone, CurrentPosition: candidateString(candidate.LibraryFields, "current_position"), PositionIsOpen: open, CanEdit: open, CanDelete: scope.IsAdmin || recommendation.AuthorEmail == scope.UserEmail})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	total := len(items)
	start := (page - 1) * pageSize
	if start >= total {
		items = []CandidateRecommendationRecord{}
	} else {
		end := start + pageSize
		if end > total {
			end = total
		}
		items = items[start:end]
	}
	return CandidateRecommendationList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// DeleteCandidateRecommendation 仅删除可查看候选人的推荐，普通成员还须是原推荐作者。
func (s *MemoryCandidateStore) DeleteCandidateRecommendation(scope CandidateLibraryScope, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, recommendation := range s.recommendations {
		if recommendation.ID != id {
			continue
		}
		candidate, exists := s.profiles[recommendation.CandidateID]
		if !exists || !candidateVisible(scope, candidate) {
			return ErrNotFound
		}
		if !scope.IsAdmin && recommendation.AuthorEmail != scope.UserEmail {
			return ErrCandidateRecommendationForbidden
		}
		delete(s.recommendations, key)
		return nil
	}
	return ErrNotFound
}
