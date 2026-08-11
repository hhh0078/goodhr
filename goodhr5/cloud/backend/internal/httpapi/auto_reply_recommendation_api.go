// Package httpapi 本文件负责候选人推荐报告的公开读取、沟通记录懒加载和团队内撤销接口。
package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

// PublicRecommendation 提供无需登录的推荐报告和真实沟通记录读取接口。
func (s *AutoReplyService) PublicRecommendation(w http.ResponseWriter, r *http.Request) {
	setPublicRecommendationHeaders(w)
	if s == nil || s.store == nil {
		writeAutoReplyError(w, http.StatusServiceUnavailable, "RECOMMENDATION_STORAGE_UNAVAILABLE", "推荐报告暂时还没准备好，请稍后再试")
		return
	}
	if r.Method != http.MethodGet {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个公开页面只支持查看")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/public/recommendations/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeAutoReplyError(w, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "这份推荐报告没有找到，可能已经撤销了")
		return
	}
	publicID := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		item, err := s.store.GetPublicRecommendation(r.Context(), publicID)
		if err != nil {
			writePublicRecommendationStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recommendation": publicRecommendation(item)})
		return
	}
	if len(parts) == 2 && parts[1] == "conversations" {
		items, err := s.store.PublicRecommendationConversations(r.Context(), publicID)
		if err != nil {
			writePublicRecommendationStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "conversations": items})
		return
	}
	writeAutoReplyError(w, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "这份推荐报告地址没认出来")
}

// Recommendation 处理当前团队成员对推荐报告公开链接的管理操作。
func (s *AutoReplyService) Recommendation(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auto-reply/recommendations/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || parts[1] != "revoke" || r.Method != http.MethodPost {
		writeAutoReplyError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "这个推荐报告操作暂时不支持")
		return
	}
	requestContext, ok := s.currentRequestContext(w, r, false, false)
	if !ok {
		return
	}
	if err := s.store.RevokeRecommendationShare(r.Context(), requestContext.Tenant.ID, parts[0]); err != nil {
		writeAutoReplyStoreError(w, err, "推荐报告公开链接没撤销成功")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// publicRecommendation 只返回公开页面需要的业务字段，不暴露团队、内部任务和 AI 配置数据。
func publicRecommendation(item CandidateRecommendation) map[string]any {
	return map[string]any{
		"public_id": item.PublicID, "version": item.Version, "status": item.Status,
		"match_score": item.MatchScore, "recommendation_level": item.RecommendationLevel,
		"summary": item.Summary, "report": item.Report, "share_enabled": item.ShareEnabled,
		"source_cutoff_at": item.SourceCutoffAt, "generated_at": item.GeneratedAt,
	}
}

// setPublicRecommendationHeaders 禁止公开推荐被搜索引擎收录或被浏览器缓存。
func setPublicRecommendationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// writePublicRecommendationStoreError 把撤销、未知编号和存储故障转换为安全公开响应。
func writePublicRecommendationStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeAutoReplyError(w, http.StatusNotFound, "RECOMMENDATION_NOT_FOUND", "这份推荐报告没有找到，可能已经撤销了")
		return
	}
	writeAutoReplyInternalError(w, "RECOMMENDATION_LOAD_FAILED", "推荐报告暂时没读出来，请稍后再试", err)
}
