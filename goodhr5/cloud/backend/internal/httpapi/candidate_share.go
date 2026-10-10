// 本文件提供经过简历权限校验的分享与取消接口，并通过随机链接公开白名单简历资料。
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// CandidateShareService 复用简历库团队权限，只公开简历资料，不公开内部工作记录。
type CandidateShareService struct {
	candidates *CandidateService
	store      CandidateShareStore
}

// NewCandidateShareService 创建分享服务，沿用已有候选人详情的权限范围。
func NewCandidateShareService(candidates *CandidateService, store CandidateShareStore) *CandidateShareService {
	return &CandidateShareService{candidates: candidates, store: store}
}

// PrivateShare 创建或取消当前有权访问的简历分享，取消时撤销该简历的所有链接。
func (s *CandidateShareService) PrivateShare(w http.ResponseWriter, r *http.Request) {
	setCandidateShareHeaders(w)
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "这个分享入口暂时不支持此操作")
		return
	}
	if s.candidates == nil || s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "分享功能还没准备好，请稍后再试")
		return
	}
	scope, ok := s.candidates.libraryScope(w, r)
	if !ok {
		return
	}
	id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/candidates/"), "/share")
	if id == "" || strings.Contains(id, "/") || !strings.HasSuffix(r.URL.Path, "/share") {
		writeError(w, http.StatusNotFound, "这份简历暂时没找到")
		return
	}
	// 分享路由先于简历详情分发，必须独立拒绝预留示例身份，不能依赖示例未写入数据库。
	if id == builtinCandidateID {
		writeError(w, http.StatusForbidden, "这份是内置示例简历，仅供登录后查看，不能公开分享")
		return
	}
	if _, err := s.candidates.store.GetPositionCandidate(scope.TenantID, id, "", scope.UserEmail, scope.IsAdmin); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "这份简历暂时没找到")
		} else {
			writeError(w, http.StatusInternalServerError, "简历暂时没读出来，请稍后再试")
		}
		return
	}
	if r.Method == http.MethodDelete {
		if err := s.store.RevokeCandidateShares(scope.TenantID, id); err != nil {
			writeError(w, http.StatusInternalServerError, "分享还没取消成功，请稍后再试")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		writeError(w, http.StatusInternalServerError, "分享链接没生成成功，请再试一次")
		return
	}
	token := hex.EncodeToString(secret)
	digest := sha256.Sum256([]byte(token))
	if err := s.store.SaveCandidateShare(CandidateShare{TokenHash: hex.EncodeToString(digest[:]), TenantID: scope.TenantID, CandidateID: id, UserEmail: scope.UserEmail, CreatedAt: time.Now().UTC()}); err != nil {
		writeError(w, http.StatusInternalServerError, "分享链接没保存成功，请再试一次")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": token, "path": "/resumes/share/" + token})
}

// PublicShare 无需登录读取最新简历白名单，链接撤销或简历删除后立即返回不存在。
func (s *CandidateShareService) PublicShare(w http.ResponseWriter, r *http.Request) {
	setCandidateShareHeaders(w)
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "这个链接只用于查看简历")
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/public/resume-shares/")
	if len(token) != 64 {
		writeError(w, http.StatusNotFound, "这份分享已取消，或链接没有复制完整")
		return
	}
	if _, err := hex.DecodeString(token); err != nil {
		writeError(w, http.StatusNotFound, "这份分享已取消，或链接没有复制完整")
		return
	}
	if s.store == nil || s.candidates == nil || s.candidates.store == nil {
		writeError(w, http.StatusServiceUnavailable, "分享功能暂时没准备好，请稍后再试")
		return
	}
	digest := sha256.Sum256([]byte(token))
	share, err := s.store.LoadCandidateShare(hex.EncodeToString(digest[:]))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "这份分享已取消，或简历已删除")
		} else {
			writeError(w, http.StatusInternalServerError, "分享暂时没读出来，请稍后再试")
		}
		return
	}
	// 即使存在历史或错误的分享映射，匿名入口也不得公开内置示例资料。
	if share.CandidateID == builtinCandidateID {
		writeError(w, http.StatusNotFound, "这份分享已取消，或简历已删除")
		return
	}
	item, err := s.candidates.store.GetPositionCandidate(share.TenantID, share.CandidateID, "", share.UserEmail, true)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "这份分享已取消，或简历已删除")
		} else {
			writeError(w, http.StatusInternalServerError, "简历暂时没读出来，请稍后再试")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "candidate": publicSharedCandidate(item)})
}

// setCandidateShareHeaders 禁止缓存、搜索索引和向外部站点发送带令牌的来源地址。
func setCandidateShareHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

// publicSharedCandidate 仅选取求职资料，未知补充字段和内部工作记录永远不进入公开响应。
func publicSharedCandidate(item PositionCandidate) map[string]any {
	result := map[string]any{
		"candidate_name": item.CandidateName, "birth_ym": item.BirthYM, "phone": item.Phone, "email": item.Email,
		"work_region": item.WorkRegion, "work_years": item.WorkYears, "education_level": item.EducationLevel,
		"expected_position": item.ExpectedPosition, "expected_salary_min": item.ExpectedSalaryMin, "expected_salary_max": item.ExpectedSalaryMax,
		"personal_description": item.PersonalDescription, "basic_info": item.BasicInfo, "work_status": item.WorkStatus,
		"work_experiences": safeSlice(item.WorkExperiences), "project_experiences": safeSlice(item.ProjectExperiences),
		"educations": safeSlice(item.Educations), "certificates": safeSlice(item.Certificates), "honors": safeSlice(item.Honors),
	}
	for _, key := range []string{"gender", "age", "current_region", "expected_region", "current_position", "expected_salary_text", "current_salary_text"} {
		// 只接受已规范化的文字；历史异常JSON对象不能借同名字段把内部数据带到公开响应。
		if value := candidateString(item.LibraryFields, key); value != "" {
			result[key] = value
		}
	}
	if age, ok := item.LibraryFields["age"].(float64); ok && age > 0 && age < 120 {
		result["age"] = age
	} else if age, ok := item.LibraryFields["age"].(int); ok && age > 0 && age < 120 {
		result["age"] = age
	}
	if avatar := candidateString(item.LibraryFields, "avatar_data_url"); avatar != "" {
		result["avatar_data_url"] = avatar
	}
	return result
}
