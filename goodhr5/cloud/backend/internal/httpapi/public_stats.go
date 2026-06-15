// 本文件负责提供官网可公开读取的系统统计接口。
package httpapi

import "net/http"

// PublicStatsService 处理官网公开统计接口。
type PublicStatsService struct {
	users      AdminUserStore
	tasks      TaskStore
	agents     AgentStore
	dailyStats SystemDailyStatsStore
}

// NewPublicStatsService 创建官网公开统计服务。
// users 为用户统计存储，tasks 为任务统计存储，agents 为本地程序绑定存储，dailyStats 为系统按日统计存储。
func NewPublicStatsService(users AdminUserStore, tasks TaskStore, agents AgentStore, dailyStats SystemDailyStatsStore) *PublicStatsService {
	return &PublicStatsService{users: users, tasks: tasks, agents: agents, dailyStats: dailyStats}
}

// Today 返回官网首页需要展示的今日统计。
// w 为响应对象，r 为请求对象。
func (s *PublicStatsService) Today(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	stats := AdminUserStats{}
	if s.users != nil {
		loaded, err := s.users.Stats()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load public stats")
			return
		}
		stats = loaded
	}
	if s.agents != nil {
		if count, err := s.agents.ActiveBindingCount(); err == nil {
			stats.AgentBindingCount = count
		}
	}
	todayGreeted := 0
	if s.tasks != nil {
		count, err := s.tasks.TodayGreetedTotal()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load public stats")
			return
		}
		todayGreeted = count
	}
	processedResumeCount := 0
	if s.dailyStats != nil {
		dailyStats, err := s.dailyStats.TodayStats()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load public stats")
			return
		}
		processedResumeCount = dailyStats.ProcessedResumeCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                           true,
		"processed_resume_count":       processedResumeCount,
		"today_greeted_count":          todayGreeted,
		"today_registered_count":       stats.TodayRegisteredCount,
		"agent_binding_count":          stats.AgentBindingCount,
		"processed_resume_count_label": intString(processedResumeCount),
		"today_greeted_count_label":    intString(todayGreeted),
		"today_registered_count_label": intString(stats.TodayRegisteredCount),
	})
}
