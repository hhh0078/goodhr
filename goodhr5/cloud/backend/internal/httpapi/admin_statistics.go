// 本文件定义仅超级管理员可见的全站统计接口、北京时间范围和汇总合同，不返回个人或候选人明细。
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// SystemStatistics 保存全站期间汇总、当前快照和同批新用户漏斗，金额均为精确十进制字符串。
type SystemStatistics struct {
	StartDate      string                 `json:"start_date"`
	EndDate        string                 `json:"end_date"`
	AsOf           time.Time              `json:"as_of"`
	Metrics        map[string]int64       `json:"metrics"`
	Amounts        map[string]string      `json:"amounts"`
	Current        map[string]int64       `json:"current"`
	CurrentAmounts map[string]string      `json:"current_amounts"`
	Funnel         []SystemFunnelStep     `json:"funnel"`
	Curve          []SystemConsumptionDay `json:"curve"`
	Platforms      []SystemStatisticGroup `json:"platforms"`
	Models         []SystemStatisticGroup `json:"models"`
	Sources        []SystemStatisticGroup `json:"sources"`
	Versions       []SystemStatisticGroup `json:"versions"`
	Errors         []SystemStatisticGroup `json:"errors"`
	Blocked        []SystemStatisticGroup `json:"blocked"`
	Warnings       []string               `json:"warnings"`
}

// SystemFunnelStep 表示同一注册批次按顺序完成某一步的去重人数。
type SystemFunnelStep struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// SystemConsumptionDay 是北京时间自然日的消费金额，无消费日期仍保留零值。
type SystemConsumptionDay struct {
	Date   string `json:"date"`
	Amount string `json:"amount"`
	Calls  int64  `json:"calls"`
}

// SystemStatisticGroup 只包含平台、模型或来源等汇总维度，不包含用户邮箱或业务正文。
type SystemStatisticGroup struct {
	Name    string           `json:"name"`
	Count   int64            `json:"count"`
	Amount  string           `json:"amount,omitempty"`
	Metrics map[string]int64 `json:"metrics,omitempty"`
}

// systemStatisticsStore 将只读聚合与HTTP鉴权分开，测试可以注入合成数据。
type systemStatisticsStore interface {
	Summary(context.Context, time.Time, time.Time, time.Time) (SystemStatistics, error)
}

// SystemStatisticsService 复用管理员权限，不接受用户或团队筛选，始终展示全站总数。
type SystemStatisticsService struct {
	admin *AdminUserService
	store systemStatisticsStore
	now   func() time.Time
}

// NewSystemStatisticsService 注入既有鉴权和数据库连接，无数据库时明确返回不可用。
func NewSystemStatisticsService(admin *AdminUserService, db *sql.DB) *SystemStatisticsService {
	service := &SystemStatisticsService{admin: admin, now: time.Now}
	if db != nil {
		service.store = &postgresSystemStatistics{db: db}
	}
	return service
}

// statisticsRange 解析今天、昨天、近七天、近三十天或不超过366天的自定义自然日，拒绝未来与倒置日期。
func statisticsRange(r *http.Request, now time.Time) (time.Time, time.Time, error) {
	today := aiUsageDay(now)
	start, end := today, today.AddDate(0, 0, 1)
	switch r.URL.Query().Get("period") {
	case "", "today":
	case "yesterday":
		start, end = today.AddDate(0, 0, -1), today
	case "last7":
		start = today.AddDate(0, 0, -6)
	case "last30":
		start = today.AddDate(0, 0, -29)
	case "custom":
		var err error
		start, err = time.ParseInLocation(time.DateOnly, r.URL.Query().Get("start_date"), today.Location())
		if err != nil {
			return start, end, errors.New("请选择完整的开始日期和结束日期")
		}
		last, err := time.ParseInLocation(time.DateOnly, r.URL.Query().Get("end_date"), today.Location())
		if err != nil || last.Before(start) || last.After(today) || last.Sub(start) > 365*24*time.Hour {
			return start, end, errors.New("一次最多查询366天，结束日期不能早于开始日期或晚于今天")
		}
		end = last.AddDate(0, 0, 1)
	default:
		return start, end, errors.New("这个统计时间没认出来，请重新选择")
	}
	return start, end, nil
}

// Summary 鉴权后获取同一数据库快照；失败明确报错，不能将读取失败伪装成零。
func (s *SystemStatisticsService) Summary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "请使用查询方式查看统计")
		return
	}
	if !s.admin.requireSuperAdmin(w, r) {
		return
	}
	session, err := s.admin.auth.SessionFromRequest(r)
	if err != nil || session.Kind == extensionSessionKind {
		writeError(w, http.StatusForbidden, "统计页请到管理后台查看")
		return
	}
	for key, values := range r.URL.Query() {
		if len(values) != 1 || (key != "period" && key != "start_date" && key != "end_date") {
			writeError(w, http.StatusBadRequest, "统计页只支持日期筛选")
			return
		}
	}
	now := s.now()
	start, end, err := statisticsRange(r, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "统计数据库还没连接好，请稍后再试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.store.Summary(ctx, start, end, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "这次统计没读完整，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stats": result})
}
