-- 文件作用说明：为用户增加密码登录所需的密码摘要字段，已有用户保持为空并在下次验证码登录后补设密码。
ALTER TABLE users
ADD COLUMN IF NOT EXISTS password_hash TEXT;

COMMENT ON COLUMN users.password_hash IS '用户密码的 bcrypt 摘要；历史账号为空时必须在验证码登录后设置密码';
