// 本文件实现开发和测试环境的简历人工维护、幂等、权限隔离和岗位推荐存储。
package httpapi

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"
)

// SaveLibraryCandidate 在同一锁内查重并保存，重试同一请求不会创建第二份简历。
func (s *MemoryCandidateStore) SaveLibraryCandidate(scope CandidateLibraryScope, id string, fields map[string]any, version int64, key string) (PositionCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation := "candidate:" + id
	fingerprint := candidateFingerprint(fields)
	requestID := candidateRequestKey(scope, operation, key)
	if receipt, ok := s.requests[requestID]; requestID != "" && ok {
		if receipt.Fingerprint != fingerprint {
			return PositionCandidate{}, ErrCandidateRequest
		}
		item, ok := s.profiles[receipt.ResultID]
		if !ok || !candidateVisible(scope, item) {
			return PositionCandidate{}, ErrNotFound
		}
		return s.decorateLibraryCandidateLocked(item), nil
	}
	item, exists := s.profiles[id]
	if id != "" {
		if !exists || !candidateVisible(scope, item) {
			return PositionCandidate{}, ErrNotFound
		}
		if item.RowVersion != version {
			return PositionCandidate{}, ErrCandidateVersion
		}
	}
	phone := candidateString(fields, "phone")
	platform := candidateString(fields, "platform_id")
	platformID := candidateString(fields, "platform_candidate_id")
	for _, existing := range s.profiles {
		if existing.TenantID != scope.TenantID || existing.ID == id {
			continue
		}
		kind, _ := candidateDuplicateReasons(existing, CandidateDuplicateQuery{Phone: phone, PlatformID: platform, PlatformCandidateID: platformID})
		if kind == "exact" {
			return PositionCandidate{}, ErrCandidateDuplicate
		}
	}
	if id == "" {
		id = s.nextID("candidate")
		item = PositionCandidate{ID: id, TenantID: scope.TenantID, UserEmail: scope.UserEmail, CreatorName: scope.AuthorName, CreatedAt: s.now(), LibraryFields: map[string]any{}, RowVersion: 0, PlatformID: firstNonEmpty(platform, "manual"), PlatformCandidateID: id}
	}
	// 复制可变扩展字段，避免保存失败时污染原记录。
	raw, _ := json.Marshal(item.LibraryFields)
	item.LibraryFields = map[string]any{}
	_ = json.Unmarshal(raw, &item.LibraryFields)
	applyCandidateFields(&item, fields)
	item.RowVersion++
	item.UpdatedAt = s.now()
	protected := map[string]bool{}
	for _, field := range item.ManualFields {
		protected[field] = true
	}
	for _, field := range sortedCandidateFieldNames(fields) {
		protected[field] = true
	}
	item.ManualFields = []string{}
	for field := range protected {
		item.ManualFields = append(item.ManualFields, field)
	}
	sort.Strings(item.ManualFields)
	source := CandidateSource{PlatformID: platform, PlatformCandidateID: platformID, SourceType: candidateString(fields, "source_type"), SourceURL: candidateString(fields, "source_url")}
	if source.PlatformID != "" || source.SourceType != "" {
		found := false
		for i, old := range item.Sources {
			if old.PlatformID == source.PlatformID && old.PlatformCandidateID == source.PlatformCandidateID {
				item.Sources[i] = source
				found = true
				break
			}
		}
		if !found {
			item.Sources = append(item.Sources, source)
		}
	}
	s.profiles[id] = item
	if requestID != "" {
		s.requests[requestID] = candidateRequestReceipt{fingerprint, id}
	}
	return s.decorateLibraryCandidateLocked(item), nil
}

