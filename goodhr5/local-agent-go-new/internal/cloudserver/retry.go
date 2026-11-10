// Package cloudserver 本文件在服务器恢复已阻塞的推荐及简历待办，绝不重新领取聊天发送动作。
package cloudserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path/filepath"
	"time"

	"goodhr5/local-agent-go-new/internal/flow/auto_reply"
	"goodhr5/local-agent-go-new/internal/integration/ai"
	"goodhr5/local-agent-go-new/internal/integration/cloud"
	"goodhr5/local-agent-go-new/internal/storage"
)

// retryJobs 只重置当前已认证账号的报告、简历待办，已有发送凭据总是保留。
func (s *Service) retryJobs(w http.ResponseWriter, ctx context.Context, scope runScope, token string) {
	c := cloud.New(s.host)
	session, err := c.ValidateSession(ctx, token)
	if err != nil || session.UserID != scope.user || session.SessionKind == "browser_extension" {
		output(w, 403, map[string]any{"ok": false, "error": "登录状态已变更"})
		return
	}
	store, err := storage.OpenAutoReplyScratch(newStateAuthority(s.host, s.secret, scope.tenant, scope.user))
	if err != nil {
		output(w, 503, map[string]any{"ok": false, "error": "服务器待办暂不可用"})
		return
	}
	hash := sha256.Sum256([]byte(scope.tenant + "\x00" + scope.user))
	c.EnableLocalAutoReply(store, filepath.Join(s.files, hex.EncodeToString(hash[:])))
	cred := cloud.AgentCredentials{Token: token, MachineID: scope.machine}
	count, err := c.RetryBlockedAutoReplyRecommendations(ctx, cred)
	retries := 0
	if err == nil {
		retries, err = c.ResetAutoReplyResumeRetries(ctx, cred)
	}
	if err != nil {
		store.Close()
		output(w, 503, map[string]any{"ok": false, "error": "服务器未能确认待办调整，请稍后重试"})
		return
	}
	s.mu.Lock()
	if count > 0 && !s.shuttingDown {
		s.done.Add(1)
		go func() {
			defer s.done.Done()
			defer store.Close()
			work, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			responder := auto_reply.AIResponder{AI: ai.New(), Cloud: c}
			_ = responder.ProcessPendingRecommendations(work, cred)
		}()
	} else {
		store.Close()
	}
	s.mu.Unlock()
	output(w, 200, map[string]any{"ok": true, "retried_count": count + retries, "report_count": count, "resume_count": retries})
}
