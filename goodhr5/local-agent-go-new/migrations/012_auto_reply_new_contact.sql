-- 文件作用：保存平台来源最近一次删除边界，以及只读核实新消息前的短期会话线索。
CREATE TABLE auto_reply_deletion_boundaries (
    owner_id TEXT NOT NULL, -- 已认证的本地账号隔离键
    source_hash TEXT NOT NULL, -- 平台及候选人稳定编号的摘要
    deletion_sequence INTEGER NOT NULL, -- 云端删除事件的递增序号
    deleted_at TEXT NOT NULL, -- 云端核实的删除时间或旧库迁移的保守下限
    PRIMARY KEY(owner_id, source_hash)
) WITHOUT ROWID;
CREATE TABLE auto_reply_contact_probes (
    owner_id TEXT NOT NULL, -- 已认证的本地账号隔离键
    platform_id TEXT NOT NULL, -- 招聘平台
    account_id TEXT NOT NULL, -- HR手动绑定的招聘账号摘要
    thread_id TEXT NOT NULL, -- 已观察到未读的稳定会话编号
    intent_json TEXT NOT NULL, -- 恢复定位所需的列表身份和岗位，不含聊天正文
    PRIMARY KEY(owner_id,platform_id,account_id,thread_id)
) WITHOUT ROWID;
CREATE TABLE auto_reply_deletion_documents (
    owner_id TEXT NOT NULL, -- 本地已认证账号
    event_key TEXT NOT NULL, -- 已确认删除的云候选编号
    sha256 TEXT NOT NULL, -- 原正式附件的内容摘要，无文件名和正文
    PRIMARY KEY(owner_id,event_key,sha256)
) WITHOUT ROWID;
