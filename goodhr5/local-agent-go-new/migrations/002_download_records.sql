-- 文件作用说明：保存浏览器下载结果，供本地程序去重提示和查询历史记录，不保存 Cookie、Token 或页面正文。

CREATE TABLE IF NOT EXISTS download_records (
    id TEXT PRIMARY KEY,                         -- Worker 生成的下载记录编号
    url TEXT NOT NULL DEFAULT '',               -- 下载来源地址
    page_url TEXT NOT NULL DEFAULT '',          -- 触发下载的页面地址
    file_path TEXT NOT NULL DEFAULT '',         -- 本机最终保存路径
    file_name TEXT NOT NULL DEFAULT '',         -- 最终文件名
    suggested_filename TEXT NOT NULL DEFAULT '', -- 浏览器建议文件名
    size INTEGER NOT NULL DEFAULT 0,             -- 文件字节数
    status TEXT NOT NULL,                        -- saved 或 failed 下载状态
    error TEXT NOT NULL DEFAULT '',              -- 下载失败原因
    created_at TEXT NOT NULL,                    -- Worker 捕获下载的时间
    updated_at TEXT NOT NULL                     -- 本地记录更新时间
);

-- 按下载时间倒序查询历史记录使用的索引。
CREATE INDEX IF NOT EXISTS idx_download_records_created_at
    ON download_records(created_at DESC);
