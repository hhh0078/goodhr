-- 文件作用：升级时让已被旧协议未知字段拒收的简历、接管提醒立即进入原有后台重试。
-- 只调度失败记录，不改原始载荷、幂等编号、尝试次数或租约；成功记录及其他网络错误不参与。
UPDATE auto_reply_outbox
SET payload_json = json_set(payload_json, '$.next_attempt_at', '0001-01-01T00:00:00Z'),
    revision = revision + 1
WHERE status = 'failed'
  AND json_extract(payload_json, '$.last_error') = '这次参数没读明白，请检查后再试'
  AND ((json_extract(payload_json, '$.kind') = 'candidate'
        AND json_type(payload_json, '$.payload.input.resume_source_message_id') = 'text')
    OR (json_extract(payload_json, '$.kind') = 'notice'
        AND json_type(payload_json, '$.payload.status') IS NOT NULL));
