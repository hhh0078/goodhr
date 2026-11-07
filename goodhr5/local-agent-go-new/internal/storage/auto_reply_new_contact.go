// Package storage 本文件按账号持久化删除时间界线和只读核实的恢复线索。
package storage

import (
	"database/sql"
	"errors"
)

// RememberContactDeletion 保存来源的最新删除事件，旧事件重放不能降低代次。
func (t *AutoReplyTx) RememberContactDeletion(hash string, sequence int64, cutoff string) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO auto_reply_deletion_boundaries VALUES(?,?,?,?)
 ON CONFLICT(owner_id,source_hash) DO UPDATE SET deletion_sequence=excluded.deletion_sequence,deleted_at=excluded.deleted_at
 WHERE excluded.deletion_sequence>auto_reply_deletion_boundaries.deletion_sequence OR (excluded.deletion_sequence=auto_reply_deletion_boundaries.deletion_sequence AND auto_reply_deletion_boundaries.deleted_at='')`, t.owner, hash, sequence, cutoff)
	return err
}

// ContactDeletion 只读取已核实平台来源的删除边界，不用姓名或手机号定位。
func (t *AutoReplyTx) ContactDeletion(hash string) (seq int64, cutoff string, err error) {
	err = t.tx.QueryRowContext(t.ctx, "SELECT deletion_sequence,deleted_at FROM auto_reply_deletion_boundaries WHERE owner_id=? AND source_hash=?", t.owner, hash).Scan(&seq, &cutoff)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return
}

// SetContactProbe 在点击清未读前保留路由，核实结束后通过空值清除。
func (t *AutoReplyTx) SetContactProbe(platform, account, thread, intent string) error {
	if intent == "" {
		_, err := t.tx.ExecContext(t.ctx, "DELETE FROM auto_reply_contact_probes WHERE owner_id=? AND platform_id=? AND account_id=? AND thread_id=?", t.owner, platform, account, thread)
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, "INSERT OR REPLACE INTO auto_reply_contact_probes VALUES(?,?,?,?,?)", t.owner, platform, account, thread, intent)
	return err
}

// ContactProbes 读取该招聘账号被点击后需要恢复的精确会话。
func (t *AutoReplyTx) ContactProbes(platform, account string) ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT intent_json FROM auto_reply_contact_probes WHERE owner_id=? AND platform_id=? AND account_id=? ORDER BY thread_id", t.owner, platform, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, rows.Err()
}

// RememberDeletionDocuments 记录正式附件内容摘要并重放本事件已有的锚，不扫描用户下载目录。
func (t *AutoReplyTx) RememberDeletionDocuments(cloudID string, hashes []string) ([]string, error) {
	for _, hash := range hashes {
		if _, err := t.tx.ExecContext(t.ctx, "INSERT OR IGNORE INTO auto_reply_deletion_documents VALUES(?,?,?)", t.owner, cloudID, hash); err != nil {
			return nil, err
		}
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT sha256 FROM auto_reply_deletion_documents WHERE owner_id=? AND event_key=? ORDER BY sha256", t.owner, cloudID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var h string
		if err = rows.Scan(&h); err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// HasContactDeletions 检查当前登录账号是否有已删除平台来源；没有删除记录时无需多一次页面核身。
func (t *AutoReplyTx) HasContactDeletions() (bool, error) {
	var found bool
	err := t.tx.QueryRowContext(t.ctx, "SELECT EXISTS(SELECT 1 FROM auto_reply_deletion_boundaries WHERE owner_id=?)", t.owner).Scan(&found)
	return found, err
}
