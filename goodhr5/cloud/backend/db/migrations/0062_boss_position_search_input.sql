-- 本迁移为 Boss 岗位切换配置补充岗位搜索输入框。
-- 只在缺少 searchInput 时补默认选择器，不覆盖后台已有配置。
UPDATE system_configs
SET config_value = jsonb_set(
  CASE
    WHEN config_value ? 'position' THEN config_value
    ELSE jsonb_set(config_value, '{position}', '{}'::jsonb, true)
  END,
  '{position,searchInput}',
  COALESCE(
    config_value #> '{position,searchInput}',
    '{"target_classes":[[".ipt.chat-job-search"],["input[placeholder=''请输入职位名称'']"]]}'::jsonb
  ),
  true
)
WHERE config_key = 'platform.boss'
  AND enabled = true;
