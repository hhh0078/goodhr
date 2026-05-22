-- 为系统默认提示词补齐复核提示词默认值，供岗位模板“设置默认值”直接回填。
UPDATE system_configs
SET config_value = jsonb_set(
  config_value,
  '{review_prompt}',
  to_jsonb(
    '你是一个资深的HR专家。当前候选人分数接近岗位阈值，请做打招呼前二次复核评分。

重要提示：
1. 仅输出 JSON，不能输出其它内容。
2. 返回字段必须是 score 和 reason。
3. score 范围是 0-100，可以是小数。
4. reason 控制在30字以内。
5. 评分更关注风险点与关键硬指标。

岗位要求：
${岗位信息}

候选人信息：
${候选人信息}

请返回JSON：{"score": 72, "reason": "边界候选人可谨慎通过"}'::text
  ),
  true
)
WHERE config_key = 'ai.default_prompts'
  AND (
    config_value ->> 'review_prompt' IS NULL
    OR btrim(config_value ->> 'review_prompt') = ''
  );
