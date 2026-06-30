// 本文件负责提供团队统计页面所需的管理员汇总接口。
package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

// TeamStatsService 处理团队统计查询。
type TeamStatsService struct {
	auth        *AuthService
	db          *sql.DB
	tenantStore TenantStore
}

// NewTeamStatsService 创建团队统计服务。
// auth 用于识别当前用户，db 用于聚合团队数据，tenantStore 用于校验团队管理员权限。
func NewTeamStatsService(auth *AuthService, db *sql.DB, tenantStore TenantStore) *TeamStatsService {
	return &TeamStatsService{auth: auth, db: db, tenantStore: tenantStore}
}

// Summary 返回当前团队在指定时间范围内的员工统计。
// 仅团队管理员可访问，默认统计本月数据。
func (s *TeamStatsService) Summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.auth.SessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "session is invalid or expired")
		return
	}
	if s.db == nil || s.tenantStore == nil {
		writeJSON(w, http.StatusOK, emptyTeamStatsPayload())
		return
	}
	tenant, err := s.tenantStore.GetOrCreateTenant(session.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	isAdmin, _ := s.tenantStore.IsTenantAdmin(tenant.ID, session.Email)
	if !isAdmin {
		writeError(w, http.StatusForbidden, "只有团队管理员才能看统计")
		return
	}
	start, end, period := resolveTeamStatsRange(r.URL.Query().Get("period"), r.URL.Query().Get("start_date"), r.URL.Query().Get("end_date"), time.Now())
	items, err := s.loadMemberStats(tenant.ID, start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load team stats")
		return
	}
	totals := teamStatsTotals(items)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"period":     period,
		"start_date": start.Format(time.DateOnly),
		"end_date":   end.AddDate(0, 0, -1).Format(time.DateOnly),
		"totals":     totals,
		"members":    items,
	})
}

// loadMemberStats 按员工聚合团队任务和简历摘要。
// tenantID 为团队 ID，start/end 为左闭右开的统计时间范围。
func (s *TeamStatsService) loadMemberStats(tenantID string, start time.Time, end time.Time) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `
SELECT
	u.email,
	COALESCE(task_stats.task_count, 0)::int,
	COALESCE(task_stats.scanned_count, 0)::int,
	COALESCE(task_stats.skipped_count, 0)::int,
	COALESCE(task_stats.failed_count, 0)::int,
	COALESCE(candidate_stats.resume_count, 0)::int,
	COALESCE(candidate_stats.detail_count, 0)::int,
	COALESCE(candidate_stats.greeted_count, 0)::int
FROM users u
LEFT JOIN (
	SELECT
		tr.user_id,
		COUNT(*) AS task_count,
		SUM(tr.scanned_count) AS scanned_count,
		SUM(tr.skipped_count) AS skipped_count,
		SUM(tr.failed_count) AS failed_count
	FROM task_runs tr
	WHERE tr.created_at >= $2 AND tr.created_at < $3
	GROUP BY tr.user_id
) task_stats ON task_stats.user_id = u.id
LEFT JOIN (
	SELECT
		cp.created_by_user_id AS user_id,
		COUNT(DISTINCT cp.id) FILTER (WHERE cp.created_at >= $2 AND cp.created_at < $3) AS resume_count,
		COUNT(ce.id) FILTER (WHERE ce.detail_fetched_at >= $2 AND ce.detail_fetched_at < $3) AS detail_count,
		COUNT(ce.id) FILTER (WHERE ce.greeted_at >= $2 AND ce.greeted_at < $3) AS greeted_count
	FROM candidate_profiles cp
	LEFT JOIN candidate_engagements ce ON ce.candidate_id = cp.id
	WHERE cp.tenant_id = $1
	GROUP BY cp.created_by_user_id
) candidate_stats ON candidate_stats.user_id = u.id
WHERE u.tenant_id = $1
ORDER BY candidate_stats.greeted_count DESC NULLS LAST, candidate_stats.resume_count DESC NULLS LAST, u.email
`, tenantID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var email string
		var taskCount, scannedCount, skippedCount, failedCount int
		var resumeCount, detailCount, greetedCount int
		if err := rows.Scan(&email, &taskCount, &scannedCount, &skippedCount, &failedCount, &resumeCount, &detailCount, &greetedCount); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"email":         email,
			"task_count":    taskCount,
			"scanned_count": scannedCount,
			"resume_count":  resumeCount,
			"detail_count":  detailCount,
			"greeted_count": greetedCount,
			"skipped_count": skippedCount,
			"failed_count":  failedCount,
		})
	}
	return items, rows.Err()
}

// resolveTeamStatsRange 解析团队统计时间周期。
// period 支持 today、week、month、last_month、custom；默认使用本月。
func resolveTeamStatsRange(period string, startDate string, endDate string, now time.Time) (time.Time, time.Time, string) {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch strings.TrimSpace(period) {
	case "today":
		return today, today.AddDate(0, 0, 1), "today"
	case "week":
		offset := (int(today.Weekday()) + 6) % 7
		start := today.AddDate(0, 0, -offset)
		return start, start.AddDate(0, 0, 7), "week"
	case "last_month":
		start := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), "last_month"
	case "custom":
		start, startOK := parseTeamStatsDate(startDate, loc)
		end, endOK := parseTeamStatsDate(endDate, loc)
		if startOK && endOK && !end.Before(start) {
			return start, end.AddDate(0, 0, 1), "custom"
		}
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 1, 0), "month"
}

// parseTeamStatsDate 解析前端传入的日期。
// value 为空或格式错误时返回 false。
func parseTeamStatsDate(value string, loc *time.Location) (time.Time, bool) {
	parsed, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(value), loc)
	return parsed, err == nil
}

// teamStatsTotals 汇总员工统计为顶部总数。
// items 为 loadMemberStats 返回的员工列表。
func teamStatsTotals(items []map[string]any) map[string]int {
	totals := map[string]int{
		"task_count":    0,
		"scanned_count": 0,
		"resume_count":  0,
		"detail_count":  0,
		"greeted_count": 0,
		"skipped_count": 0,
		"failed_count":  0,
	}
	for _, item := range items {
		for key := range totals {
			if value, ok := item[key].(int); ok {
				totals[key] += value
			}
		}
	}
	return totals
}

// emptyTeamStatsPayload 返回无数据库时的空统计结果。
// 本地内存模式下用于保持前端页面可打开。
func emptyTeamStatsPayload() map[string]any {
	now := time.Now()
	start, end, period := resolveTeamStatsRange("month", "", "", now)
	return map[string]any{
		"ok":         true,
		"period":     period,
		"start_date": start.Format(time.DateOnly),
		"end_date":   end.AddDate(0, 0, -1).Format(time.DateOnly),
		"totals":     teamStatsTotals(nil),
		"members":    []map[string]any{},
	}
}
