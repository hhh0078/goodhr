// 本文件校验操作账号与绑定设备，再将有限的自动回复指令转给服务器的专用执行进程。
package httpapi

import (
	"bytes"
	"context"
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
	if needsExecution && rejectAutoReplyRollout(w) {
		return
	}
	// 停止、查状态和交回执不新开执行能力；会员到期后也必须能结束旧任务并保存已发生的结果。
	access, ok := s.currentRequestContext(w, r, needsExecution, true)
	if !ok {
		return
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
