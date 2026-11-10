// Package storage 本文件按当前账号统计和重置已耗尽的简历待办，不触碰消息、发送或简历资料。
package storage

import "time"

// CountBlockedResumeRetries 只统计当前账号已尝试三次且尚未完成的简历待办。
func (t *AutoReplyTx) CountBlockedResumeRetries() (count int, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM auto_reply_conversations
		WHERE owner_id=? AND json_type(payload_json,'$.resume_retry')='object'
		AND json_extract(payload_json,'$.resume_retry.attempts')>=3
		AND COALESCE(julianday(json_extract(payload_json,'$.resume_retry.lease_until')),0)<=julianday(?)`, t.owner, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&count)
	return
}

// ResetBlockedResumeRetries 由明确人工操作重置耗尽项；保留来源，仅修改次数、错误、到期时间和行版本。
func (t *AutoReplyTx) ResetBlockedResumeRetries() (int, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := t.tx.ExecContext(t.ctx, `UPDATE auto_reply_conversations
		SET payload_json=json_set(json_remove(payload_json,'$.resume_retry.lease_until'),
			'$.resume_retry.attempts',0, -- 已领取次数归零，允许人工批准的新一轮尝试。
			'$.resume_retry.last_error','', -- 清除上轮错误说明。
			'$.resume_retry.next_attempt_at',?, -- 立即恢复调度。
			'$.updated_at',?), -- 会话业务更新时间。
			revision=revision+1,updated_at=? -- 行版本与存储更新时间。
		WHERE owner_id=? AND json_type(payload_json,'$.resume_retry')='object'
		AND json_extract(payload_json,'$.resume_retry.attempts')>=3
		AND COALESCE(julianday(json_extract(payload_json,'$.resume_retry.lease_until')),0)<=julianday(?)`, now, now, now, t.owner, now)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}
