-- 文件作用：新增账号私有消息、轻量未读计数与永久幂等凭据；已读正文由后台在72小时后清理。
CREATE TABLE IF NOT EXISTS user_message_state (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 unread_count BIGINT NOT NULL DEFAULT 0 CHECK (unread_count >= 0),
 version BIGINT NOT NULL DEFAULT 0,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE user_message_state IS '账号消息未读数量与递增版本，避免轮询扫描消息表';
COMMENT ON COLUMN user_message_state.user_id IS '消息所属账号';
COMMENT ON COLUMN user_message_state.unread_count IS '当前未读消息数量';
COMMENT ON COLUMN user_message_state.version IS '消息新增、首次已读或清理时递增的版本号';
COMMENT ON COLUMN user_message_state.updated_at IS '最近消息变化时间';
CREATE TABLE IF NOT EXISTS messages (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 title TEXT NOT NULL,
 body TEXT NOT NULL,
 message_type TEXT NOT NULL DEFAULT 'system',
 action_url TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 read_at TIMESTAMPTZ
);
COMMENT ON TABLE messages IS '账号私有消息中心；未读消息保留，已读满72小时后删除';
COMMENT ON COLUMN messages.id IS '消息唯一编号';
COMMENT ON COLUMN messages.user_id IS '唯一收件账号，不能跨账号读取';
COMMENT ON COLUMN messages.title IS '消息列表标题';
COMMENT ON COLUMN messages.body IS '展开时读取的纯文本正文';
COMMENT ON COLUMN messages.message_type IS '业务消息类型，例如resume_import或system';
COMMENT ON COLUMN messages.action_url IS 'GoodHR站内业务操作地址';
COMMENT ON COLUMN messages.created_at IS '消息创建时间';
COMMENT ON COLUMN messages.read_at IS '首次展开时间，清理时间从此开始计算';
CREATE INDEX IF NOT EXISTS idx_messages_user_created ON messages(user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_messages_read_cleanup ON messages(read_at) WHERE read_at IS NOT NULL;
CREATE TABLE IF NOT EXISTS user_message_receipts (
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 idempotency_key TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 message_id UUID NOT NULL,
 PRIMARY KEY(user_id, idempotency_key)
);
COMMENT ON TABLE user_message_receipts IS '消息推送幂等凭据，仅存摘要与编号，清理正文后重试也不重复推送';
COMMENT ON COLUMN user_message_receipts.user_id IS '推送目标账号';
COMMENT ON COLUMN user_message_receipts.idempotency_key IS '业务操作稳定幂等编号';
COMMENT ON COLUMN user_message_receipts.fingerprint IS '推送内容SHA256摘要，同编号不同内容拒绝保存';
COMMENT ON COLUMN user_message_receipts.message_id IS '首次成功创建的消息编号，允许正文已清理';
