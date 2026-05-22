-- 将系统默认提示词升级为评分模式，统一要求返回 score/reason JSON。
UPDATE system_configs
SET config_value = jsonb_set(
  jsonb_set(
    jsonb_set(
      config_value,
      '{filter_prompt}',
      to_jsonb(
        '你是一个资深的HR专家。请根据岗位要求给候选人打“打招呼建议分”。\n\n重要提示：\n1. 仅输出 JSON，不能输出其它内容。\n2. 返回字段必须是 score 和 reason。\n3. score 范围是 0-100，可以是小数。\n4. reason 控制在30字以内。\n5. 如果岗位要求中包含经验、学历、行业等硬条件，必须重点评估。\n\n岗位要求：\n${岗位信息}\n\n候选人信息：\n${候选人信息}\n\n请返回JSON：{"score": 78, "reason": "匹配核心要求"}'::text
      ),
      true
    ),
    '{open_detail_prompt}',
    to_jsonb(
      '你是资深招聘顾问。请根据“岗位要求”和“候选人基础信息”给出【查看详情建议分】。\n\n目标：\n- 在存在潜在匹配可能时，优先建议打开详情进一步确认。\n\n评分规则（0-100）：\n- 75-100：建议打开详情\n- 55-74：有潜力，建议打开详情核验\n- 35-54：匹配较弱，可酌情打开\n- 0-34：明显不匹配，不建议打开\n\n宽松要求：\n1) 经验方向接近、能力可迁移可加分\n2) 信息不完整时可给“待核验”加分，不直接判死\n3) 核心条件未明确冲突时，保留进一步查看空间\n4) 对普通岗位适度放宽，对高要求岗位适度收紧\n\n输出约束：\n- 只输出 JSON，不要任何额外文字\n- 仅包含字段：score、reason\n- score 为 0-100 数字（可小数）\n- reason 30 字以内\n\n岗位要求：\n${岗位信息}\n\n候选人基础信息：\n${候选人信息}\n\n返回：\n{"score": 0, "reason": "原因"}'::text
    ),
    true
  ),
  '{review_prompt}',
  to_jsonb(
    '你是一个资深的HR专家。当前候选人分数接近岗位阈值，请做打招呼前二次复核评分。\n\n重要提示：\n1. 仅输出 JSON，不能输出其它内容。\n2. 返回字段必须是 score 和 reason。\n3. score 范围是 0-100，可以是小数。\n4. reason 控制在30字以内。\n5. 评分更关注风险点与关键硬指标。\n\n岗位要求：\n${岗位信息}\n\n候选人信息：\n${候选人信息}\n\n请返回JSON：{"score": 72, "reason": "边界候选人可谨慎通过"}'::text
  ),
  true
)
WHERE config_key = 'ai.default_prompts';
