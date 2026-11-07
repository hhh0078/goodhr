-- 文件作用：升级时恢复被旧 AI 自建必要条件阻断的推荐资格检查，不变更候选人历史、消息或发送记录。
-- 每个会话只恢复最新候选消息的一份已完成收据，已经登记过该轮推荐的会话仍沿用原有任务。
-- 后台按收据所属账号重新读取岗位、候选人和证据，通过当前资格检查后才可生成报告。
WITH affected AS (
    SELECT MAX(r.sequence) AS sequence
    FROM auto_reply_proposal_receipts r
    JOIN auto_reply_conversations c
      ON c.owner_id = r.owner_id AND c.id = json_extract(r.payload_json, '$.conversation_id')
    WHERE r.status = 'completed'
      AND json_extract(r.payload_json, '$.based_on_message_key') = json_extract(c.payload_json, '$.last_candidate_message_key')
      AND EXISTS (
          SELECT 1 FROM auto_reply_confirmations i
          WHERE i.owner_id = c.owner_id AND i.candidate_id = c.candidate_id AND i.position_id = c.position_id
            AND i.source_type = 'ai' AND COALESCE(i.condition_id, '') = ''
            AND json_extract(i.payload_json, '$.archived_at') IS NULL
            AND json_extract(i.payload_json, '$.item_type') IN ('required', 'confirm')
            AND json_extract(i.payload_json, '$.status') IN ('pending', 'unmatched')
      )
      AND NOT EXISTS (
          SELECT 1 FROM auto_reply_recommendations j
          WHERE j.owner_id = c.owner_id AND json_extract(j.payload_json, '$.conversation_id') = c.id
            AND json_extract(j.payload_json, '$.based_on_message_key') = json_extract(r.payload_json, '$.based_on_message_key')
            AND json_extract(j.payload_json, '$.config_version') = json_extract(r.payload_json, '$.config_version')
      )
    GROUP BY c.owner_id, c.id
)
UPDATE auto_reply_proposal_receipts
SET payload_json = json_set(payload_json, '$.status', 'pending', '$.attempts', 0,
                            '$.last_error', '', '$.next_attempt_at', '0001-01-01T00:00:00Z'),
    revision = revision + 1
WHERE sequence IN (SELECT sequence FROM affected);
