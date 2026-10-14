// 本文件实现开发环境收藏夹管理、权限隔离和简历关联，删除收藏夹保留候选人主体。
package httpapi

import (
	"sort"
	"strings"
)

// ListCandidateFolders 列出可见收藏夹，人数按当前操作者可访问的候选人去重统计。
func (s *MemoryCandidateStore) ListCandidateFolders(scope CandidateLibraryScope) ([]CandidateFolder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := []CandidateFolder{}
	for _, tag := range s.folders {
		if candidateFolderVisible(scope, tag) {
			items = append(items, s.publicCandidateFolderLocked(scope, tag))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name || (items[i].Name == items[j].Name && items[i].ID < items[j].ID)
	})
	return items, nil
}

// publicCandidateFolderLocked 计算当前成员的管理权限和去重人数，调用者须持有存储锁。
func (s *MemoryCandidateStore) publicCandidateFolderLocked(scope CandidateLibraryScope, tag CandidateFolder) CandidateFolder {
	tag.CanEdit, tag.CandidateCount = candidateFolderEditable(scope, tag), 0
	for candidateID, linked := range s.folderLinks {
		if linked[tag.ID] && candidateVisible(scope, s.profiles[candidateID]) {
			tag.CandidateCount++
		}
	}
	return tag
}

// SaveCandidateFolder 创建或编辑收藏夹，同范围同名不可重复，个人收藏夹管理不受管理员身份绕过。
func (s *MemoryCandidateStore) SaveCandidateFolder(scope CandidateLibraryScope, id, name, visibility string) (CandidateFolder, error) {
	name, visibility, err := normalizeCandidateFolder(name, visibility)
	if err != nil {
		return CandidateFolder{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tag := CandidateFolder{TenantID: scope.TenantID, CreatedByEmail: scope.UserEmail, CreatedAt: s.now()}
	if id != "" {
		var exists bool
		tag, exists = s.folders[id]
		if !exists || !candidateFolderVisible(scope, tag) {
			return CandidateFolder{}, ErrNotFound
		}
		if !candidateFolderEditable(scope, tag) {
			return CandidateFolder{}, ErrCandidateFolderForbidden
		}
		if visibility == "personal" && tag.Scope == "team" && tag.CreatedByEmail != scope.UserEmail {
			return CandidateFolder{}, ErrCandidateFolderScopeForbidden
		}
	}
	for _, other := range s.folders {
		if other.ID != id && other.TenantID == scope.TenantID && other.Scope == visibility && strings.EqualFold(other.Name, name) && (visibility == "team" || other.CreatedByEmail == tag.CreatedByEmail) {
			return CandidateFolder{}, ErrCandidateFolderConflict
		}
	}
	if id == "" {
		id = s.nextID("folder")
	}
	tag.ID, tag.Name, tag.Scope, tag.UpdatedAt = id, name, visibility, s.now()
	s.folders[id] = tag
	return s.publicCandidateFolderLocked(scope, tag), nil
}

// DeleteCandidateFolder 删除收藏夹和关联，不修改候选人内容或候选人数。
func (s *MemoryCandidateStore) DeleteCandidateFolder(scope CandidateLibraryScope, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tag, exists := s.folders[id]
	if !exists || !candidateFolderVisible(scope, tag) {
		return ErrNotFound
	}
	if !candidateFolderEditable(scope, tag) {
		return ErrCandidateFolderForbidden
	}
	delete(s.folders, id)
	for _, linked := range s.folderLinks {
		delete(linked, id)
	}
	return nil
}

// candidateMutationFoldersLocked 只校验明确提交的收藏夹编号，不根据原始标签创建或关联收藏夹。
func (s *MemoryCandidateStore) candidateMutationFoldersLocked(scope CandidateLibraryScope, fields map[string]any) ([]string, bool, error) {
	raw, present := fields["folder_ids"]
	if !present {
		return nil, false, nil
	}
	selected, err := candidateFolderIDs(raw)
	if err != nil {
		return nil, true, err
	}
	for _, id := range selected {
		if !candidateFolderVisible(scope, s.folders[id]) {
			return nil, true, ErrCandidateFolderInvalid
		}
	}
	return selected, true, nil
}

// saveCandidateFoldersLocked 只替换操作者可见收藏夹，保留其他成员个人关联和原始简历标签。
func (s *MemoryCandidateStore) saveCandidateFoldersLocked(scope CandidateLibraryScope, id string, ids []string) {
	if s.folderLinks[id] == nil {
		s.folderLinks[id] = map[string]bool{}
	}
	for folderID := range s.folderLinks[id] {
		if candidateFolderVisible(scope, s.folders[folderID]) {
			delete(s.folderLinks[id], folderID)
		}
	}
	for _, folderID := range ids {
		s.folderLinks[id][folderID] = true
	}
}

// decorateCandidateFoldersLocked 只补充操作者可见收藏夹名称和ID，保留独立的原始简历标签。
func (s *MemoryCandidateStore) decorateCandidateFoldersLocked(scope CandidateLibraryScope, item PositionCandidate) PositionCandidate {
	tags := []CandidateFolder{}
	for tagID := range s.folderLinks[item.ID] {
		tag := s.folders[tagID]
		if candidateFolderVisible(scope, tag) {
			tags = append(tags, s.publicCandidateFolderLocked(scope, tag))
		}
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name || (tags[i].Name == tags[j].Name && tags[i].ID < tags[j].ID)
	})
	return applyVisibleCandidateFolders(item, tags)
}
