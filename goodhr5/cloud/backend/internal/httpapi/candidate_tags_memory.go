// 本文件实现开发环境标签管理、权限隔离和简历关联，删除标签保留候选人主体。
package httpapi

import (
	"sort"
	"strings"
)

// ListCandidateTags 列出可见标签，人数按当前操作者可访问的候选人去重统计。
func (s *MemoryCandidateStore) ListCandidateTags(scope CandidateLibraryScope) ([]CandidateTag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []CandidateTag{}
	for _, tag := range s.tags {
		if candidateTagVisible(scope, tag) {
			items = append(items, s.publicCandidateTagLocked(scope, tag))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name || (items[i].Name == items[j].Name && items[i].ID < items[j].ID)
	})
	return items, nil
}

// publicCandidateTagLocked 计算当前成员的管理权限和去重人数，调用者须持有存储锁。
func (s *MemoryCandidateStore) publicCandidateTagLocked(scope CandidateLibraryScope, tag CandidateTag) CandidateTag {
	tag.CanEdit, tag.CandidateCount = candidateTagEditable(scope, tag), 0
	for candidateID, linked := range s.tagLinks {
		if linked[tag.ID] && candidateVisible(scope, s.profiles[candidateID]) {
			tag.CandidateCount++
		}
	}
	return tag
}

// SaveCandidateTag 创建或编辑标签，同范围同名不可重复，个人标签管理不受管理员身份绕过。
func (s *MemoryCandidateStore) SaveCandidateTag(scope CandidateLibraryScope, id, name, visibility string) (CandidateTag, error) {
	name, visibility, err := normalizeCandidateTag(name, visibility)
	if err != nil {
		return CandidateTag{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := CandidateTag{TenantID: scope.TenantID, CreatedByEmail: scope.UserEmail, CreatedAt: s.now()}
	if id != "" {
		var exists bool
		tag, exists = s.tags[id]
		if !exists || !candidateTagVisible(scope, tag) {
			return CandidateTag{}, ErrNotFound
		}
		if !candidateTagEditable(scope, tag) {
			return CandidateTag{}, ErrCandidateTagForbidden
		}
		if visibility == "personal" && tag.Scope == "team" && tag.CreatedByEmail != scope.UserEmail {
			return CandidateTag{}, ErrCandidateTagScopeForbidden
		}
	}
	for _, other := range s.tags {
		if other.ID != id && other.TenantID == scope.TenantID && other.Scope == visibility && strings.EqualFold(other.Name, name) && (visibility == "team" || other.CreatedByEmail == tag.CreatedByEmail) {
			return CandidateTag{}, ErrCandidateTagConflict
		}
	}
	if id == "" {
		id = s.nextID("tag")
	}
	tag.ID, tag.Name, tag.Scope, tag.UpdatedAt = id, name, visibility, s.now()
	s.tags[id] = tag
	return s.publicCandidateTagLocked(scope, tag), nil
}

// DeleteCandidateTag 删除标签和关联，不修改候选人内容或候选人数。
func (s *MemoryCandidateStore) DeleteCandidateTag(scope CandidateLibraryScope, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag, exists := s.tags[id]
	if !exists || !candidateTagVisible(scope, tag) {
		return ErrNotFound
	}
	if !candidateTagEditable(scope, tag) {
		return ErrCandidateTagForbidden
	}
	delete(s.tags, id)
	for _, linked := range s.tagLinks {
		delete(linked, id)
	}
	return nil
}

// candidateMutationTagsLocked 解析已选ID和兼容名称，先完整校验，避免失败时写入部分关联。
func (s *MemoryCandidateStore) candidateMutationTagsLocked(scope CandidateLibraryScope, fields map[string]any) ([]string, []CandidateTag, bool, error) {
	ids, present := fields["tag_ids"]
	if present {
		selected, err := candidateTagIDs(ids)
		if err != nil {
			return nil, nil, true, err
		}
		for _, id := range selected {
			if !candidateTagVisible(scope, s.tags[id]) {
				return nil, nil, true, ErrCandidateTagInvalid
			}
		}
		return selected, nil, true, nil
	}
	legacy, present := fields["tags"]
	if !present {
		return nil, nil, false, nil
	}
	names, err := candidateTagIDs(legacy)
	if err != nil {
		return nil, nil, true, err
	}
	selected, pending := []string{}, []CandidateTag{}
	for _, name := range names {
		var found CandidateTag
		for _, tag := range s.tags {
			if candidateTagVisible(scope, tag) && strings.EqualFold(tag.Name, name) && (found.ID == "" || tag.Scope == "personal") {
				found = tag
			}
		}
		if found.ID == "" {
			if scope.IsExtension {
				return nil, nil, true, ErrCandidateTagInvalid
			}
			found = CandidateTag{ID: s.nextID("tag"), TenantID: scope.TenantID, Name: name, Scope: "personal", CreatedByEmail: scope.UserEmail, CreatedAt: s.now(), UpdatedAt: s.now()}
			pending = append(pending, found)
		}
		selected = append(selected, found.ID)
	}
	return selected, pending, true, nil
}

// saveCandidateTagsLocked 只替换操作者可见标签，保留其他成员的个人标签关联。
func (s *MemoryCandidateStore) saveCandidateTagsLocked(scope CandidateLibraryScope, id string, ids []string, pending []CandidateTag) {
	for _, tag := range pending {
		s.tags[tag.ID] = tag
	}
	if s.tagLinks[id] == nil {
		s.tagLinks[id] = map[string]bool{}
	}
	for tagID := range s.tagLinks[id] {
		if candidateTagVisible(scope, s.tags[tagID]) {
			delete(s.tagLinks[id], tagID)
		}
	}
	for _, tagID := range ids {
		s.tagLinks[id][tagID] = true
	}
}

// decorateCandidateTagsLocked 只返回操作者可见标签名称和ID，不直接暴露历史自由文本标签。
func (s *MemoryCandidateStore) decorateCandidateTagsLocked(scope CandidateLibraryScope, item PositionCandidate) PositionCandidate {
	tags := []CandidateTag{}
	for tagID := range s.tagLinks[item.ID] {
		tag := s.tags[tagID]
		if candidateTagVisible(scope, tag) {
			tags = append(tags, s.publicCandidateTagLocked(scope, tag))
		}
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name || (tags[i].Name == tags[j].Name && tags[i].ID < tags[j].ID)
	})
	return applyVisibleCandidateTags(item, tags)
}
