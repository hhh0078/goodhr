insert into system_configs (config_key, config_value, description)
values (
  'frontend.update_info',
  jsonb_build_object(
    'version', '4.1.0',
    'content', '优化配置结构与广告位展示。',
    'force_update', false
  ),
  '前端更新信息'
)
on conflict (config_key) do update set
  config_value = excluded.config_value,
  description = excluded.description,
  updated_at = now();
