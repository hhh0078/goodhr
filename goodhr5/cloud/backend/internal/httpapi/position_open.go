// 本文件提供岗位独立开启开关和列表筛选，不改变任务运行状态或删除岗位数据。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ErrCandidatePositionClosed 表示岗位已关闭，禁止新增或推进候选人推荐。
var ErrCandidatePositionClosed = errors.New("这个岗位已经关闭了，换个开启的岗位试试")

// filterOpenPositions 筛选岗位独立状态，省略条件时只返回开启岗位。
func filterOpenPositions(items []Position, visibility string) []Position {
	result := []Position{}
	for _, item := range items {
		if visibility == "all" || (visibility == "false" && !item.IsOpen) || (visibility != "false" && item.IsOpen) {
			result = append(result, item)
		}
	}
	return result
}

// SetOpen 仅修改本人岗位的开启状态，关闭后保留配置、任务记录和推荐历史。
func (s *PositionService) SetOpen(w http.ResponseWriter, r *http.Request) {
	session, ok := s.currentSession(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/positions/"))
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "岗位编号没认出来")
		return
	}
	input, err := readCandidateJSON(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "岗位状态格式没认出来")
		return
	}
	open, ok := input["is_open"].(bool)
	if !ok || len(input) != 1 {
		writeError(w, http.StatusBadRequest, "请明确选择开启或关闭岗位")
		return
	}
	item, err := s.store.SetPositionOpen(session.Email, id, open)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "这个岗位暂时无法管理")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "岗位状态没保存成功，稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "position": publicPosition(item)})
}

// SetPositionOpen 持锁修改本人岗位开关，不覆盖其他配置或运行状态。
func (s *MemoryPositionStore) SetPositionOpen(userEmail, id string, open bool) (Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.positions[id]
	if !ok || item.UserEmail != userEmail {
		return Position{}, ErrNotFound
	}
	item.IsOpen, item.IsOpenSet, item.UpdatedAt = open, true, s.now()
	s.positions[id] = item
	return item, nil
}

// SetPositionOpen 只更新本人岗位开关，数据库行锁与推荐写入检查互斥。
func (s *PostgresPositionStore) SetPositionOpen(userEmail, id string, open bool) (Position, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var found string
	err := s.db.QueryRowContext(ctx, `UPDATE positions p SET is_open=$1,updated_at=now() FROM users u WHERE p.user_id=u.id AND u.email=$2 AND p.id::text=$3 RETURNING p.id::text`, open, userEmail, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return Position{}, ErrNotFound
	}
	if err != nil {
		return Position{}, err
	}
	return s.PositionByID("", userEmail, id, false)
}
