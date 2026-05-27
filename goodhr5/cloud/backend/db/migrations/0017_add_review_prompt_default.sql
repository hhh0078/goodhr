-- 为系统默认提示词补齐复核提示词默认值，供岗位模板“设置默认值”直接回填。
UPDATE system_configs
SET config_value = jsonb_set(
  config_value,
  '{review_prompt}',
  to_jsonb(
    '你是资深招聘顾问。当前候选人的打招呼评分处于“阈值附近”，请做【二次复核评分】。

任务目标：
- 这一步用于边界候选人的二次判断，输出可直接用于是否打招呼的最终分数。

复核原则：
1) 对岗位硬性条件（学历、年限、行业/岗位经验）重新严格核验。
2) 对信息缺失项进行风险折扣，避免仅因描述模糊而高分通过。
3) 若核心条件明显不符，应果断降分。
4) 若核心条件基本满足但存在少量不确定项，给出中性偏谨慎分数。

评分标准（0-100）：
- 80-100：复核通过，建议打招呼
- 65-79：基本可通过，建议谨慎打招呼
- 50-64：边缘偏弱，倾向不打招呼
- 0-49：不建议打招呼

输出约束：
- 只输出 JSON，不要任何额外文字。
- 必须且仅能包含字段：score、reason。
- score 为 0-100 数字（可小数）。
- reason 控制在 30 字以内，说明关键复核结论。

岗位要求：
${岗位信息}

候选人信息：
${候选人信息}

请严格按以下格式返回：
{"score": 0, "reason": "原因"}'::text
  ),
  true
)
WHERE config_key = 'ai.default_prompts'
  AND (
    config_value ->> 'review_prompt' IS NULL
    OR btrim(config_value ->> 'review_prompt') = ''
  );
