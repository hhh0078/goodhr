-- 本迁移为用户增加订阅信息，并写入默认订阅套餐系统配置。
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS subscription JSONB NOT NULL DEFAULT jsonb_build_object(
        'member_type', 'plus',
        'expires_at', now() + interval '3 days'
    );

COMMENT ON COLUMN users.subscription IS '用户订阅信息JSON，包含member_type会员类型和expires_at到期时间';

UPDATE users
SET subscription = jsonb_build_object(
    'member_type', COALESCE(NULLIF(subscription->>'member_type', ''), 'plus'),
    'expires_at', COALESCE(NULLIF(subscription->>'expires_at', ''), (created_at + interval '3 days')::text)
)
WHERE subscription IS NULL
   OR subscription = '{}'::jsonb
   OR subscription->>'expires_at' IS NULL
   OR subscription->>'member_type' IS NULL;

INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.subscription_plans',
  '[
    {
      "id": "monthly",
      "name": "按月订阅",
      "member_type": "plus",
      "duration_days": 30,
      "original_price": 70,
      "discount_amount": 0,
      "features": ["Plus会员权益", "任务启动权限", "本地执行器联动"],
      "description": "适合短期招聘任务使用，按月开通Plus会员。",
      "created_at": "2026-05-26"
    },
    {
      "id": "quarterly",
      "name": "按季度订阅",
      "member_type": "plus",
      "duration_days": 90,
      "original_price": 210,
      "discount_amount": 30,
      "features": ["Plus会员权益", "任务启动权限", "本地执行器联动", "季度优惠"],
      "description": "适合连续招聘使用，季度订阅原价210元，优惠30元。",
      "created_at": "2026-05-26"
    },
    {
      "id": "yearly",
      "name": "按年订阅",
      "member_type": "plus",
      "duration_days": 365,
      "original_price": 840,
      "discount_amount": 240,
      "features": ["Plus会员权益", "任务启动权限", "本地执行器联动", "年度优惠"],
      "description": "适合长期招聘团队使用，年度订阅原价840元，优惠240元。",
      "created_at": "2026-05-26"
    }
  ]'::jsonb,
  '订阅套餐配置，供前端订阅页面展示',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled;
