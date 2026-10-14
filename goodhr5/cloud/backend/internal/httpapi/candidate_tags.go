// 本文件定义受管理的简历标签、个人和团队权限，以及标签管理 HTTP 接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	ErrCandidateTagInvalid        = errors.New("标签暂时不存在或您无权使用，请刷新标签列表")
	ErrCandidateTagConflict       = errors.New("这个范围里已经有同名标签，换个名字试试")
	ErrCandidateTagForbidden      = errors.New("这个标签只能由创建者或团队管理员管理")
	ErrCandidateTagScopeForbidden = errors.New("只有标签创建者可以把团队标签改为个人标签")
)

// CandidateTag 表示一个有明确可见范围的标签，人数只统计当前用户可访问的真实简历。
type CandidateTag struct {
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

// CandidateTagStore 提供标签管理能力，候选人的标签关联在原简历保存事务中完成。
type CandidateTagStore interface {
	ListCandidateTags(scope CandidateLibraryScope) ([]CandidateTag, error)
	SaveCandidateTag(scope CandidateLibraryScope, id, name, visibility string) (CandidateTag, error)
	DeleteCandidateTag(scope CandidateLibraryScope, id string) error
}

// candidateTagVisible 限制个人标签仅作者本人可见，管理员也不能越过个人范围。
func candidateTagVisible(scope CandidateLibraryScope, tag CandidateTag) bool {
	return tag.ID != "" && tag.TenantID == scope.TenantID && (tag.Scope == "team" || (tag.Scope == "personal" && tag.CreatedByEmail == scope.UserEmail))
}

// candidateTagEditable 限制个人标签仅本人管理，团队标签允许作者和当前团队管理员管理。
func candidateTagEditable(scope CandidateLibraryScope, tag CandidateTag) bool {
	return candidateTagVisible(scope, tag) && (tag.CreatedByEmail == scope.UserEmail || (tag.Scope == "team" && scope.IsAdmin))
}

// normalizeCandidateTag 校验标签名称和可见范围，创建时必须明确个人或团队。
func normalizeCandidateTag(name, visibility string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 50 {
		return "", "", errors.New("标签名称请填写1到50个字")
	}
	if visibility != "personal" && visibility != "team" {
		return "", "", errors.New("请先选择个人标签或团队标签")
	}
	return name, visibility, nil
}

// candidateTagIDs 校验标签ID数组，去重并限制数量，空数组明确表示清除可见标签关联。
func candidateTagIDs(value any) ([]string, error) {
	if value == nil {
		return nil, errors.New("标签编号请使用数组，清除时提交空数组")
	}
	raw, err := json.Marshal(value)
	var input []string
	if err != nil || json.Unmarshal(raw, &input) != nil || len(input) > 30 {
		return nil, errors.New("标签编号请使用文字数组，最多30个")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, id := range input {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 100 {
			return nil, ErrCandidateTagInvalid
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result, nil
}

// applyVisibleCandidateTags 复制扩展字段并用可见关联覆盖旧标签，防止管理员读到他人个人标签。
func applyVisibleCandidateTags(item PositionCandidate, tags []CandidateTag) PositionCandidate {
	fields := map[string]any{}
	for key, value := range item.LibraryFields {
		fields[key] = value
	}
	item.LibraryFields = fields
	item.TagIDs = []string{}
	item.TagDetails = []CandidateTag{}
	names := []string{}
	for _, tag := range tags {
		item.TagIDs = append(item.TagIDs, tag.ID)
		item.TagDetails = append(item.TagDetails, tag)
		names = append(names, tag.Name)
	}
	item.LibraryFields["tags"] = names
	return item
}

// Tags 复用候选人认证入口管理标签；删除只解除关联，不删除候选人。
func (s *CandidateService) Tags(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension && r.Method != http.MethodGet {
		writeError(w, http.StatusForbidden, "插件只能选择已有标签，请到简历库管理标签")
		return
	}
	store, ok := s.store.(CandidateTagStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "标签还没准备好，请稍后再试")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/candidates/tags")
	id := strings.TrimPrefix(path, "/")
	if strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "这个标签入口没找到")
		return
	}
	if r.Method == http.MethodGet && id == "" {
		items, err := store.ListCandidateTags(scope)
		if err != nil {
			writeCandidateError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tags": safeSlice(items)})
		return
	}
	if r.Method == http.MethodDelete && id != "" {
		if err := store.DeleteCandidateTag(scope, id); err != nil {
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
	name, visibility, err := normalizeCandidateTag(candidateString(input, "name"), candidateString(input, "scope"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.SaveCandidateTag(scope, id, name, visibility)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tag": item})
}
