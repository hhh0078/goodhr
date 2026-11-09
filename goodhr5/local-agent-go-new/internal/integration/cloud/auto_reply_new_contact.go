// Package cloud 本文件核对已删除来源的新入站，按删除代次建立独立本地会话和附件上传依据。
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/storage"
)

// AutoReplyDeletionBoundary 将本机只读核实限定在当前平台来源的最近一次删除之后。
type AutoReplyDeletionBoundary struct {
	SealedAfter time.Time `json:"sealed_after,omitzero"`
	Sequence    int64     `json:"sequence"`
	DeletedAt   time.Time `json:"deleted_at"`
	SourceHash  string    `json:"source_hash"`
}

// HistoryCutoff 取浏览器实际允许读取的较晚边界，保持旧删除事件的原始编号和时间。
func (b AutoReplyDeletionBoundary) HistoryCutoff() time.Time {
	if b.SealedAfter.After(b.DeletedAt) {
		return b.SealedAfter
	}
	return b.DeletedAt
}

// generationSuffix 为已封印的旧代保留原墓碑，只用明确更晚的观察边界派生新的实体编号。
func (b AutoReplyDeletionBoundary) generationSuffix() string {
	suffix := "|post-delete|" + strconv.FormatInt(b.Sequence, 10)
	if !b.SealedAfter.IsZero() {
		suffix += "|observed-after-seal|" + b.HistoryCutoff().Format(time.RFC3339Nano)
	}
	return suffix
}

// AutoReplyReopenProof 仅上传两条已验证消息的摘要和平台时间，不传正文或会话历史。
type AutoReplyReopenProof struct {
	ConversationID        string    `json:"conversation_id,omitempty"`
	LocalCandidateID      string    `json:"local_candidate_id,omitempty"`
	DeletionSequence      int64     `json:"deletion_sequence"`
	InboundMessageHash    string    `json:"inbound_message_hash"`
	InboundAt             time.Time `json:"inbound_at"`
	AttachmentMessageHash string    `json:"attachment_message_hash"`
	AttachmentAt          time.Time `json:"attachment_at"`
}

// ApplyAutoReplyCandidateDeletion 在同一事务收下最新来源边界及删除保护，文件回收可以独立重试。
func (c *Client) ApplyAutoReplyCandidateDeletion(ctx context.Context, cred AgentCredentials, event CandidateDeletion) error {
	if event.CandidateID == "" || event.CandidateID == BuiltinCandidateID {
		return fmt.Errorf("删除事件缺少云简历编号")
	}
	if _, ok := c.localAutoOwners.Load(autoReplyDigest(cred.Token)); !ok {
		return fmt.Errorf("清理简历前需要核对当前登录账号")
	}
	return c.purgeAutoReplyCandidate(ctx, cred, event.CandidateID, event.SourceHashes, "", event)
}

// originalContactID 返回原平台线程在本机使用的稳定编号，新代次也保留相同的真实线程字段。
func originalContactID(q AutoReplyCandidateLookup) string {
	return "conversation-" + autoReplyDigest(q.PlatformID+"|"+q.PlatformAccountID+"|"+q.PlatformThreadID)
}

// readContactBoundary 从可信来源查删除序号；只有旧线程墓碑时暂时只允许只读核身，不能填猜测的截止时间。
func readContactBoundary(tx *storage.AutoReplyTx, q AutoReplyCandidateLookup) (*AutoReplyDeletionBoundary, error) {
	if q.PlatformID == "" || q.PlatformAccountID == "" || q.PlatformThreadID == "" {
		return nil, fmt.Errorf("核实删除边界缺少平台、账号或会话编号")
	}
	var hash string
	if q.PlatformCandidateID != "" {
		hash = autoReplyDigest(q.PlatformID + "\x00" + q.PlatformCandidateID)
	} else {
		rows, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", autoReplyFilter("platform_id", q.PlatformID), autoReplyFilter("account_id", q.PlatformAccountID), autoReplyFilter("thread_id", q.PlatformThreadID))
		if err != nil {
			return nil, err
		}
		if len(rows) > 1 {
			return nil, fmt.Errorf("同一账号的会话身份不唯一")
		}
		if len(rows) == 1 && rows[0].DeletionBoundary != nil {
			hash = rows[0].DeletionBoundary.SourceHash
		}
	}
	if hash != "" {
		seq, stamp, err := tx.ContactDeletion(hash)
		if err != nil {
			return nil, err
		}
		if seq > 0 {
			b := &AutoReplyDeletionBoundary{Sequence: seq, SourceHash: hash}
			if stamp != "" {
				b.DeletedAt, err = time.Parse(time.RFC3339Nano, stamp)
			}
			if err != nil || b.DeletedAt.IsZero() {
				return b, err
			}
			id := "conversation-" + autoReplyDigest(originalContactID(q)+b.generationSuffix())
			sealed, err := tx.HasPurgedKey("conversations", id)
			if err != nil || !sealed {
				return b, err
			}
			// 老版本曾把发布冲突错误地当作永久删除。保持封印，以首次重新观察后的新消息和新附件开启沟通。
			marker := autoReplyDigest("sealed-conversation-observation|" + id + "|" + hash)
			_, observed, err := tx.ContactDeletion(marker)
			if err != nil {
				return nil, err
			}
			if observed == "" {
				b.SealedAfter = time.Now().UTC()
				err = tx.RememberContactDeletion(marker, seq, b.SealedAfter.Format(time.RFC3339Nano))
			} else {
				b.SealedAfter, err = time.Parse(time.RFC3339Nano, observed)
			}
			return b, err
		}
	}
	blocked, err := tx.HasPurgedKey("conversations", originalContactID(q))
	if err != nil {
		return nil, err
	}
	if blocked {
		return &AutoReplyDeletionBoundary{}, nil
	}
	return nil, nil
}

