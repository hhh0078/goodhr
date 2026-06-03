-- 本迁移确保 Boss 平台存在岗位读取与岗位切换配置。
-- 只补缺失字段，不覆盖管理员已经手动填写的真实选择器。

WITH boss_config AS (
    SELECT
        config_key,
        CASE
            WHEN config_value ? 'position' THEN config_value
            ELSE jsonb_set(config_value, '{position}', '{}'::jsonb, true)
        END AS base_config
    FROM system_configs
    WHERE config_key = 'platform.boss'
      AND enabled = true
)
UPDATE system_configs AS s
SET config_value = jsonb_set(
    jsonb_set(
        jsonb_set(
            jsonb_set(
                jsonb_set(
                    jsonb_set(
                        boss_config.base_config,
                        '{position,current}',
                        COALESCE(
                            boss_config.base_config #> '{position,current}',
                            '{"target_classes":[["当前岗位名称选择器，请改成真实CSS"]]}'::jsonb
                        ),
                        true
                    ),
                    '{position,switchBtn}',
                    COALESCE(
                        boss_config.base_config #> '{position,switchBtn}',
                        '{"target_classes":[["岗位选择按钮选择器，请改成真实CSS"]]}'::jsonb
                    ),
                    true
                ),
                '{position,list}',
                COALESCE(
                    boss_config.base_config #> '{position,list}',
                    '{"target_classes":[["岗位列表容器选择器，请改成真实CSS"]]}'::jsonb
                ),
                true
            ),
            '{position,item}',
            COALESCE(
                boss_config.base_config #> '{position,item}',
                '{"target_classes":[["岗位列表岗位项选择器，请改成真实CSS"]]}'::jsonb
            ),
            true
        ),
        '{position,itemText}',
        COALESCE(
            boss_config.base_config #> '{position,itemText}',
            '{"target_classes":[["岗位列表岗位名称文字选择器，请改成真实CSS"]]}'::jsonb
        ),
        true
    ),
    '{position,clickTarget}',
    COALESCE(
        boss_config.base_config #> '{position,clickTarget}',
        '{"target_classes":[["岗位列表岗位点击目标选择器，请改成真实CSS"]]}'::jsonb
    ),
    true
)
FROM boss_config
WHERE s.config_key = boss_config.config_key
  AND (
    NOT (boss_config.base_config ? 'position')
    OR boss_config.base_config #> '{position,current}' IS NULL
    OR boss_config.base_config #> '{position,switchBtn}' IS NULL
    OR boss_config.base_config #> '{position,list}' IS NULL
    OR boss_config.base_config #> '{position,item}' IS NULL
    OR boss_config.base_config #> '{position,itemText}' IS NULL
    OR boss_config.base_config #> '{position,clickTarget}' IS NULL
  );
