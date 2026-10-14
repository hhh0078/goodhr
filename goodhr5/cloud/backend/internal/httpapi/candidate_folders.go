// 本文件定义受管理的简历收藏夹、个人和团队权限，以及收藏夹管理 HTTP 接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	ErrCandidateFolderInvalid        = errors.New("收藏夹暂时不存在或您无权使用，请刷新收藏夹列表")
	ErrCandidateFolderConflict       = errors.New("这个范围里已经有同名收藏夹，换个名字试试")
	ErrCandidateFolderForbidden      = errors.New("这个收藏夹只能由创建者或团队管理员管理")
	ErrCandidateFolderScopeForbidden = errors.New("只有收藏夹创建者可以把团队收藏夹改为个人收藏夹")
)

// CandidateFolder 表示一个有明确可见范围的收藏夹，人数只统计当前用户可访问的真实简历。
type CandidateFolder struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"-"`
	Name           string    `json:"name"`
	Scope          string    `json:"scope"`
	CreatedByEmail string    `json:"created_by_email"`
	CandidateCount int       `json:"candidate_count"`
	CanEdit        bool      `json:"can_edit"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CandidateFolderStore 提供收藏夹管理能力，候选人的收藏夹关联在原简历保存事务中完成。
type CandidateFolderStore interface {
	ListCandidateFolders(scope CandidateLibraryScope) ([]CandidateFolder, error)
	SaveCandidateFolder(scope CandidateLibraryScope, id, name, visibility string) (CandidateFolder, error)
	DeleteCandidateFolder(scope CandidateLibraryScope, id string) error
}

// candidateFolderVisible 限制个人收藏夹仅作者本人可见，管理员也不能越过个人范围。
func candidateFolderVisible(scope CandidateLibraryScope, tag CandidateFolder) bool {
	return tag.ID != "" && tag.TenantID == scope.TenantID && (tag.Scope == "team" || (tag.Scope == "personal" && tag.CreatedByEmail == scope.UserEmail))
}

// candidateFolderEditable 限制个人收藏夹仅本人管理，团队收藏夹允许作者和当前团队管理员管理。
func candidateFolderEditable(scope CandidateLibraryScope, tag CandidateFolder) bool {
	return candidateFolderVisible(scope, tag) && (tag.CreatedByEmail == scope.UserEmail || (tag.Scope == "team" && scope.IsAdmin))
}

// normalizeCandidateFolder 校验收藏夹名称和可见范围，创建时必须明确个人或团队。
func normalizeCandidateFolder(name, visibility string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 50 {
		return "", "", errors.New("收藏夹名称请填写1到50个字")
	}
	if visibility != "personal" && visibility != "team" {
		return "", "", errors.New("请先选择个人收藏夹或团队收藏夹")
	}
	return name, visibility, nil
}

// candidateFolderIDs 校验收藏夹ID数组，去重并限制数量，空数组明确表示清除可见收藏夹关联。
func candidateFolderIDs(value any) ([]string, error) {
	if value == nil {
		return nil, errors.New("收藏夹编号请使用数组，清除时提交空数组")
	}
	raw, err := json.Marshal(value)
	var input []string
	if err != nil || json.Unmarshal(raw, &input) != nil || len(input) > 30 {
		return nil, errors.New("收藏夹编号请使用文字数组，最多30个")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range input {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 100 {
			return nil, ErrCandidateFolderInvalid
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result, nil
}

// applyVisibleCandidateFolders 仅补充可见收藏夹编号和明细，原始简历标签始终保留在独立字段中。
func applyVisibleCandidateFolders(item PositionCandidate, tags []CandidateFolder) PositionCandidate {
	item.FolderIDs = []string{}
	item.FolderDetails = []CandidateFolder{}
	for _, tag := range tags {
		item.FolderIDs = append(item.FolderIDs, tag.ID)
		item.FolderDetails = append(item.FolderDetails, tag)
	}
	return item
}

// Folders 复用候选人认证入口管理收藏夹；删除只解除收藏关系，不修改原始标签或删除候选人。
func (s *CandidateService) Folders(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension && r.Method != http.MethodGet {
		writeError(w, http.StatusForbidden, "插件只能选择已有收藏夹，请到简历库管理收藏夹")
		return
	}
	store, ok := s.store.(CandidateFolderStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "收藏夹还没准备好，请稍后再试")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/candidates/folders")
	id := strings.TrimPrefix(path, "/")
	if strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "这个收藏夹入口没找到")
		return
	}
	if r.Method == http.MethodGet && id == "" {
		items, err := store.ListCandidateFolders(scope)
		if err != nil {
			writeCandidateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folders": safeSlice(items)})
		return
	}
	if r.Method == http.MethodDelete && id != "" {
		if err := store.DeleteCandidateFolder(scope, id); err != nil {
			writeCandidateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if (r.Method != http.MethodPost || id != "") && (r.Method != http.MethodPatch || id == "") {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	input, err := readCandidateJSON(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, visibility, err := normalizeCandidateFolder(candidateString(input, "name"), candidateString(input, "scope"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.SaveCandidateFolder(scope, id, name, visibility)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "folder": item})
}
