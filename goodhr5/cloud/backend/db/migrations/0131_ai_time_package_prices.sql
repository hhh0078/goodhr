-- 文件作用：将时间套餐月/季/年新订单价格调整为69/179/549元；保留天数、999元内部额度、其他配置、旧订单及已购权益。
UPDATE system_configs c
SET config_value = COALESCE((
    SELECT jsonb_agg(
        CASE item->>'id'
            WHEN 'monthly' THEN jsonb_set(item, '{price_cents}', '6900'::jsonb, true)
            WHEN 'quarterly' THEN jsonb_set(item, '{price_cents}', '17900'::jsonb, true)
            WHEN 'yearly' THEN jsonb_set(item, '{price_cents}', '54900'::jsonb, true)
            ELSE item
        END ORDER BY item_order
    )
    FROM jsonb_array_elements(c.config_value) WITH ORDINALITY AS plans(item, item_order)
), '[]'::jsonb)
WHERE c.config_key = 'system.ai_time_plans'
  AND jsonb_typeof(c.config_value) = 'array';
