-- 文件作用：保存全部联系人列表的账号级扫描时刻及每个线程的匿名变更线索，不保存列表正文或人员资料。
CREATE TABLE auto_reply_list_scopes (
    owner_id TEXT NOT NULL, -- 已认证的 GoodHR 账号隔离键
    platform_id TEXT NOT NULL, -- 招聘平台
    account_id TEXT NOT NULL, -- 平台招聘账号
    scope_hash TEXT NOT NULL, -- 空串为全部联系人，其他值为限定线程摘要
    scanned_ns INTEGER NOT NULL, -- 上一份成功列表快照时间，UTC Unix 纳秒
    PRIMARY KEY(owner_id,platform_id,account_id,scope_hash)
) WITHOUT ROWID;
CREATE TABLE auto_reply_list_rows (
    owner_id TEXT NOT NULL, -- 已认证的 GoodHR 账号隔离键
    platform_id TEXT NOT NULL, -- 招聘平台
    account_id TEXT NOT NULL, -- 平台招聘账号
    scope_hash TEXT NOT NULL, -- 普通或单会话模式的隔离范围
    thread_hash TEXT NOT NULL, -- 平台线程号摘要
    signature TEXT NOT NULL, -- 本行可见属性的摘要
    preview_digest TEXT NOT NULL, -- 当前最后一条可见预览的匿名摘要
    witness_digest TEXT NOT NULL, -- 本轮确有变化的预览摘要
    revision INTEGER NOT NULL, -- 观察版本，防止旧确认清除新待办
    pending INTEGER NOT NULL, -- 是否需要打开聊天核对
    after_ns INTEGER NOT NULL, -- 待办的最早可信时间，零表示使用已采集的聊天游标
    checked_ns INTEGER NOT NULL, -- 上次核对时间，供相同摘要轮转检查使用
    PRIMARY KEY(owner_id,platform_id,account_id,scope_hash,thread_hash)
) WITHOUT ROWID;
CREATE INDEX idx_ar_list_purge ON auto_reply_list_rows(owner_id,platform_id,account_id,thread_hash);