// AutoReplyDeletionBoundary 返回当前已认证账号的平台来源边界，平台未进入本地存储时保持旧调用语义。
func (c *Client) AutoReplyDeletionBoundary(ctx context.Context, cred AgentCredentials, q AutoReplyCandidateLookup) (b *AutoReplyDeletionBoundary, err error) {
	if !c.LocalAutoReplyEnabled() {
		return nil, nil
	}
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var lookupErr error
		b, lookupErr = readContactBoundary(tx, q)
		return lookupErr
	})
	return
}

// BeginAutoReplyContactProbe 在核实未读的动作清除红点之前留下精确路由，未确认新消息前不保存正文或简历。
func (c *Client) BeginAutoReplyContactProbe(ctx context.Context, cred AgentCredentials, q AutoReplyCandidateLookup, intent AutoReplyCaptureIntent) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		b, err := readContactBoundary(tx, q)
		if err != nil {
			return err
		}
		if b == nil {
			any, err := tx.HasContactDeletions()
			if err != nil {
				return err
			}
			if !any || q.PlatformCandidateID != "" {
				return fmt.Errorf("该会话没有重新联系的删除边界")
			}
		}
		if intent.PlatformThreadID != q.PlatformThreadID || strings.TrimSpace(intent.CandidateName) == "" {
			return fmt.Errorf("会话恢复线索不完整")
		}
		raw, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		return tx.SetContactProbe(q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, string(raw))
	})
}

// FinishAutoReplyContactProbe 核实没有可用新留言时结束只读线索，保留原有删除标记。
func (c *Client) FinishAutoReplyContactProbe(ctx context.Context, cred AgentCredentials, q AutoReplyCandidateLookup) error {
	return c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		return tx.SetContactProbe(q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, "")
	})
}

// ActivateAutoReplyContact 在同一事务内复核最新边界、创建全新身份并保存第一条入站；重复操作恢复同代记录。
func (c *Client) ActivateAutoReplyContact(ctx context.Context, cred AgentCredentials, q AutoReplyCandidateLookup, b AutoReplyDeletionBoundary, first AutoReplyMessage, intent AutoReplyCaptureIntent) (conv AutoReplyConversation, err error) {
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		current, err := readContactBoundary(tx, q)
		if err != nil {
			return err
		}
		key := localAutoReplyMessageKey(first)
		if current == nil || b.Sequence <= 0 || b.DeletedAt.IsZero() || current.Sequence != b.Sequence || current.SourceHash != b.SourceHash || !current.DeletedAt.Equal(b.DeletedAt) || !current.SealedAfter.Equal(b.SealedAfter) {
			return storage.ErrAutoReplyCandidateDeleted
		}
		if q.PlatformCandidateID == "" || intent.PlatformCandidateID != q.PlatformCandidateID || intent.PlatformThreadID != q.PlatformThreadID || strings.TrimSpace(intent.CandidateName) == "" {
			return storage.ErrAutoReplyCandidateDeleted
		}
		if first.Direction != "candidate" || key == "" || first.PlatformSentAt == nil || !first.PlatformSentAt.After(b.HistoryCutoff()) || first.PlatformSentAt.After(time.Now()) {
			return storage.ErrAutoReplyCandidateDeleted
		}
		suffix := b.generationSuffix()
		id := "conversation-" + autoReplyDigest(originalContactID(q)+suffix)
		old, exists, err := getAutoReplyEntity[AutoReplyConversation](tx, "conversations", id)
		if err != nil {
			return err
		}
		if exists {
			conv = old
			return tx.SetContactProbe(q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, "")
		}
		found, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", autoReplyFilter("platform_id", q.PlatformID), autoReplyFilter("account_id", q.PlatformAccountID), autoReplyFilter("thread_id", q.PlatformThreadID))
		if err != nil {
			return err
		}
		if len(found) > 0 {
			return fmt.Errorf("会话已有其他归属，暂不建立新联系")
		}
		candidateID, err := newLocalAutoReplyCandidateID()
		if err != nil {
			return err
		}
		stamp := time.Now().UTC()
		identity := CandidatePlatformIdentity{ID: "identity-" + autoReplyDigest(q.PlatformID+"|"+q.PlatformAccountID+"|"+q.PlatformCandidateID+suffix), CandidateID: candidateID, PlatformID: q.PlatformID, PlatformAccountID: q.PlatformAccountID, PlatformCandidateID: q.PlatformCandidateID, CandidateName: intent.CandidateName, FirstSeenAt: stamp, LastSeenAt: stamp}
		conv = AutoReplyConversation{ID: id, PlatformIdentityID: identity.ID, CandidateID: candidateID, PlatformID: q.PlatformID, PlatformAccountID: q.PlatformAccountID, PlatformThreadID: q.PlatformThreadID, CandidateName: intent.CandidateName, CaptureIntent: &intent, DeletionBoundary: current, ReopenInboundHash: autoReplyDigest(key), ReopenInboundAt: first.PlatformSentAt, CreatedAt: stamp, UpdatedAt: stamp}
		if err := tx.Put("candidates", candidateID, AutoReplyStoredCandidate{ID: candidateID, CandidateName: intent.CandidateName}); err != nil {
			return err
		}
		if err := tx.Put("identities", identity.ID, identity); err != nil {
			return err
		}
		if err := tx.Put("conversations", id, conv); err != nil {
			return err
		}
		if _, _, err := syncLocalAutoReplyMessages(tx, AutoReplyMessageSyncRequest{ConversationID: id, Messages: []AutoReplyMessage{first}}); err != nil {
			return err
		}
		conv.LastSyncedMessageKey, conv.LastCandidateMessageKey = key, key
		return tx.SetContactProbe(q.PlatformID, q.PlatformAccountID, q.PlatformThreadID, "")
	})
	return
}

