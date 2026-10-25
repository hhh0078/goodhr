// 本文件提供本人岗位筛选记录HTTP接口，明确校验正常会话、有效租户成员和岗位所有权。
package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ScreeningRecordService 只处理筛选事实快照，不启动本地任务或任何AI调用。
type ScreeningRecordService struct {
	auth      *AuthService
	positions PositionStore
	tenants   TenantStore
	store     ScreeningRecordStore
}

// NewScreeningRecordService 注入既有认证、岗位与租户存储，保留独立业务存储边界。
func NewScreeningRecordService(auth *AuthService, positions PositionStore, tenants TenantStore, store ScreeningRecordStore) *ScreeningRecordService {
	return &ScreeningRecordService{auth: auth, positions: positions, tenants: tenants, store: store}
}

// scopeFromRequest 只允许正常登录用户读取或写入自己的岗位，管理员不会获得其他成员岗位权限。
func (s *ScreeningRecordService) scopeFromRequest(w http.ResponseWriter, r *http.Request) (ScreeningScope, bool) {
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "请先登录再查看筛选记录")
		return ScreeningScope{}, false
	}
	if session.Kind == extensionSessionKind {
		writeError(w, http.StatusForbidden, "插件会话不能访问筛选记录")
		return ScreeningScope{}, false
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "api" || parts[1] != "positions" || parts[2] == "" || parts[3] != "screening-records" {
		writeError(w, http.StatusNotFound, "没有找到这个筛选记录入口")
		return ScreeningScope{}, false
	}
	tenant, err := s.tenants.GetOrCreateTenant(session.Email)
	if err != nil || tenant.ID == "" {
		writeError(w, http.StatusInternalServerError, "暂时无法确认团队，请稍后再试")
		return ScreeningScope{}, false
	}
	members, err := s.tenants.ListMembers(tenant.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "暂时无法确认团队成员")
		return ScreeningScope{}, false
	}
	active := false
	for _, member := range members {
		if strings.EqualFold(member.Email, session.Email) && member.Status == "active" {
			active = true
			break
		}
	}
	if !active {
		writeError(w, http.StatusForbidden, "当前账号不在有效团队成员中")
		return ScreeningScope{}, false
	}
	position, err := s.positions.PositionByID(tenant.ID, session.Email, parts[2], false)
	if errors.Is(err, ErrNotFound) || err == nil && !strings.EqualFold(position.UserEmail, session.Email) {
		writeError(w, http.StatusNotFound, "没有找到本人的这个岗位")
		return ScreeningScope{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "暂时无法读取岗位")
		return ScreeningScope{}, false
	}
	return ScreeningScope{TenantID: tenant.ID, PositionID: parts[2], UserEmail: strings.ToLower(session.Email)}, true
}

// Collection 处理筛选快照上报及分页查询，不提供修改或删除历史记录的入口。
func (s *ScreeningRecordService) Collection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	scope, ok := s.scopeFromRequest(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodGet {
		query, err := parseScreeningQuery(r)
		if err != nil {
			s.writeError(w, err)
			return
		}
		result, err := s.store.List(r.Context(), scope, query)
		if err != nil {
			s.writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "records": result.Records, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "筛选记录内容超过256KiB")
		} else {
			writeError(w, http.StatusBadRequest, "筛选记录内容没有读完整")
		}
		return
	}
	var snapshot ScreeningSnapshot
	if err := strictAssistantJSON(raw, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "请提交单份完整JSON快照，不能指定租户或所有者等额外字段")
		return
	}
	item, err := s.store.Save(r.Context(), scope, snapshot)
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": item.ID, "revision": item.Revision})
}

// writeError 将输入与幂等冲突映射为稳定状态码，不将底层SQL或内部所有者信息返回客户端。
func (s *ScreeningRecordService) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrScreeningRecordInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrScreeningRecordConflict):
		writeError(w, http.StatusConflict, ErrScreeningRecordConflict.Error())
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "没有找到本人的这个岗位")
	default:
		writeError(w, http.StatusInternalServerError, "筛选记录暂时没有保存或读取成功，请稍后重试")
	}
}

// parseScreeningQuery 解析白名单范围参数，保留零值并拒绝无限页长和重复歧义参数。
func parseScreeningQuery(r *http.Request) (ScreeningRecordQuery, error) {
	values := r.URL.Query()
	query := ScreeningRecordQuery{Page: 1}
	allowed := map[string]bool{"page": true, "page_size": true, "first_score_min": true, "first_score_max": true, "second_score_min": true, "second_score_max": true, "first_started_from": true, "first_started_to": true, "second_started_from": true, "second_started_to": true}
	for key, list := range values {
		if !allowed[key] || len(list) != 1 {
			return query, screeningInput("查询参数重复或不支持")
		}
	}
	if value := values.Get("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return query, screeningInput("页码应为正整数")
		}
		query.Page = page
	}
	if value := values.Get("page_size"); value != "" && value != "20" {
		return query, screeningInput("每页固定显示20条")
	}
	for key, target := range map[string]**float64{"first_score_min": &query.FirstScoreMin, "first_score_max": &query.FirstScoreMax, "second_score_min": &query.SecondScoreMin, "second_score_max": &query.SecondScoreMax} {
		if value := values.Get(key); value != "" {
			number, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return query, screeningInput("分数范围应为有效数字")
			}
			*target = &number
		}
	}
	for key, target := range map[string]**time.Time{"first_started_from": &query.FirstStartedFrom, "first_started_to": &query.FirstStartedTo, "second_started_from": &query.SecondStartedFrom, "second_started_to": &query.SecondStartedTo} {
		if value := values.Get(key); value != "" {
			at, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return query, screeningInput("请使用带时区的RFC3339时间")
			}
			at = at.UTC()
			*target = &at
		}
	}
	return query, validateScreeningQuery(&query)
}
