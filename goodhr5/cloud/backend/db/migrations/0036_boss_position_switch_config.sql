-- 本迁移为 Boss 平台配置增加当前岗位读取与岗位切换选择器占位配置。
-- 占位值使用中文，方便管理员在后台系统配置中替换为真实 CSS 选择器。

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{position}',
    '{
      "current": {
        "target_classes": [["当前岗位名称选择器，请改成真实CSS"]]
      },
      "switchBtn": {
        "target_classes": [["岗位选择按钮选择器，请改成真实CSS"]]
      },
      "list": {
        "target_classes": [["岗位列表容器选择器，请改成真实CSS"]]
      },
      "item": {
        "target_classes": [["岗位列表岗位项选择器，请改成真实CSS"]]
      }
    }'::jsonb,
    true
)
WHERE config_key = 'platform.boss'
  AND enabled = true
  AND NOT (config_value ? 'position');
