// Package cloud 本文件处理已认证账号的云简历删除事件，级联清除本机自动回复资料和独占的受管附件。
package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"goodhr5/local-agent-go-new/internal/storage"
)

// PurgeLocalAutoReplyCandidate 按云端编号或本账号唯一且无冲突的平台来源清理，重复投递可继续完成文件回收。
func (c *Client) PurgeLocalAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, cloudCandidateID string, sourceHashes []string) error {
	cloudCandidateID = strings.TrimSpace(cloudCandidateID)
	if cloudCandidateID == "" || cloudCandidateID == BuiltinCandidateID {
		return fmt.Errorf("删除事件缺少云简历编号")
	}
	// 删除操作不能回落到凭证摘要账号，必须绑定已经通过云端核实的用户。
	if _, ok := c.localAutoOwners.Load(autoReplyDigest(credentials.Token)); !ok {
		return fmt.Errorf("清理简历前需要核对当前登录账号")
	}
	return c.purgeAutoReplyCandidate(ctx, credentials, cloudCandidateID, sourceHashes, "")
}

// purgeAutoReplyCandidate 也接受云端明确拒绝一次上传时的本地来源，避免首次上传尚无云映射留下孤立资料。
func (c *Client) purgeAutoReplyCandidate(ctx context.Context, credentials AgentCredentials, cloudID string, sources []string, rejectedLocalID string) error {
	eventKey := "cloud:" + cloudID
	if cloudID == "" {
		eventKey = "local:" + rejectedLocalID
	}
	err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		targets := map[string]bool{}
		if cloudID != "" {
			ids, err := tx.CloudLinksByCandidate(cloudID)
			if err != nil {
				return err
			}
			for _, id := range ids {
				targets[id] = true
			}
			if err = tx.BlockPurgedKey("cloud", cloudID); err != nil {
				return err
			}
		}
		if rejectedLocalID != "" {
			targets[rejectedLocalID] = true
		}
		matchedSource, err := addDeletedCandidateSources(tx, cloudID, sources, targets)
		if err != nil {
			return err
		}
		legacyChanged := false
		if err := tx.UpdateLegacyAutoReplySnapshot(func(raw []byte) ([]byte, error) {
			changed, err := c.purgeLegacySnapshot(tx, credentials, eventKey, cloudID, targets, raw)
			legacyChanged = changed != nil
			return changed, err
		}); err != nil {
			return err
		}
		if len(targets) == 0 && !matchedSource && !legacyChanged {
			return nil
		}
		for id := range targets {
			if err := tx.BlockPurgedKey("candidates", id); err != nil {
				return err
			}
		}
		for _, kind := range []string{"identities", "conversations", "runs", "recommendations", "reports", "attachments", "tools", "messages", "confirmations", "suggestions", "proposal_receipts", "sends", "outbox", "cloud_links", "candidates"} {
			needData := kind == "recommendations" || kind == "attachments"
			if !needData && kind != "identities" && kind != "conversations" && kind != "runs" && kind != "reports" {
				if err := tx.DeletePurgedChildren(kind); err != nil {
					return err
				}
				continue
			}
			rows, err := tx.RelatedPurgedRows(kind, needData)
			if err != nil {
				return err
			}
			for _, row := range rows {
				switch kind {
				case "identities", "conversations", "runs", "reports":
					if err = tx.BlockPurgedKey(kind, row.ID); err != nil {
						return err
					}
				case "recommendations":
					var job struct {
						InputHash string `json:"input_hash"`
					}
					if err = json.Unmarshal(row.Data, &job); err != nil {
						return err
					}
					if job.InputHash != "" {
						if err = tx.BlockPurgedKey("reports", job.InputHash); err != nil {
							return err
						}
					}
				case "attachments":
					var item localAttachment
					if err = json.Unmarshal(row.Data, &item); err != nil {
						return err
					}
					if item.RelativePath != "" {
						if err = tx.QueuePurgedAttachment(eventKey, item.RelativePath, item.SHA256); err != nil {
							return err
						}
					}
				}
			}
			if err = tx.DeletePurgedChildren(kind); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.cleanPurgedEventFiles(ctx, credentials, eventKey)
}

// cleanPurgedEventFiles 只回收同一删除事件的文件，单个被占用或替换的文件不阻塞其他简历的回执。
func (c *Client) cleanPurgedEventFiles(ctx context.Context, credentials AgentCredentials, eventKey string) error {
	return c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		return tx.CleanPurgedAttachments(eventKey, func(path, digest string) error { return c.removePrivateAutoReplyAttachment(credentials, path, digest) })
	})
}

