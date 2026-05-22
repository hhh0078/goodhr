-- 补充打开详情提示词、详情文本定位配置和个人详情查看概率。

ALTER TABLE user_preferences
ADD COLUMN IF NOT EXISTS detail_open_probability INTEGER NOT NULL DEFAULT 30;

COMMENT ON COLUMN user_preferences.detail_open_probability IS '关键词模式下打开详情的个人概率(0-100)';

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{detail,content}',
    '{"target_classes":[[".boss-popup__body",".resume-detail",".geek-detail","#resume","[class*=resume]","[class*=geek-detail]"]]}'::jsonb,
    true
)
WHERE config_key = 'platform.boss'
  AND enabled = true;

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{behavior,needsDetailPage}',
    'true'::jsonb,
    true
)
WHERE config_key = 'platform.boss'
  AND enabled = true;

UPDATE system_configs
SET config_value = jsonb_set(
    config_value,
    '{detail,content}',
    '{"target_classes":[[".new-resume-detail--inner",".km-scrollbar__view",".km-scrollbar__wrap",".new-shortcut-resume--wrapper"]]}'::jsonb,
    true
)
WHERE config_key = 'platform.zhaopin'
  AND enabled = true;
