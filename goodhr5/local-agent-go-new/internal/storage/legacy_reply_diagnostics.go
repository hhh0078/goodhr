// Package storage 本文件在业务资料迁移成功后清理旧自动回复诊断正文，保留设备技术记录及已经确认的费用。
package storage

import (
	"context"
	"encoding/json"
	"strconv"
)

const legacyReplyLogCleanupVersion = "privacy:auto_reply_task_logs:v1"

// RedactLegacyReplyDiagnostics 在一笔事务内处理已能证明为自动回复的记录。历史日志无账号字段，不能拿来认领或上传。
func (s *Store) RedactLegacyReplyDiagnostics(ctx context.Context, redact func(TaskLog) (TaskLog, bool)) error {
	if done, err := s.GetSetting(ctx, legacyReplyLogCleanupVersion); err != nil || done != "" {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// 覆盖旧记录使用过的 SQLite 页内空间；仅在此次清理期间启用，结束后恢复原连接设定。
	var original int
	if err = conn.QueryRowContext(ctx, "PRAGMA secure_delete").Scan(&original); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA secure_delete=ON"); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "PRAGMA secure_delete="+strconv.Itoa(original)) }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	inserted, err := tx.ExecContext(ctx, "INSERT INTO local_settings(key,value,updated_at) VALUES (?,'1',strftime('%Y-%m-%dT%H:%M:%fZ','now')) ON CONFLICT(key) DO NOTHING", legacyReplyLogCleanupVersion)
	if err != nil {
		return err
	}
	n, err := inserted.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	// 空任务号的推荐报告账单是旧后台自动回复推荐的特有格式；还须验证同岗位任务和费用回执。
	const selectLogs = `SELECT l.id,l.flow,l.step,l.status,l.level,l.category,l.input_json,l.output_json,
    (t.task_id IS NULL AND l.flow='ai' AND l.step='ai_billing')
 FROM task_logs l LEFT JOIN task_runs t ON t.task_id=l.task_id WHERE l.id>? AND (
    t.task_type='auto_reply' OR (t.task_id IS NULL AND (
       l.flow='auto_reply' OR (l.flow='analysis' AND l.step GLOB 'analysis.auto_reply.*')
       OR (l.task_id='' AND l.position_id<>'' AND l.flow='ai' AND l.step='ai_billing'
           AND l.message GLOB 'AI 推荐报告，候选人“*”，*'
           AND EXISTS (SELECT 1 FROM task_runs a WHERE a.position_id=l.position_id AND a.task_type='auto_reply')))))
 ORDER BY l.id LIMIT 128`
	var after int64
	for {
		rows, err := tx.QueryContext(ctx, selectLogs, after)
		if err != nil {
			return err
		}
		batch := make([]TaskLog, 0, 128)
		read := 0
		for rows.Next() {
			var item TaskLog
			var input, output string
			var needsReceipt bool
			if err = rows.Scan(&item.ID, &item.Flow, &item.Step, &item.Status, &item.Level, &item.Category, &input, &output, &needsReceipt); err != nil {
				rows.Close()
				return err
			}
			item.Input, item.Output = json.RawMessage(input), json.RawMessage(output)
			after = item.ID
			read++
			safe, receipt := redact(item)
			if !needsReceipt || receipt {
				batch = append(batch, safe)
			}
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		for _, item := range batch {
			if _, err = tx.ExecContext(ctx, `UPDATE task_logs SET flow=?,step=?,status=?,level=?,category=?,message=?,error_message='',input_json=?,output_json=? WHERE id=?`,
				item.Flow, item.Step, item.Status, item.Level, item.Category, item.Message, string(item.Input), string(item.Output), item.ID); err != nil {
				return err
			}
		}
		if read < 128 {
			break
		}
	}
	// 通用任务的自由文本摘要和异常可能包含整句候选人来信，只保留此任务真实状态及短技术码。
	_, err = tx.ExecContext(ctx, `UPDATE task_runs SET
   current_step=CASE WHEN length(current_step) BETWEEN 1 AND 96 AND current_step GLOB '[a-zA-Z]*' AND current_step NOT GLOB '*[^a-zA-Z0-9_.:-]*' THEN current_step ELSE 'legacy_step' END,
   error_code=CASE WHEN length(error_code) BETWEEN 1 AND 96 AND error_code GLOB '[A-Z]*' AND error_code NOT GLOB '*[^A-Z0-9_]*' THEN error_code ELSE '' END,
   summary=CASE status WHEN 'failed' THEN '自动回复任务未完成' WHEN 'stopped' THEN '自动回复任务已停止' WHEN 'completed' THEN '自动回复任务已完成' ELSE '自动回复任务记录' END,
   status=CASE WHEN status IN ('running','failed','stopped','completed','pending','cancelled','interrupted') THEN status ELSE 'unknown' END,error_message=''
   WHERE task_type='auto_reply'`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
