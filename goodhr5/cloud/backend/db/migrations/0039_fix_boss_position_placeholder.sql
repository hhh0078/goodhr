-- 修正 Boss 直聘 position 配置中的占位符选择器为真实值
UPDATE system_configs
SET config_value = jsonb_set(config_value, '{position,itemText}', '{"target_classes":[["label"]]}'::jsonb, true)
WHERE config_key = 'platform.boss'
  AND enabled = true
  AND config_value #> '{position,itemText}' IS NOT NULL
  AND config_value #>> '{position,itemText,target_classes,0,0}' LIKE '%请改成真实CSS%';

UPDATE system_configs
SET config_value = jsonb_set(config_value, '{position,clickTarget}', '{"parent_classes":[["job-list"]],"target_classes":[["job-item"]]}'::jsonb, true)
WHERE config_key = 'platform.boss'
  AND enabled = true
  AND config_value #> '{position,clickTarget}' IS NOT NULL
  AND config_value #>> '{position,clickTarget,target_classes,0,0}' LIKE '%请改成真实CSS%';
