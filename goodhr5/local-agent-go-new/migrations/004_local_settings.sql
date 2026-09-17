-- 文件作用说明：保存本地程序的键值设置，例如用户切换过的下载目录，重启后依然生效。

CREATE TABLE IF NOT EXISTS local_settings (
    key TEXT PRIMARY KEY,          -- 设置项名称，例如 download_roots
    value TEXT NOT NULL DEFAULT '', -- 设置项内容，多值时使用换行分隔
    updated_at TEXT NOT NULL        -- 最近一次修改时间
);
