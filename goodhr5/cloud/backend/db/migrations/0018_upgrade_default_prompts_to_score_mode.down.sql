-- 回滚评分模式默认提示词到旧版布尔决策文案。
UPDATE system_configs
SET config_value = jsonb_set(
  jsonb_set(
    config_value,
    '{filter_prompt}',
    to_jsonb(
      '你是一个资深的HR专家。请根据岗位要求判断候选人是否值得继续沟通。\n\n重要提示：\n1. 这个API仅用于岗位与候选人的筛选。\n2. 请根据岗位要求判断候选人是否值得继续沟通。\n3. 必须返回JSON格式，包含isok和msg两个字段。\n4. isok字段只能是true或false。\n5. msg字段是决策原因，10个字以内。\n\n岗位要求：\n${岗位信息}\n\n候选人基本信息：\n${候选人信息}\n\n请判断是否值得继续沟通，返回JSON格式：{"isok": true, "msg": "符合基本要求"}'::text
    ),
    true
  ),
  '{open_detail_prompt}',
  to_jsonb(
    '你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。\n\n重要提示：\n1. 这个API仅用于岗位与候选人的筛选。\n2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。\n3. 必须返回JSON格式，包含should_open_detail和reason两个字段。\n4. should_open_detail字段只能是true或false。\n5. reason字段是决策原因，20个字以内。\n\n岗位要求：\n${岗位信息}\n\n候选人基本信息：\n${候选人信息}\n\n请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"should_open_detail": true, "reason": "符合基本要求"}'::text
  ),
  true
)
WHERE config_key = 'ai.default_prompts';