// FindCandidateDuplicates 只返回可查看记录，不把无权限的姓名和手机号泄露给成员。
func (s *MemoryCandidateStore) FindCandidateDuplicates(scope CandidateLibraryScope, query CandidateDuplicateQuery) ([]CandidateDuplicateMatch, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []PositionCandidate{}
	for _, item := range s.profiles {
		items = append(items, s.decorateLibraryCandidateLocked(item))
	}
	matches, hidden := selectCandidateDuplicateMatches(scope, query, items)
	return matches, hidden, nil
}

// ListCandidateRecommendations 读取候选人的全部人工推荐关系和历史。
func (s *MemoryCandidateStore) ListCandidateRecommendations(scope CandidateLibraryScope, id string) ([]CandidateRecommendation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.profiles[id]
	if !ok || !candidateVisible(scope, item) {
		return nil, ErrNotFound
	}
	return s.recommendationsLocked(id), nil
}

// SaveCandidateRecommendation 同一岗位只建立一条关系，每次明确推进追加历史。
func (s *MemoryCandidateStore) SaveCandidateRecommendation(scope CandidateLibraryScope, id string, position Position, stage, content, key string, occurredAt ...time.Time) (CandidateRecommendation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate, ok := s.profiles[id]
	if !ok || !candidateVisible(scope, candidate) {
		return CandidateRecommendation{}, ErrNotFound
	}
	operation := "recommendation:" + id + ":" + position.ID
	requestID := candidateRequestKey(scope, operation, key)
	fingerprint := candidateRecommendationFingerprint(stage, content, occurredAt)
	relationKey := id + "|" + position.ID
	item, exists := s.recommendations[relationKey]
	if receipt, ok := s.requests[requestID]; requestID != "" && ok {
		if receipt.Fingerprint != fingerprint {
			return CandidateRecommendation{}, ErrCandidateRequest
		}
		return item, nil
	}
	if !exists {
		item = CandidateRecommendation{ID: s.nextID("recommendation"), CandidateID: id, PositionID: position.ID, PositionName: position.Name, AuthorEmail: scope.UserEmail, AuthorName: scope.AuthorName, CreatedAt: s.now(), Events: []CandidateRecommendationEvent{}}
	}
	if !exists || item.Stage != stage || content != "" || (len(occurredAt) > 0 && !occurredAt[0].IsZero()) {
		event := CandidateRecommendationEvent{ID: s.nextID("recommendation_event"), FromStage: item.Stage, ToStage: stage, Content: content, AuthorEmail: scope.UserEmail, AuthorName: scope.AuthorName, CreatedAt: s.now(), OccurredAt: candidateRecommendationTime(occurredAt, s.now())}
		item.Events = append([]CandidateRecommendationEvent{event}, item.Events...)
		sort.SliceStable(item.Events, func(i, j int) bool {
			if !item.Events[i].OccurredAt.Equal(item.Events[j].OccurredAt) {
				return item.Events[i].OccurredAt.After(item.Events[j].OccurredAt)
			}
			return item.Events[i].CreatedAt.After(item.Events[j].CreatedAt)
		})
		item.Stage = stage
		item.UpdatedAt = s.now()
	}
	s.recommendations[relationKey] = item
	if requestID != "" {
		s.requests[requestID] = candidateRequestReceipt{fingerprint, item.ID}
	}
	return item, nil
}