// contactAttachmentProof 验证附件为本代已同步的候选人新发卡片，并生成可供云端核验的最少字段。
func contactAttachmentProof(tx *storage.AutoReplyTx, conv AutoReplyConversation, key string) (*AutoReplyReopenProof, error) {
	if conv.DeletionBoundary == nil {
		return nil, nil
	}
	b := conv.DeletionBoundary
	if b.DeletedAt.IsZero() || conv.ReopenInboundAt == nil || !conv.ReopenInboundAt.After(b.HistoryCutoff()) || conv.ReopenInboundHash == "" || key == "" {
		return nil, errors.New("重新建档需要候选人在删除后新发的附件")
	}
	row, found, err := tx.MessageByKey(conv.ID, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("尚未核实新附件来源")
	}
	var m AutoReplyMessage
	if err = json.Unmarshal(row.Data, &m); err != nil {
		return nil, err
	}
	if m.Direction != "candidate" || m.MessageType != "resume" || m.PlatformSentAt == nil || !m.PlatformSentAt.After(b.HistoryCutoff()) || m.PlatformSentAt.After(time.Now()) {
		return nil, fmt.Errorf("该附件不属于删除后的新入站")
	}
	return &AutoReplyReopenProof{DeletionSequence: b.Sequence, InboundMessageHash: conv.ReopenInboundHash, InboundAt: *conv.ReopenInboundAt, AttachmentMessageHash: autoReplyDigest(key), AttachmentAt: *m.PlatformSentAt}, nil
}

// candidateContact 查真实本地候选编号的当前代次，避免仅凭平台编号把旧回包升级为新联系人。
func candidateContact(tx *storage.AutoReplyTx, candidateID string) (AutoReplyConversation, error) {
	if candidateID == "" {
		return AutoReplyConversation{}, nil
	}
	records, err := listAutoReplyEntities[AutoReplyConversation](tx, "conversations", autoReplyFilter("candidate_id", candidateID))
	if err != nil {
		return AutoReplyConversation{}, err
	}
	var result AutoReplyConversation
	for _, item := range records {
		if item.DeletionBoundary != nil {
			if result.ID != "" {
				return result, fmt.Errorf("简历对应多个重新联系的会话")
			}
			result = item
		}
	}
	return result, nil
}

// HasAutoReplyDeletionBoundaries 判断未带稳定候选编号的列表是否需要先做只读核身，以保护新设备首次采集。
func (c *Client) HasAutoReplyDeletionBoundaries(ctx context.Context, cred AgentCredentials) (found bool, err error) {
	if !c.LocalAutoReplyEnabled() {
		return false, nil
	}
	err = c.autoReplyTransaction(ctx, cred, func(tx *storage.AutoReplyTx) error {
		var lookupErr error
		found, lookupErr = tx.HasContactDeletions()
		return lookupErr
	})
	return
}

// storedContactAttachmentProof 确认新候选附件已实际存入私有目录，再接受从该附件得出的结构化资料。
func storedContactAttachmentProof(tx *storage.AutoReplyTx, conv AutoReplyConversation, key string) (*AutoReplyReopenProof, error) {
	proof, err := contactAttachmentProof(tx, conv, key)
	if err != nil || proof == nil {
		return proof, err
	}
	row, found, err := tx.MessageByKey(conv.ID, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("新附件消息不存在")
	}
	files, err := listAutoReplyEntities[localAttachment](tx, "attachments", autoReplyFilter("conversation_id", conv.ID))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.SourceMessageID == row.ID && file.RelativePath != "" && file.SHA256 != "" {
			return proof, nil
		}
	}
	return nil, fmt.Errorf("尚未取得本次新发附件的受管文件")
}
