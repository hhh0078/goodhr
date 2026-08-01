-- 本迁移允许有效 Max 直接切换 Plus，并删除岗位累计打招呼字段。

ALTER TABLE positions
    DROP COLUMN IF EXISTS greeted_count;

COMMENT ON COLUMN payment_orders.upgrade_from_member_type IS '套餐切换前的会员类型，例如plus或max；普通续费订单为空';

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{cards}',
        COALESCE(
            (
                SELECT jsonb_agg(
                    CASE
                        WHEN card->>'id' = 'subscription' THEN
                            jsonb_set(
                                card,
                                '{content}',
                                to_jsonb('新用户注册赠送 3 天 Max 全能版。免费版可使用关键词筛选和基础自动打招呼；Plus 基础包月版支持 AI 筛选和 AI 自动打招呼，不支持自动回复；Max 全能包年版开放自动回复。有效 Plus 升级 Max 时会按剩余时间抵扣；有效 Max 也可以直接切换 Plus，原 Max 剩余时间不折算，新套餐从付款时间重新计算。'::text),
                                true
                            )
                        ELSE card
                    END
                    ORDER BY card_index
                )
                FROM jsonb_array_elements(COALESCE(config_value->'cards', '[]'::jsonb))
                     WITH ORDINALITY AS cards(card, card_index)
            ),
            '[]'::jsonb
        ),
        true
    ),
    updated_at = now()
WHERE config_key = 'system.guide';

UPDATE system_configs
SET config_value = jsonb_set(
        config_value,
        '{sections}',
        COALESCE(
            (
                SELECT jsonb_agg(
                    CASE
                        WHEN section->>'id' = 'subscription' THEN
                            jsonb_set(
                                section,
                                '{items}',
                                '[
                                  "新用户注册赠送 3 天 Max 全能版，可以体验包括自动回复在内的全部现有功能。",
                                  "免费版支持关键词筛选和基础自动打招呼；Plus 基础包月版支持 AI 筛选和 AI 自动打招呼；Max 全能包年版额外支持自动回复。",
                                  "Plus 升级 Max 时按 Plus 剩余时间折算抵扣；Max 切换 Plus 时不折算剩余时间，新套餐从付款时间重新计算。",
                                  "开始 AI 岗位和自动回复前，前端、云端和本地程序都会按统一会员权限检查。",
                                  "支付记录用户可查看自己的记录，超级管理员可查看全部记录。"
                                ]'::jsonb,
                                true
                            )
                        ELSE section
                    END
                    ORDER BY section_index
                )
                FROM jsonb_array_elements(COALESCE(config_value->'sections', '[]'::jsonb))
                     WITH ORDINALITY AS sections(section, section_index)
            ),
            '[]'::jsonb
        ),
        true
    ),
    updated_at = now()
WHERE config_key = 'system.guide';
