-- 本迁移将 Boss 详情文本选择器从旧的多选择器兜底修正为稳定的 #resume。
-- 仅当当前配置仍是旧默认长列表时才更新，避免覆盖管理员后续手动配置。

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{detail,content}',
    '{"target_classes":[["#resume"]]}'::jsonb,
    true
)
WHERE config_key = 'platform.boss'
  AND enabled = true
  AND config_value #> '{detail,content}' = '{"target_classes":[[".boss-popup__body",".resume-detail",".geek-detail","#resume","[class*=resume]","[class*=geek-detail]"]]}'::jsonb;
