// 本文件校验操作账号与绑定设备，再将有限的自动回复指令交给服务器编排模块。
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// runtimeProxy 不信任外部的身份头，使用已核验团队和用户标识转发命令。
func (s *AutoReplyService) runtimeProxy(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/auto-reply/runtime/")
	if action != "start" && action != "stop" && action != "status" && action != "next" && action != "complete" && action != "retry-reports" {
		writeAutoReplyError(w, 404, "NOT_FOUND", "未识别的流程指令")
		return
	}
	if r.Method != http.MethodPost && !(action == "status" && r.Method == http.MethodGet) {
		writeAutoReplyError(w, 405, "METHOD_NOT_ALLOWED", "请求方式未识别")
		return
	}
	needsExecution := action == "start" || action == "next" || action == "retry-reports"
	// 停止、查状态和交回执不新开执行能力；会员到期后也必须能结束旧任务并保存已发生的结果。
	access, ok := s.currentRequestContext(w, r, needsExecution, true)
	if !ok {
		return
	}
	if action == "start" {
		// 平台必须来自本人岗位，不能只相信客户端传入的平台名称。
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 96<<20))
		var input struct {
			PositionID string `json:"position_id"`
			PlatformID string `json:"platform_id"`
		}
		if err != nil || json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.PositionID) == "" {
			writeAutoReplyError(w, 400, "POSITION_REQUIRED", "开始前请先选择自动回复岗位")
			return
		}
		position, err := s.positions.PositionByID(access.Tenant.ID, access.Session.Email, input.PositionID, false)
		if err != nil {
			writeAutoReplyStoreError(w, err, "自动回复岗位暂时无法核对")
			return
		}
		if rejectAutoReplyRollout(w, position.PlatformID) {
			return
		}
		if input.PlatformID != position.PlatformID {
			writeAutoReplyError(w, 409, "PLATFORM_MISMATCH", "当前招聘页面和岗位的平台没对上，请核对后重试")
			return
		}
		if failure := validateStartAIBalance(s.aiWallet, access.Session.Email); failure != nil {
			writeAutoReplyError(w, failure.status, failure.code, failure.message)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	uid, err := s.store.activeTenantUserID(r.Context(), access.Tenant.ID, access.Session.Email)
	if err != nil {
		writeAutoReplyStoreError(w, err, "操作账号暂时无法核对")
		return
	}
	secret := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"))
	base := strings.TrimSpace(os.Getenv("GOODHR_AUTO_REPLY_RUNTIME_URL"))
	if base == "" {
		base = "http://127.0.0.1:38086"
	}
	dest, err := url.Parse(base)
	if secret == "" || err != nil || dest.Host == "" || (dest.Scheme != "http" && dest.Scheme != "https") {
		writeAutoReplyError(w, 503, "RUNTIME_UNAVAILABLE", "云端执行程序尚未配置好")
		return
	}
	dest.Path = strings.TrimRight(dest.Path, "/") + r.URL.Path
	dest.RawQuery = r.URL.RawQuery
	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, dest.String(), http.MaxBytesReader(w, r.Body, 96<<20))
	if err != nil {
		writeAutoReplyError(w, 502, "RUNTIME_UNAVAILABLE", "暂时无法连接云端执行程序")
		return
	}
	req.Header.Set("Authorization", r.Header.Get("Authorization"))
	req.Header.Set("X-GoodHR-Machine-ID", r.Header.Get("X-GoodHR-Machine-ID"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoodHR-Runtime-Service", secret)
	req.Header.Set("X-GoodHR-Runtime-Tenant", access.Tenant.ID)
	req.Header.Set("X-GoodHR-Runtime-User", uid)
	if s.runtime != nil {
		s.runtime.ServeHTTP(w, req)
		return
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		writeAutoReplyError(w, 502, "RUNTIME_UNAVAILABLE", "云端执行程序暂时未连接")
		return
	}
	defer resp.Body.Close()
	const max = 96 << 20
	var b bytes.Buffer
	if _, err = io.CopyN(&b, resp.Body, max+1); (err != nil && err != io.EOF) || b.Len() > max {
		writeAutoReplyError(w, 502, "RUNTIME_RESPONSE_TOO_LARGE", "执行程序返回的数据过多")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(b.Bytes())
}
