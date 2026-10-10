-- 文件作用：持久化每个账号的简历库下载导入开关，未设置的账号默认开启。
CREATE TABLE IF NOT EXISTS resume_library_settings (
 user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 auto_import_downloads BOOLEAN NOT NULL DEFAULT true,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE resume_library_settings IS '账号独立的简历库设置';
COMMENT ON COLUMN resume_library_settings.user_id IS '设置所属账号';
COMMENT ON COLUMN resume_library_settings.auto_import_downloads IS '是否通过AI导入招聘平台下载的简历，默认开启';
COMMENT ON COLUMN resume_library_settings.updated_at IS '设置最近修改时间';
