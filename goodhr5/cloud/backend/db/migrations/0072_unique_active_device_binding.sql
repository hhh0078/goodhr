-- 本迁移限制新版稳定设备编号同一时间只能由一个账号占用，超级管理员解绑后允许换绑。

CREATE UNIQUE INDEX IF NOT EXISTS idx_local_agents_unique_active_stable_device
ON local_agents (machine_id)
WHERE bind_status = 'active' AND machine_id LIKE 'goodhr-device-v1-%';

COMMENT ON INDEX idx_local_agents_unique_active_stable_device IS '新版稳定设备编号的有效账号唯一占用约束，管理员解绑后释放设备';
