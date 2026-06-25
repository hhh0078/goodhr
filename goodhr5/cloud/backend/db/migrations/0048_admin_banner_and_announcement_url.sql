-- 本迁移为公共系统配置补充公告跳转链接和后台首页常驻广告位；仅写入缺省值，不覆盖管理员已有配置。
UPDATE system_configs
SET config_value =
  jsonb_set(
    config_value::jsonb ||
      jsonb_build_object(
        'admin_banner',
        COALESCE(
          config_value::jsonb->'admin_banner',
          jsonb_build_object(
            'enabled', true,
            'text', 'GoodHR 猎头管理系统已上线（完全免费），点击前往体验。',
            'background_color', '#fff7df',
            'text_color', '#6b4a00',
            'url', 'https://goodhr5.58it.cn'
          )
        )
      ),
    '{announcements}',
    COALESCE(
      (
        SELECT jsonb_agg(
          CASE
            WHEN item ? 'url' THEN item
            ELSE item || jsonb_build_object('url', '')
          END
        )
        FROM jsonb_array_elements(COALESCE(config_value::jsonb->'announcements', '[]'::jsonb)) AS item
      ),
      '[]'::jsonb
    )
  )
WHERE config_key = 'system.app_config';
