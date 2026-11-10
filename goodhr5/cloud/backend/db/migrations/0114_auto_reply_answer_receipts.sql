-- 文件作用：记录每个真实来源会话对一版HR公开答复的领取、发送或人工核对结果。
CREATE TABLE auto_reply_answer_receipts(
 tenant_id uuid NOT NULL,user_id uuid NOT NULL,delivery_id text NOT NULL,question_id uuid NOT NULL REFERENCES auto_reply_cloud_questions(id) ON DELETE CASCADE,
 version integer NOT NULL,kind text NOT NULL DEFAULT 'conversations' CHECK(kind='conversations'),conversation_id text NOT NULL,machine_id text NOT NULL,
 status text NOT NULL CHECK(status IN ('claimed','sent','manual')),outbound_message_key text NOT NULL DEFAULT '',updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,user_id,delivery_id),UNIQUE(tenant_id,user_id,question_id,version,conversation_id),
 FOREIGN KEY(tenant_id,user_id,kind,conversation_id) REFERENCES auto_reply_cloud_entities(tenant_id,user_id,kind,id) ON DELETE CASCADE
);
COMMENT ON TABLE auto_reply_answer_receipts IS '服务器补答收据，同问题和版本在每个相关会话最多确认一次';
COMMENT ON COLUMN auto_reply_answer_receipts.tenant_id IS '来源团队';
COMMENT ON COLUMN auto_reply_answer_receipts.user_id IS '拥有对应招聘会话的成员';
COMMENT ON COLUMN auto_reply_answer_receipts.delivery_id IS '问题、答复版本和会话组合摘要';
COMMENT ON COLUMN auto_reply_answer_receipts.question_id IS 'HR已答复的岗位问题';
COMMENT ON COLUMN auto_reply_answer_receipts.version IS '本次发送对应的答复版本';
COMMENT ON COLUMN auto_reply_answer_receipts.kind IS '外键固定引用会话实体';
COMMENT ON COLUMN auto_reply_answer_receipts.conversation_id IS '收到过这个问题的真实候选人会话';
COMMENT ON COLUMN auto_reply_answer_receipts.machine_id IS '本次领取任务的绑定设备';
COMMENT ON COLUMN auto_reply_answer_receipts.status IS '已领取可恢复、已实际发送、发送不明需人工核对';
COMMENT ON COLUMN auto_reply_answer_receipts.outbound_message_key IS '页面实际回读的己方消息稳定编号';
COMMENT ON COLUMN auto_reply_answer_receipts.updated_at IS '最近领取或完成时间';
