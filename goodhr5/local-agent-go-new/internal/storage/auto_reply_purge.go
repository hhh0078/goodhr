// Package storage 本文件按当前账号阻止已删除简历的旧任务回写，并支持在同一事务清除相关实体及旧快照。
package storage

import (
	"database/sql"
	"errors"
	"strings"
)

// ErrAutoReplyCandidateDeleted 表示该实体或其上级已经永久删除。
var ErrAutoReplyCandidateDeleted = errors.New("这份简历已删除，已停止保存或同步旧任务")

// purgeParents 只接受明确的业务关联字段；从不按姓名、正文、手机号或任意 JSON 字符串搜索。
var purgeParents = map[string][]string{
	"identities":        {"candidates:$.candidate_id"},
	"conversations":     {"candidates:$.candidate_id", "identities:$.platform_identity_id"},
	"messages":          {"conversations:$.conversation_id"},
	"confirmations":     {"candidates:$.candidate_id", "conversations:$.conversation_id"},
	"attachments":       {"candidates:$.candidate_id", "conversations:$.conversation_id"},
	"runs":              {"candidates:$.candidate_id", "conversations:$.conversation_id"},
	"tools":             {"runs:$.ai_run_id"},
	"suggestions":       {"conversations:$.conversation_id"},
	"reports":           {"candidates:$.report.candidate.id", "cloud:$.report.candidate.id"},
	"recommendations":   {"candidates:$.candidate_id", "conversations:$.conversation_id", "reports:$.input_hash"},
	"proposal_receipts": {"conversations:$.conversation_id"},
	"outbox":            {"candidates:$.payload.local_candidate_id", "conversations:$.payload.conversation_id", "reports:$.payload.key"},
	"sends":             {"conversations:$.conversation_id"},
}

// purgeKeyExpressions 构造同一套稳定关联条件，供查找关联行和检查在途写入共同使用。
func purgeKeyExpressions(kind string) string {
	expressions := []string{"'" + kind + ":'||id"}
	for _, parent := range purgeParents[kind] {
		target, path, _ := strings.Cut(parent, ":")
		expressions = append(expressions, "'"+target+":'||json_extract(payload_json,'"+path+"')")
	}
	if kind == "cloud_links" {
		expressions = append(expressions, "CASE WHEN id LIKE 'candidate:%' THEN 'cloud:'||json_extract(payload_json,'$.value') END",
			"CASE WHEN id LIKE 'candidate:%' THEN 'candidates:'||substr(id,11) WHEN id LIKE 'hash:%' THEN 'candidates:'||substr(substr(id,6),1,instr(substr(id,6)||'|','|')-1) END")
	}
	return strings.Join(expressions, ",")
}

// BlockPurgedKey 给受影响的稳定根标记永久删除；重复事件不会产生新行。
func (t *AutoReplyTx) BlockPurgedKey(kind, id string) error {
	_, err := t.tx.ExecContext(t.ctx, "INSERT OR IGNORE INTO auto_reply_purge_keys(owner_id,entity_key) VALUES(?,?)", t.owner, kind+":"+id)
	t.hasPurgeKeys = true
	t.purgeKeysChecked = true
	return err
}

// HasPurgedKey 检查当前账号的精确删除标记或已判定过的平台来源，避免重复投递重新猜测身份。
func (t *AutoReplyTx) HasPurgedKey(kind, id string) (bool, error) {
	var found bool
	err := t.tx.QueryRowContext(t.ctx, "SELECT EXISTS(SELECT 1 FROM auto_reply_purge_keys WHERE owner_id=? AND entity_key=?)", t.owner, kind+":"+id).Scan(&found)
	return found, err
}

// checkPurgedWrite 在旧任务回写时检查同账号墓碑；没有删除事件的事务只检查一次。
func (t *AutoReplyTx) checkPurgedWrite(kind, id string, raw []byte) error {
	if !t.purgeKeysChecked {
		if err := t.tx.QueryRowContext(t.ctx, "SELECT EXISTS(SELECT 1 FROM auto_reply_purge_keys WHERE owner_id=?)", t.owner).Scan(&t.hasPurgeKeys); err != nil {
			return err
		}
		t.purgeKeysChecked = true
	}
	if !t.hasPurgeKeys {
		return nil
	}
	var blocked bool
	query := "WITH item(id,payload_json) AS (VALUES(?,?)) SELECT EXISTS(SELECT 1 FROM item, auto_reply_purge_keys WHERE owner_id=? AND entity_key IN (" + purgeKeyExpressions(kind) + "))"
	if err := t.tx.QueryRowContext(t.ctx, query, id, string(raw), t.owner).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		return ErrAutoReplyCandidateDeleted
	}
	return nil
}

// RelatedPurgedRows 返回本次事务能明确关联到已删根的业务行；大段正文仅在需要回收关系或文件时读取。
func (t *AutoReplyTx) RelatedPurgedRows(kind string, includeData bool) ([]AutoReplyRow, error) {
	table, err := autoReplyTable(kind)
	if err != nil {
		return nil, err
	}
	projection := "id,NULL"
	if includeData {
		projection = "id,payload_json"
	}
	query := "SELECT " + projection + " FROM " + table + " WHERE owner_id=? AND EXISTS (SELECT 1 FROM auto_reply_purge_keys k WHERE k.owner_id=? AND k.entity_key IN (" + purgeKeyExpressions(kind) + "))"
	rows, err := t.tx.QueryContext(t.ctx, query, t.owner, t.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AutoReplyRow
	for rows.Next() {
		var row AutoReplyRow
		var raw []byte
		if err = rows.Scan(&row.ID, &raw); err != nil {
			return nil, err
		}
		row.Data = raw
		result = append(result, row)
	}
	return result, rows.Err()
}

// DeletePurgedChildren 用一条删除语句回收同账号已证实的关联行，长对话不需要逐条读入消息正文。
func (t *AutoReplyTx) DeletePurgedChildren(kind string) error {
	table, err := autoReplyTable(kind)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "DELETE FROM "+table+" WHERE owner_id=? AND EXISTS (SELECT 1 FROM auto_reply_purge_keys k WHERE k.owner_id=? AND k.entity_key IN ("+purgeKeyExpressions(kind)+"))", t.owner, t.owner)
	return err
}

