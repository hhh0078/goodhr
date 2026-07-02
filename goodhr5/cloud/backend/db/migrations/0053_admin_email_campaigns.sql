-- 本迁移新增超管邮件批次和收件人记录，用于批量邮件、发送进度和打开追踪。
CREATE TABLE IF NOT EXISTS email_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject TEXT NOT NULL DEFAULT '',
    target_summary TEXT NOT NULL DEFAULT '',
    source_key TEXT NOT NULL DEFAULT '',
    created_by_email TEXT NOT NULL DEFAULT '',
    total_count INTEGER NOT NULL DEFAULT 0,
    sent_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    opened_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_email_batches_source_key
ON email_batches (source_key)
WHERE source_key <> '';

CREATE TABLE IF NOT EXISTS email_recipients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES email_batches(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    opened BOOLEAN NOT NULL DEFAULT false,
    opened_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    UNIQUE (batch_id, email)
);

CREATE INDEX IF NOT EXISTS idx_email_recipients_batch_id ON email_recipients (batch_id);
CREATE INDEX IF NOT EXISTS idx_email_recipients_opened ON email_recipients (opened);

COMMENT ON TABLE email_batches IS '超管邮件发送批次表，只保存标题、对象摘要和统计，不保存正文';
COMMENT ON COLUMN email_batches.subject IS '邮件标题';
COMMENT ON COLUMN email_batches.target_summary IS '收件对象摘要';
COMMENT ON COLUMN email_batches.source_key IS '自动任务幂等键，手动发送为空';
COMMENT ON COLUMN email_batches.created_by_email IS '创建邮件批次的管理员邮箱';
COMMENT ON COLUMN email_batches.total_count IS '收件人总数';
COMMENT ON COLUMN email_batches.sent_count IS '发送成功数量';
COMMENT ON COLUMN email_batches.failed_count IS '发送失败数量';
COMMENT ON COLUMN email_batches.opened_count IS '已打开追踪图片的数量';
COMMENT ON COLUMN email_batches.finished_at IS '发送完成时间';

COMMENT ON TABLE email_recipients IS '邮件收件人发送记录表，不保存邮件正文';
COMMENT ON COLUMN email_recipients.batch_id IS '所属邮件批次ID';
COMMENT ON COLUMN email_recipients.email IS '收件人邮箱';
COMMENT ON COLUMN email_recipients.status IS '发送状态：pending/sent/failed';
COMMENT ON COLUMN email_recipients.error_message IS '发送失败原因';
COMMENT ON COLUMN email_recipients.opened IS '是否加载过邮件追踪图片';
COMMENT ON COLUMN email_recipients.opened_at IS '首次打开追踪图片时间';
COMMENT ON COLUMN email_recipients.sent_at IS '发送成功时间';

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
    'system.email_recovery',
    '{
        "enabled": true,
        "hour": 9,
        "wechat": "a1224299352",
        "templates": {
            "local_agent": {
                "subject": "GoodHR 本地程序还差一步",
                "html": "<p>我小声提醒一下，本地程序没绑定，任务暂时跑不起来。</p><p>你可以回到后台，先把本地程序启动并绑定好。</p>"
            },
            "ai_config": {
                "subject": "GoodHR AI 配置还没填完",
                "html": "<p>你的本地程序已经有进展了，AI 配置还差一点。</p><p>填完以后，我就能帮你少看很多简历。</p>"
            },
            "platform_account": {
                "subject": "GoodHR 平台账号还没创建",
                "html": "<p>现在还没有招聘平台账号，任务没有地方开工。</p><p>先创建一个平台账号，我再继续干活。</p>"
            },
            "position": {
                "subject": "GoodHR 岗位模板还没创建",
                "html": "<p>岗位模板还没创建，我暂时不知道该帮你筛谁。</p><p>这一步大概 10 秒，填完我以后少打扰你。</p>"
            },
            "greet_success": {
                "subject": "GoodHR 还没打招呼成功",
                "html": "<p>我看到你还没完成第一次打招呼成功。</p><p>可能是平台账号、岗位或本地程序还有点小卡点，可以回来检查一下。</p>"
            },
            "paid": {
                "subject": "GoodHR 会员功能可以继续试试",
                "html": "<p>你的基础流程已经走通了，会员功能可以帮你更省时间。</p><p>如果你愿意，可以回来看看订阅方案。</p>"
            }
        }
    }'::jsonb,
    '自动挽回邮件配置',
    true
)
ON CONFLICT (config_key) DO NOTHING;