// addDeletedCandidateSources 限定同一稳定来源、同一招聘账号，拒绝跨账号猜测及覆盖已指向其他云简历的本地映射。
func addDeletedCandidateSources(tx *storage.AutoReplyTx, cloudID string, sources []string, targets map[string]bool) (bool, error) {
	matched := false
	if len(sources) == 0 {
		return matched, nil
	}
	wanted := map[string]bool{}
	for _, s := range sources {
		wanted[s] = true
	}
	rows, err := tx.AutoReplyPurgeSources()
	if err != nil {
		return false, err
	}
	groups := map[string][]storage.AutoReplyPurgeSource{}
	for _, row := range rows {
		platform, source := strings.TrimSpace(row.Platform), strings.TrimSpace(row.PlatformCandidate)
		if platform == "" || source == "" || row.Account == "" {
			continue
		}
		key := autoReplyDigest(platform + "\x00" + source)
		if wanted[key] {
			groups[key] = append(groups[key], row)
		}
	}
	for hash, group := range groups {
		// 同一云事件只判定一次已发现的来源；先删去已映射账号后，原来有歧义的另一账号不能在重投时变成“唯一”。
		resolution := autoReplyDigest(cloudID + "|" + hash)
		seen, err := tx.HasPurgedKey("source-checked", resolution)
		if err != nil {
			return false, err
		}
		if seen {
			continue
		}
		if err = tx.BlockPurgedKey("source-checked", resolution); err != nil {
			return false, err
		}
		account := group[0].Account
		candidate := ""
		ambiguous := false
		for _, row := range group {
			if row.Account != account || candidate != "" && row.Candidate != "" && candidate != row.Candidate {
				ambiguous = true
				break
			}
			if row.Candidate != "" {
				candidate = row.Candidate
			}
		}
		if ambiguous {
			continue
		}
		if candidate != "" {
			link, exists, err := getAutoReplyEntity[localCloudLink](tx, "cloud_links", "candidate:"+candidate)
			if err != nil {
				return false, err
			}
			if exists && link.Value != "" && link.Value != cloudID {
				continue
			}
			targets[candidate] = true
		}
		matched = true
		for _, row := range group {
			if row.Identity != "" {
				if err := tx.BlockPurgedKey("identities", row.Identity); err != nil {
					return false, err
				}
			}
			if row.Conversation != "" {
				if err := tx.BlockPurgedKey("conversations", row.Conversation); err != nil {
					return false, err
				}
			}
		}
	}
	return matched, nil
}