// UpdateLegacyAutoReplySnapshot 在实体清除事务内同步清理保留的旧账号快照，保留迁移标记与其他候选人资料。
func (t *AutoReplyTx) UpdateLegacyAutoReplySnapshot(clean func([]byte) ([]byte, error)) error {
	var raw []byte
	err := t.tx.QueryRowContext(t.ctx, "SELECT value FROM local_settings WHERE key=?", t.owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil || len(raw) == 0 {
		return err
	}
	updated, err := clean(raw)
	if err != nil {
		return err
	}
	if updated == nil {
		return nil
	}
	_, err = t.tx.ExecContext(t.ctx, "UPDATE local_settings SET value=? WHERE key=?", string(updated), t.owner)
	return err
}

// QueuePurgedAttachment 将受管文件的物理删除意图与业务删除一起提交。
func (t *AutoReplyTx) QueuePurgedAttachment(eventKey, path, digest string) error {
	_, err := t.tx.ExecContext(t.ctx, "INSERT OR IGNORE INTO auto_reply_purge_files(owner_id,event_key,path,sha256) VALUES(?,?,?,?)", t.owner, eventKey, path, digest)
	return err
}

// CleanPurgedAttachments 在写事务内复核引用和执行受管文件回收，失败或进程退出会保留意图供重试。
func (t *AutoReplyTx) CleanPurgedAttachments(eventKey string, remove func(path, digest string) error) error {
	if _, err := t.tx.ExecContext(t.ctx, "DELETE FROM auto_reply_purge_files WHERE owner_id=? AND 0", t.owner); err != nil {
		return err
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT path,sha256 FROM auto_reply_purge_files WHERE owner_id=? AND event_key=?", t.owner, eventKey)
	if err != nil {
		return err
	}
	type file struct{ path, digest string }
	var pending []file
	for rows.Next() {
		var f file
		if err = rows.Scan(&f.path, &f.digest); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, f)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, f := range pending {
		var used bool
		if err = t.tx.QueryRowContext(t.ctx, "SELECT EXISTS(SELECT 1 FROM auto_reply_attachments WHERE json_extract(payload_json,'$.relative_path')=?)", f.path).Scan(&used); err != nil {
			return err
		}
		if !used {
			if err = remove(f.path, f.digest); err != nil {
				return err
			}
		}
		if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM auto_reply_purge_files WHERE owner_id=? AND event_key=? AND path=?", t.owner, eventKey, f.path); err != nil {
			return err
		}
	}
	return nil
}

// CloudLinksByCandidate 返回当前账号下同一云简历的所有本地关联；相同姓名或手机号不参与匹配。
func (t *AutoReplyTx) CloudLinksByCandidate(cloudID string) ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT substr(id,11) FROM auto_reply_cloud_links WHERE owner_id=? AND id LIKE 'candidate:%' AND json_extract(payload_json,'$.value')=?", t.owner, cloudID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PendingPurgedFileEvents 枚举当前账号尚未收尾的文件事件，便于迟到上传回执即使没有下一次云端投递也能恢复。
func (t *AutoReplyTx) PendingPurgedFileEvents() ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT DISTINCT event_key FROM auto_reply_purge_files WHERE owner_id=?", t.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// AutoReplyPurgeSource 仅包含核对来源和实体归属所需的稳定编号，不读取候选人的简历或聊天内容。
type AutoReplyPurgeSource struct{ Platform, Account, PlatformCandidate, Candidate, Identity, Conversation string }

// AutoReplyPurgeSources 收集同账号现有身份、待采集会话及未完成上传的明确平台来源；未收到云ID时也可核对。
func (t *AutoReplyTx) AutoReplyPurgeSources() ([]AutoReplyPurgeSource, error) {
	rows, err := t.tx.QueryContext(t.ctx, `
        SELECT platform_id,account_id,platform_candidate_id,candidate_id,id,'' FROM auto_reply_identities WHERE owner_id=?
        UNION ALL
        SELECT platform_id,account_id,json_extract(payload_json,'$.capture_intent.platform_candidate_id'),candidate_id,identity_id,id FROM auto_reply_conversations
        WHERE owner_id=? AND json_extract(payload_json,'$.capture_intent.platform_candidate_id') IS NOT NULL
        UNION ALL
        SELECT json_extract(payload_json,'$.payload.input.platform_id'),json_extract(payload_json,'$.payload.input.platform_account_id'),
               json_extract(payload_json,'$.payload.input.platform_candidate_id'),json_extract(payload_json,'$.payload.local_candidate_id'),'','' FROM auto_reply_outbox
        WHERE owner_id=? AND json_extract(payload_json,'$.kind')='candidate'`, t.owner, t.owner, t.owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AutoReplyPurgeSource
	for rows.Next() {
		var values [6]sql.NullString
		if err = rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5]); err != nil {
			return nil, err
		}
		result = append(result, AutoReplyPurgeSource{values[0].String, values[1].String, values[2].String, values[3].String, values[4].String, values[5].String})
	}
	return result, rows.Err()
}
