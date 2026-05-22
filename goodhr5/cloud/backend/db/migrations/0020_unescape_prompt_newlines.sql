-- 将提示词中的字面量 \n 转成真实换行，避免前端输入框显示为单行转义文本。

-- 修复系统默认提示词。
UPDATE system_configs
SET config_value = jsonb_set(
  jsonb_set(
    jsonb_set(
      config_value,
      '{filter_prompt}',
      to_jsonb(replace(coalesce(config_value->>'filter_prompt', ''), '\n', E'\n')),
      true
    ),
    '{open_detail_prompt}',
    to_jsonb(replace(coalesce(config_value->>'open_detail_prompt', ''), '\n', E'\n')),
    true
  ),
  '{review_prompt}',
  to_jsonb(replace(coalesce(config_value->>'review_prompt', ''), '\n', E'\n')),
  true
)
WHERE config_key = 'ai.default_prompts';

-- 修复岗位模板提示词。
UPDATE positions
SET ai_config = jsonb_set(
  jsonb_set(
    jsonb_set(
      jsonb_set(
        jsonb_set(
          ai_config,
          '{filter_prompt}',
          to_jsonb(replace(coalesce(ai_config->>'filter_prompt', ''), '\n', E'\n')),
          true
        ),
        '{open_detail_prompt}',
        to_jsonb(replace(coalesce(ai_config->>'open_detail_prompt', ''), '\n', E'\n')),
        true
      ),
      '{review_prompt}',
      to_jsonb(replace(coalesce(ai_config->>'review_prompt', ''), '\n', E'\n')),
      true
    ),
    '{greet_prompt}',
    to_jsonb(replace(coalesce(ai_config->>'greet_prompt', ''), '\n', E'\n')),
    true
  ),
  '{click_prompt}',
  to_jsonb(replace(coalesce(ai_config->>'click_prompt', ''), '\n', E'\n')),
  true
)
WHERE
  coalesce(ai_config->>'filter_prompt', '') LIKE '%\\n%'
  OR coalesce(ai_config->>'open_detail_prompt', '') LIKE '%\\n%'
  OR coalesce(ai_config->>'review_prompt', '') LIKE '%\\n%'
  OR coalesce(ai_config->>'greet_prompt', '') LIKE '%\\n%'
  OR coalesce(ai_config->>'click_prompt', '') LIKE '%\\n%';