// purgeLegacySnapshot 在保留其他账号数据、未知字段及迁移标记的同时，删除旧版设置中仍可能残留的同一候选人原文。
func (c *Client) purgeLegacySnapshot(tx *storage.AutoReplyTx, credentials AgentCredentials, eventKey, cloudID string, candidates map[string]bool, raw []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	var old localAutoReplyData
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	for id, value := range old.CloudCandidates {
		if cloudID != "" && value == cloudID {
			candidates[id] = true
		}
	}
	identities := map[string]bool{}
	conversations := map[string]bool{}
	runs := map[string]bool{}
	reports := map[string]bool{}
	removed := map[string]map[string]bool{}
	// mark 仅记录已证明属于该简历的旧快照条目，保持未识别字段原样。
	mark := func(kind, id string) {
		if id != "" {
			if removed[kind] == nil {
				removed[kind] = map[string]bool{}
			}
			removed[kind][id] = true
		}
	}
	for id := range candidates {
		mark("candidates", id)
		mark("cloud_candidates", id)
		mark("cloud_candidate_hashes", id)
	}
	// 旧版同步摘要的键是本地候选编号与岗位编号的组合，不能用候选编号直接删除。
	for key := range old.CloudCandidateHashes {
		id, _, _ := strings.Cut(key, "|")
		if candidates[id] {
			mark("cloud_candidate_hashes", key)
		}
	}
	for id, v := range old.Identities {
		if candidates[v.CandidateID] {
			identities[id] = true
			mark("identities", id)
		}
	}
	for id, v := range old.Conversations {
		if candidates[v.CandidateID] || identities[v.PlatformIdentityID] {
			conversations[id] = true
			mark("conversations", id)
			mark("messages", id)
		}
	}
	for id, v := range old.Confirmations {
		if candidates[v.CandidateID] || conversations[v.ConversationID] {
			mark("confirmations", id)
		}
	}
	for id, v := range old.Attachments {
		if candidates[v.CandidateID] || conversations[v.ConversationID] {
			mark("attachments", id)
			mark("attachment_paths", id)
			if path := old.AttachmentPaths[id]; path != "" {
				relative, err := filepath.Rel(c.localAutoDir, path)
				if err == nil && c.validPurgeAttachmentPath(credentials, relative, v.SHA256) {
					if err = tx.QueuePurgedAttachment(eventKey, relative, v.SHA256); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for id, v := range old.Runs {
		if candidates[v.CandidateID] || conversations[v.ConversationID] {
			runs[id] = true
			mark("runs", id)
		}
	}
	for id, v := range old.Tools {
		if runs[v.AIRunID] {
			mark("tools", id)
		}
	}
	for id, v := range old.Suggestions {
		if conversations[v.ConversationID] {
			mark("suggestions", id)
		}
	}
	for id, v := range old.Notifications {
		if conversations[v.Notification.ConversationID] {
			mark("notifications", id)
		}
	}
	for id, v := range old.Reports {
		var report AutoReplyRecommendationReport
		if json.Unmarshal(v, &report) == nil && (candidates[report.Candidate.ID] || cloudID != "" && report.Candidate.ID == cloudID) {
			reports[id] = true
			for _, group := range []string{"reports", "report_claims", "report_published", "report_attempts"} {
				mark(group, id)
			}
		}
	}
	for kind, group := range map[string]map[string]bool{"identities": identities, "conversations": conversations, "runs": runs, "reports": reports} {
		for id := range group {
			if err := tx.BlockPurgedKey(kind, id); err != nil {
				return nil, err
			}
		}
	}
	changed := false
	for kind, rows := range removed {
		if doc[kind] == nil {
			continue
		}
		var values map[string]json.RawMessage
		if err := json.Unmarshal(doc[kind], &values); err != nil {
			return nil, err
		}
		initial := len(values)
		for id := range rows {
			delete(values, id)
		}
		if len(values) != initial {
			encoded, err := json.Marshal(values)
			if err != nil {
				return nil, err
			}
			doc[kind] = encoded
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return json.Marshal(doc)
}

// validPurgeAttachmentPath 只接受写入器生成的本账号相对路径与完整SHA256文件名。
func (c *Client) validPurgeAttachmentPath(credentials AgentCredentials, path, digest string) bool {
	hash, err := hex.DecodeString(digest)
	if err != nil || len(hash) != sha256.Size || digest != strings.ToLower(digest) {
		return false
	}
	name := filepath.Base(path)
	return path == filepath.Join(autoReplyDigest(c.localAutoReplyKey(credentials)), "attachments", name) && strings.TrimSuffix(name, filepath.Ext(name)) == digest
}

// removePrivateAutoReplyAttachment 通过受限根目录核对路径、普通文件和内容，不接触用户下载目录及替换过的文件。三平台均使用 Go os.Root。
func (c *Client) removePrivateAutoReplyAttachment(credentials AgentCredentials, path, digest string) error {
	if !c.validPurgeAttachmentPath(credentials, path, digest) {
		return fmt.Errorf("私有简历附件的路径或摘要不匹配，请核对本地资料目录")
	}
	base, err := os.Lstat(c.localAutoDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !base.IsDir() || base.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("自动回复私有目录已被替换，已保留文件")
	}
	root, err := os.OpenRoot(c.localAutoDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(base, opened) {
		return fmt.Errorf("自动回复私有目录发生变化，已保留文件")
	}
	for _, entry := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path), path} {
		info, err := root.Lstat(entry)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("私有简历附件路径被替换，已保留文件")
		}
	}
	file, err := root.Open(path)
	if err != nil {
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > AutoReplyMaxAttachmentBytes {
		file.Close()
		return fmt.Errorf("私有简历附件类型或大小已变化，已保留文件")
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, io.LimitReader(file, AutoReplyMaxAttachmentBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	current, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(stat, current) || hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("私有简历附件已被替换，已保留文件")
	}
	err = root.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// retryPurgedEventFiles 独立恢复此前回收失败的各事件，即使收到云端首次上传回执前删除事件已被确认也能继续。
func (c *Client) retryPurgedEventFiles(ctx context.Context, credentials AgentCredentials) error {
	var keys []string
	err := c.autoReplyTransaction(ctx, credentials, func(tx *storage.AutoReplyTx) error {
		var err error
		keys, err = tx.PendingPurgedFileEvents()
		return err
	})
	if err != nil {
		return err
	}
	var result error
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return errors.Join(result, err)
		}
		result = errors.Join(result, c.cleanPurgedEventFiles(ctx, credentials, key))
	}
	return result
}
