-- 本迁移为后台广告位补充数组配置，最多展示数量由前端限制为 3 个；保留旧 admin_banner 兼容配置。
UPDATE system_configs
SET config_value = config_value::jsonb || jsonb_build_object(
  'admin_banners',
  COALESCE(
    config_value::jsonb->'admin_banners',
    CASE
      WHEN config_value::jsonb ? 'admin_banner' THEN jsonb_build_array(config_value::jsonb->'admin_banner')
      ELSE jsonb_build_array(
        jsonb_build_object(
          'enabled', true,
          'text', 'GoodHR 猎头管理系统已上线（完全免费），点击前往体验。',
          'background_color', '#fff7df',
          'text_color', '#6b4a00',
          'url', 'https://goodhr5.58it.cn'
        )
      )
    END
  )
)
WHERE config_key = 'system.app_config';
