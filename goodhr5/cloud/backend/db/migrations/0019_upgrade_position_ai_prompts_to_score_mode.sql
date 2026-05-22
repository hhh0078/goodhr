-- 将岗位模板中遗留的布尔决策提示词升级为评分模式提示词。
UPDATE positions
SET ai_config = jsonb_set(
  jsonb_set(
    ai_config,
    '{open_detail_prompt}',
    to_jsonb(
      '你是一个资深的HR专家。请根据岗位要求给候选人打“查看详情建议分”。\n\n重要提示：\n1. 仅根据候选人基础信息评估是否值得打开详情。\n2. 仅输出 JSON，不能输出其它内容。\n3. 返回字段必须是 score 和 reason。\n4. score 范围是 0-100，可以是小数。\n5. reason 控制在30字以内。\n\n岗位要求：\n${岗位信息}\n\n候选人基础信息：\n${候选人信息}\n\n请返回JSON：{"score": 66, "reason": "可进一步确认细节"}'::text
    ),
    true
  ),
  '{filter_prompt}',
  to_jsonb(
    '你是一个资深的HR专家。请根据岗位要求给候选人打“打招呼建议分”。\n\n重要提示：\n1. 仅输出 JSON，不能输出其它内容。\n2. 返回字段必须是 score 和 reason。\n3. score 范围是 0-100，可以是小数。\n4. reason 控制在30字以内。\n5. 如果岗位要求中包含经验、学历、行业等硬条件，必须重点评估。\n\n岗位要求：\n${岗位信息}\n\n候选人信息：\n${候选人信息}\n\n请返回JSON：{"score": 78, "reason": "匹配核心要求"}'::text
  ),
  true
)
WHERE
  coalesce(ai_config->>'open_detail_prompt', '') LIKE '%should_open_detail%'
  OR coalesce(ai_config->>'filter_prompt', '') LIKE '%isok%'
  OR coalesce(ai_config->>'click_prompt', '') LIKE '%isok%'
  OR coalesce(ai_config->>'greet_prompt', '') LIKE '%isok%';

-- 兼容旧字段：把 click_prompt 同步成分数版打招呼提示词。
UPDATE positions
SET ai_config = jsonb_set(
  ai_config,
  '{click_prompt}',
  to_jsonb(coalesce(ai_config->>'filter_prompt', ai_config->>'greet_prompt', '')::text),
  true
)
WHERE
  coalesce(ai_config->>'click_prompt', '') = ''
  OR coalesce(ai_config->>'click_prompt', '') LIKE '%isok%';

-- 新字段补齐：把 greet_prompt 同步成分数版打招呼提示词。
UPDATE positions
SET ai_config = jsonb_set(
  ai_config,
  '{greet_prompt}',
  to_jsonb(coalesce(ai_config->>'filter_prompt', ai_config->>'click_prompt', '')::text),
  true
)
WHERE
  coalesce(ai_config->>'greet_prompt', '') = ''
  OR coalesce(ai_config->>'greet_prompt', '') LIKE '%isok%';
