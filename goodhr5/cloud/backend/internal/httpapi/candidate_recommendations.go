// 本文件提供独立猎头推荐列表和删除接口，复用已有候选人推荐历史及权限，不新建业务流程。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

// ErrCandidateRecommendationForbidden 表示仅推荐作者或当前团队管理员可以删除这条推荐。
var ErrCandidateRecommendationForbidden = errors.New("这条推荐只能由推荐人或团队管理员删除")

// CandidateRecommendationRecord 在既有推荐记录上补充候选人和岗位状态，用于独立推荐页面。
type CandidateRecommendationRecord struct {
	CandidateRecommendation
	CandidateName   string `json:"candidate_name"`
	Phone           string `json:"phone"`
	CurrentPosition string `json:"current_position"`
	PositionIsOpen  bool   `json:"position_is_open"`
	CanEdit         bool   `json:"can_edit"`
	CanDelete       bool   `json:"can_delete"`
}

// CandidateRecommendationQuery 表示服务端推荐分页筛选，岗位快照仅供内存开发环境使用。
type CandidateRecommendationQuery struct {
	Keyword, PositionID, Stage, Author, CreatedFrom, CreatedTo string
	Page, PageSize                                             int
	Positions                                                  map[string]Position
}

// CandidateRecommendationList 表示可访问推荐的分页结果。
type CandidateRecommendationList struct {
	Items                 []CandidateRecommendationRecord
	Total, Page, PageSize int
}

// CandidateRecommendationListStore 复用现有推荐关系存储，提供跨候选人的分页列表及安全删除。
type CandidateRecommendationListStore interface {
	ListCandidateRecommendationRecords(CandidateLibraryScope, CandidateRecommendationQuery) (CandidateRecommendationList, error)
	DeleteCandidateRecommendation(CandidateLibraryScope, string) error
}

// RecommendationCollection 读取猎头推荐列表，筛选在服务端完成，关闭岗位的历史仍可查看。
func (s *CandidateService) RecommendationCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension {
		writeError(w, http.StatusForbidden, "推荐管理请到控制台操作")
		return
	}
	store, ok := s.store.(CandidateRecommendationListStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "推荐列表还没准备好，稍后再试")
		return
	}
	query := CandidateRecommendationQuery{
		Keyword:    strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("q"), r.URL.Query().Get("keyword"))),
		PositionID: strings.TrimSpace(r.URL.Query().Get("position_id")), Stage: strings.TrimSpace(r.URL.Query().Get("stage")), Author: strings.TrimSpace(r.URL.Query().Get("author")),
		CreatedFrom: r.URL.Query().Get("created_from"), CreatedTo: r.URL.Query().Get("created_to"), Page: parsePositiveInt(r.URL.Query().Get("page")), PageSize: parsePositiveInt(r.URL.Query().Get("page_size")),
	}
	if len([]rune(query.Keyword)) > 200 || len(query.Author) > 320 || (query.Stage != "" && !validRecommendationStage(query.Stage)) || !validCandidateDate(query.CreatedFrom) || !validCandidateDate(query.CreatedTo) || (query.CreatedFrom != "" && query.CreatedTo != "" && query.CreatedFrom > query.CreatedTo) {
		writeError(w, http.StatusBadRequest, "筛选条件没认出来，请检查阶段和日期")
		return
	}
	// PostgreSQL 在分页SQL内读取实时岗位状态；内存环境复用岗位存储的当前快照。
	if _, memory := s.store.(*MemoryCandidateStore); memory && s.positionStore != nil {
		positions, err := s.positionStore.ListPositions(scope.TenantID, scope.UserEmail, scope.IsAdmin)
		if err != nil {
			writeCandidateError(w, err)
			return
		}
		query.Positions = map[string]Position{}
		for _, position := range positions {
			query.Positions[position.ID] = position
		}
	}
	result, err := store.ListCandidateRecommendationRecords(scope, query)
	if err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": safeSlice(result.Items), "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

// RecommendationDetail 删除整条推荐及阶段历史，始终保留候选人和岗位主体。
func (s *CandidateService) RecommendationDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.libraryScope(w, r)
	if !ok {
		return
	}
	if scope.IsExtension {
		writeError(w, http.StatusForbidden, "推荐管理请到控制台操作")
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/candidate-recommendations/"))
	if id == "" || strings.Contains(id, "/") || len(id) > 100 {
		writeError(w, http.StatusBadRequest, "推荐编号没认出来")
		return
	}
	store, ok := s.store.(CandidateRecommendationListStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "推荐管理还没准备好，稍后再试")
		return
	}
	if err := store.DeleteCandidateRecommendation(scope, id); err != nil {
		writeCandidateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
