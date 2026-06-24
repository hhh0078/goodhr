-- 本迁移为用户增加邮件通知画像，用于按身份、性别、系统和常用平台精准发送更新通知。
ALTER TABLE users
ADD COLUMN IF NOT EXISTS notification_profile JSONB NOT NULL DEFAULT jsonb_build_object(
    'completed', false,
    'dismissed_at', null,
    'user_type', '',
    'gender', 'female',
    'platforms', '[]'::jsonb,
    'os', '',
    'browser', '',
    'updated_at', null
);

COMMENT ON COLUMN users.notification_profile IS '用户邮件通知画像JSON，包含是否完成、身份类型、性别、常用招聘平台、电脑系统和浏览器';
