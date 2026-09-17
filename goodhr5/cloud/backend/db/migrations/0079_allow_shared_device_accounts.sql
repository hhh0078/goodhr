-- 本迁移移除电脑跨账号唯一占用限制，允许同一台电脑依次登录多个 GoodHR 账号。
-- 岗位启动时仍由云端按账号检查是否已有运行中的岗位。
DROP INDEX IF EXISTS idx_local_agents_unique_active_stable_device;
