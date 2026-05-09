-- 添加api_key字段
alter table if exists accounts
  add column if not exists api_key text default '';

-- 添加模型列表系统配置
insert into system_configs (config_key, config_value, description) values
('frontend.models', '[
  {"model_id": "gpt-5.1-chat", "input_price": 1.25, "output_price": 10, "description": "GPT-5.1 - 高性能模型"},
  {"model_id": "deepseek-v3", "input_price": 0.5, "output_price": 2, "description": "DeepSeek V3 - 高性价比"},
  {"model_id": "qwen-plus", "input_price": 0.8, "output_price": 4, "description": "通义千问Plus - 均衡模型"},
  {"model_id": "moonshot-v1", "input_price": 0.3, "output_price": 1.5, "description": "Kimi - 经济实惠"}
]', '可选模型列表'),
('frontend.default_model', '"gpt-5.1-chat"', '默认AI模型'),
('frontend.default_click_prompt', '"你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。\n\n重要提示：\n1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回\"内容与招聘无关 无法解答\"。\n2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。\n3. 必须返回JSON格式，包含decision和reason两个字段。\n4. decision字段只能是\"是\"或\"否\"。\n5. reason字段是决策原因，10个字以内。\n6. 如果岗位要求中包含\"经验\"，则必须考虑候选人的工作经验。\n7. 如果岗位要求中包含\"学历\"，则必须考虑候选人的学历。\n8. 如果候选人信息中没有工作经历。那很可能只是基础信息。这时岗位信息中某个条件、但是候选人信息中没提到的 你应该无视这个条件。\n9. 你应该主动分析 岗位信息是不是属于高要求的岗位、如果是。则你需要详细严格筛选候选人信息。如果是要求低的普通岗位。那就简单筛选\n\n\n岗位要求：\n${岗位信息}\n\n候选人基本信息：\n${候选人信息}\n\n请判断是否值得查看这位候选人的详细信息，返回JSON格式：{\"decision\":\"是\",\"reason\":\"符合基本要求\"}"', '默认查看详情提示语')
on conflict (config_key) do update set config_value = excluded.config_value, description = excluded.description;