// recommendationsLocked 在持有锁时返回候选人推荐副本，避免外部修改历史。
func (s *MemoryCandidateStore) recommendationsLocked(id string) []CandidateRecommendation {
	items := []CandidateRecommendation{}
	for _, item := range s.recommendations {
		if item.CandidateID == id {
			item.Events = append([]CandidateRecommendationEvent{}, item.Events...)
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items
}

// decorateLibraryCandidateLocked 补齐列表备注与推荐摘要。
func (s *MemoryCandidateStore) decorateLibraryCandidateLocked(item PositionCandidate) PositionCandidate {
	item.Recommendations = s.recommendationsLocked(item.ID)
	item.Notes = s.candidateNotesLocked(item.ID)
	if len(item.Notes) > 2 {
		item.Notes = item.Notes[:2]
	}
	return item
}

// saveAutomaticProfileLocked 按来源复用主体，空值和人工维护字段不覆盖。
func (s *MemoryCandidateStore) saveAutomaticProfileLocked(incoming PositionCandidate) PositionCandidate {
	for id, old := range s.profiles {
		if old.TenantID != incoming.TenantID {
			continue
		}
		same := old.PlatformID == incoming.PlatformID && old.PlatformCandidateID == candidateIdentityKey(CandidateProfileInput{PlatformCandidateID: incoming.PlatformCandidateID, CandidateName: incoming.CandidateName, Phone: incoming.Phone, Email: incoming.Email, WorkRegion: incoming.WorkRegion, WorkYears: incoming.WorkYears, BasicInfo: incoming.BasicInfo})
		for _, source := range old.Sources {
			if source.PlatformID == incoming.PlatformID && source.PlatformCandidateID == incoming.PlatformCandidateID && incoming.PlatformCandidateID != "" {
				same = true
			}
		}
		if !same {
			continue
		}
		protected := map[string]bool{}
		for _, field := range old.ManualFields {
			protected[field] = true
		}
		fields := candidateProfileFields(incoming)
		for key, value := range fields {
			if protected[key] || candidateEmptyValue(value) {
				delete(fields, key)
			}
		}
		applyCandidateFields(&old, fields)
		old.AIDetailReason = incoming.AIDetailReason
		old.AIDetailScore = incoming.AIDetailScore
		old.AIGreetReason = incoming.AIGreetReason
		old.AIGreetScore = incoming.AIGreetScore
		old.UpdatedAt = s.now()
		old.RowVersion++
		s.profiles[id] = old
		return old
	}
	if incoming.PlatformCandidateID == "" {
		incoming.PlatformCandidateID = candidateIdentityKey(CandidateProfileInput{CandidateName: incoming.CandidateName, Phone: incoming.Phone, Email: incoming.Email, WorkRegion: incoming.WorkRegion, WorkYears: incoming.WorkYears, BasicInfo: incoming.BasicInfo})
	}
	s.profiles[incoming.ID] = incoming
	return incoming
}

// candidateEmptyValue 识别自动采集缺失字段，保留人工维护内容。
func candidateEmptyValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer:
		return v.IsNil()
	case reflect.String, reflect.Slice, reflect.Map:
		return v.Len() == 0
	}
	return false
}

// candidateMatchesFilters 在内存环境使用与数据库相同的紧凑筛选条件。
func candidateMatchesFilters(item PositionCandidate, query PositionCandidateQuery) bool {
	if !candidateMatchesSource(item, query.SourceType) {
		return false
	}
	text := map[string]string{"phone": item.Phone, "region": item.WorkRegion + " " + candidateString(item.LibraryFields, "current_region") + " " + candidateString(item.LibraryFields, "expected_region"), "education": item.EducationLevel, "work_years": item.WorkYears, "status": item.WorkStatus, "creator": item.UserEmail}
	filters := map[string]string{"phone": query.Phone, "region": query.Region, "education": query.Education, "work_years": query.WorkYears, "status": query.WorkStatus, "creator": query.Creator}
	for key, value := range filters {
		if value != "" && !strings.Contains(strings.ToLower(text[key]), strings.ToLower(value)) {
			return false
		}
	}
	if query.Tag != "" {
		raw, _ := json.Marshal(item.LibraryFields["tags"])
		if !strings.Contains(strings.ToLower(string(raw)), strings.ToLower(query.Tag)) {
			return false
		}
	}
	if query.PositionID != "" {
		matched := item.PositionID == query.PositionID
		for _, rec := range item.Recommendations {
			if rec.PositionID == query.PositionID {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	date := item.CreatedAt.Format("2006-01-02")
	return (query.CreatedFrom == "" || date >= query.CreatedFrom) && (query.CreatedTo == "" || date <= query.CreatedTo)
}
