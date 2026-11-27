-- 文件作用：将新注册赠送天数改为0，并让新建账号按系统配置创建试用期限；已有会员和其他赠送规则保持原值。
UPDATE system_configs
SET config_value=jsonb_set(config_value,'{trial_days}','0'::jsonb,true)
WHERE config_key='system.onboarding_config' AND jsonb_typeof(config_value)='object';
-- registration_subscription_default 读取后台注册赠送天数，仅在创建新账号时计算会员期限。
CREATE FUNCTION registration_subscription_default() RETURNS jsonb LANGUAGE SQL STABLE AS $$
 SELECT jsonb_build_object('member_type','max','expires_at',now()+make_interval(days=>
  COALESCE((SELECT CASE WHEN config_value->>'trial_days' ~ '^[0-9]{1,4}$'
   THEN LEAST((config_value->>'trial_days')::integer,3650) ELSE 0 END
   FROM system_configs WHERE config_key='system.onboarding_config'),0)))
$$;
COMMENT ON FUNCTION registration_subscription_default() IS '新注册账号按系统配置赠送会员天数，缺省0天，不修改已有会员';
ALTER TABLE users ALTER COLUMN subscription SET DEFAULT registration_subscription_default();
