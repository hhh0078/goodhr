-- 本文件用于回滚 GoodHR 5 云端数据库初始表结构。
-- 回滚顺序与外键依赖相反，避免删除表时出现依赖错误。

DROP TABLE IF EXISTS task_logs;
DROP TABLE IF EXISTS task_runs;
DROP TABLE IF EXISTS user_ai_configs;
DROP TABLE IF EXISTS positions;
DROP TABLE IF EXISTS platform_accounts;
DROP TABLE IF EXISTS local_agents;
DROP TABLE IF EXISTS users;
