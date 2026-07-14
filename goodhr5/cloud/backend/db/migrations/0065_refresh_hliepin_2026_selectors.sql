-- 刷新 2026 版猎聘猎头端选择器：候选人改为表格行，详情使用新页面。
UPDATE system_configs
SET config_value = jsonb_set(
        jsonb_set(
          jsonb_set(
            jsonb_set(
              config_value,
              '{card,item}',
              '{"selector":"tbody tr"}'::jsonb,
              true
            ),
            '{detail,openTarget}',
            '{"selector":"a"}'::jsonb,
            true
          ),
          '{detail,content}',
          '{"selector":"body"}'::jsonb,
          true
        ),
        '{actions,greetBtn}',
        '{"selector":"button"}'::jsonb,
        true
      ),
    updated_at = now()
WHERE config_key = 'platform.hliepin';
