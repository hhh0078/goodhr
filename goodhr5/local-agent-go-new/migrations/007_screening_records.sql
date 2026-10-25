-- 文件作用说明：独立保存候选人两轮筛选完整快照及待传版本，不保存Token，不受简历结构化上传开关影响。
CREATE TABLE IF NOT EXISTS screening_records (
    attempt_id TEXT PRIMARY KEY, -- 每次筛选独立UUID，断网重传保持不变。
    owner_id TEXT NOT NULL, -- 云端已验证账号ID；不允许由其他账号领取。
    position_id TEXT NOT NULL, -- 本次筛选所属岗位。
    task_id TEXT NOT NULL, -- 本地任务编号。
    revision INTEGER NOT NULL, -- 最新完整快照版本。
    acked_revision INTEGER NOT NULL DEFAULT 0, -- 已确认上传的本地版本，旧回包不能清除更新。
    payload_json TEXT NOT NULL, -- 不含账号凭证的强类型完整快照。
    completed INTEGER NOT NULL DEFAULT 0, -- 两轮已完成或已明确未执行。
    retry_after INTEGER NOT NULL DEFAULT 0, -- 下次允许重传的UTC毫秒时间戳。
    created_at TEXT NOT NULL, -- 实际首次筛选时间。
    updated_at TEXT NOT NULL -- 最新本地修改时间。
);
CREATE INDEX IF NOT EXISTS screening_pending_owner ON screening_records(owner_id,retry_after,acked_revision,revision);
CREATE INDEX IF NOT EXISTS screening_task ON screening_records(task_id,completed);
