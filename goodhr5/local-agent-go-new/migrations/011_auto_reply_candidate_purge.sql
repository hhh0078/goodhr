-- 文件作用说明：按已认证账号阻止已删简历的旧任务回写，保存尚未完成的受管附件清理意图。
CREATE TABLE auto_reply_purge_keys (
    owner_id TEXT NOT NULL, -- 已认证账号的本地隔离键
    entity_key TEXT NOT NULL, -- 已删除实体的种类与稳定编号，不保存姓名、手机号或简历正文
    PRIMARY KEY(owner_id, entity_key)
) WITHOUT ROWID;
CREATE TABLE auto_reply_purge_files (
    owner_id TEXT NOT NULL, -- 附件归属的本地账号
    event_key TEXT NOT NULL, -- 当前清理事件归属的云端或本地简历编号
    path TEXT NOT NULL, -- 自动回复私有目录下的相对路径
    sha256 TEXT NOT NULL, -- 受管副本原始内容摘要
    PRIMARY KEY(owner_id,event_key,path)
) WITHOUT ROWID;
