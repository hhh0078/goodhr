// Package cloud 本文件定义旧快照迁移模型与本地账号归属；正式业务按SQLite实体行访问。
package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"goodhr5/local-agent-go-new/internal/storage"
	"sort"
	"time"
)

// localAutoReplyData 按已认证账号隔离本地业务数据，登录凭证不进入持久化正文。
type localAutoReplyData struct {
	Identities           map[string]CandidatePlatformIdentity   `json:"identities"`
	Conversations        map[string]AutoReplyConversation       `json:"conversations"`
	Messages             map[string][]AutoReplyMessage          `json:"messages"`
	Candidates           map[string]AutoReplyStoredCandidate    `json:"candidates"`
	Confirmations        map[string]CandidateConfirmationItem   `json:"confirmations"`
	Attachments          map[string]StoredResumeAttachment      `json:"attachments"`
	AttachmentPaths      map[string]string                      `json:"attachment_paths"`
	Runs                 map[string]AutoReplyAIRun              `json:"runs"`
	Tools                map[string]AutoReplyToolCall           `json:"tools"`
	Suggestions          map[string]AutoReplyConfigSuggestion   `json:"suggestions"`
	Notifications        map[string]AutoReplyNotificationResult `json:"notifications"`
	CloudCandidates      map[string]string                      `json:"cloud_candidates"`
	Reports              map[string]json.RawMessage             `json:"reports"`
	CloudCandidateHashes map[string]string                      `json:"cloud_candidate_hashes"`
	ReportClaims         map[string]time.Time                   `json:"report_claims"`
	ReportAttempts       map[string]int                         `json:"report_attempts"`
	ReportPublished      map[string]bool                        `json:"report_published"`
}

// EnableLocalAutoReply 在程序组装时启用本机持久化，之后敏感自动回复请求不会发送云端。
func (c *Client) EnableLocalAutoReply(store *storage.Store, directory string) {
	c.localAutoStore, c.localAutoDir, c.localAutoConfigured = store, directory, true
}

// LocalAutoReplyEnabled 返回是否已组装正式本地数据存储。
func (c *Client) LocalAutoReplyEnabled() bool { return c != nil && c.localAutoStore != nil }

// autoReplyDigest 生成不含账号、消息或凭证明文的稳定本地键。
func autoReplyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// localAutoReplyKey 优先使用启动检查核实的账号，未登录测试仅使用凭证摘要隔离。
func (c *Client) localAutoReplyKey(credentials AgentCredentials) string {
	owner := autoReplyDigest(credentials.Token)
	if authenticated, ok := c.localAutoOwners.Load(owner); ok {
		owner = authenticated.(string)
	}
	return "auto_reply:v1:" + autoReplyDigest(owner)
}

// autoReplyTransaction 先按账号完成一次原子迁移，再执行仅涉及目标实体的短事务。
func (c *Client) autoReplyTransaction(ctx context.Context, credentials AgentCredentials, action func(*storage.AutoReplyTx) error) error {
	if !c.LocalAutoReplyEnabled() {
		return fmt.Errorf("本地自动回复存储尚未准备好")
	}
	if err := validateAutoReplyCredentials(credentials); err != nil {
		return err
	}
	owner := c.localAutoReplyKey(credentials)
	if _, ok := c.localAutoMigrated.Load(owner); !ok {
		c.localAutoMu.Lock()
		err := c.localAutoStore.MigrateAutoReplyOwner(ctx, owner, owner, func(tx *storage.AutoReplyTx, raw []byte) error { return c.importLocalAutoReply(tx, credentials, raw) })
		if err == nil {
			c.localAutoMigrated.Store(owner, true)
		}
		c.localAutoMu.Unlock()
		if err != nil {
			return err
		}
	}
	return c.localAutoStore.AutoReplyTransaction(ctx, owner, action)
}

// trimLocalAutoReplyAudits 按完整AI运行裁剪最近千条审计，工具记录不会引用已移除的运行。
func trimLocalAutoReplyAudits(data *localAutoReplyData) {
	ids := make([]string, 0, len(data.Runs))
	for id := range data.Runs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return data.Runs[ids[i]].StartedAt.Before(data.Runs[ids[j]].StartedAt) })
	for id, call := range data.Tools {
		if _, ok := data.Runs[call.AIRunID]; !ok {
			delete(data.Tools, id)
		}
	}
	for _, id := range ids {
		if len(data.Runs) <= 1000 && len(data.Tools) <= 1000 {
			break
		}
		delete(data.Runs, id)
		for key, call := range data.Tools {
			if call.AIRunID == id {
				delete(data.Tools, key)
			}
		}
	}
}
