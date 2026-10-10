-- 本文件将关注赠送会员天数加入注册赠送所在的配置组，默认两天；保留全部旧配置与已入账权益。
BEGIN;

-- wechat_follow_reward_days 表示首次公众号关注绑定赠送天数，后台要求 1 至 365 的整数。
-- trial_days、local_agent、runtime_components 及其他字段均保留原值，不改变注册试用与下载更新逻辑。
UPDATE system_configs
SET config_value = config_value || jsonb_build_object('wechat_follow_reward_days', 2),
    description = CASE
        WHEN description LIKE '%关注赠送%' THEN description
        ELSE COALESCE(description, '') || '；关注赠送会员天数由 wechat_follow_reward_days 配置'
    END
WHERE config_key = 'system.onboarding_config'
  AND jsonb_typeof(config_value) = 'object'
  AND NOT (config_value ? 'wechat_follow_reward_days');

COMMIT;
