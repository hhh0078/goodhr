-- 本迁移为系统其它配置补充岗位要求 AI 优化提示词，供岗位模板页面调用用户个人 AI 配置优化岗位要求。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
  'system.app_config',
  jsonb_build_object(
    'local_agent_version', '5.0.0',
    'position_requirement_optimize_prompt',
    '你是一个招聘筛选规则整理助手。请把用户输入的岗位要求整理成适合 AI 筛选候选人简历的规则。

要求：
1. 只保留候选人自身条件，不要保留岗位福利、薪资待遇、工作时间、公司介绍、岗位职责、工作内容。
2. 去掉无法从简历中稳定判断的主观要求，例如：有上进心、责任心强、抗压能力强、沟通能力好、性格开朗、团队意识强、吃苦耐劳等。
3. 优先保留硬性条件，例如：学历、专业、工作年限、行业经验、岗位经验、证书、技能、城市、年龄、到岗状态。
4. 如果原文里有模糊条件，请改写成更清晰的筛选规则。
5. 输出中文，按条目列出，不要解释，不要输出 JSON。

用户输入：
{{input}}',
    'email_domain_whitelist',
    jsonb_build_array('qq.com', 'foxmail.com', '163.com', '126.com', 'yeah.net', 'sina.com', 'sina.cn', 'sohu.com', 'aliyun.com', '139.com', '189.cn', 'wo.cn', 'gmail.com', 'outlook.com', 'hotmail.com', 'live.com', 'icloud.com', 'yahoo.com', 'proton.me', 'protonmail.com'),
    'announcements_enabled', true,
    'announcements', jsonb_build_array()
  ),
  '前端公共系统配置：本地执行器版本要求、邮箱域名白名单、系统公告和岗位要求优化提示词',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = CASE
    WHEN system_configs.config_value::jsonb ? 'position_requirement_optimize_prompt' THEN system_configs.config_value::jsonb
    ELSE jsonb_set(
      system_configs.config_value::jsonb,
      '{position_requirement_optimize_prompt}',
      to_jsonb('你是一个招聘筛选规则整理助手。请把用户输入的岗位要求整理成适合 AI 筛选候选人简历的规则。

要求：
1. 只保留候选人自身条件，不要保留岗位福利、薪资待遇、工作时间、公司介绍、岗位职责、工作内容。
2. 去掉无法从简历中稳定判断的主观要求，例如：有上进心、责任心强、抗压能力强、沟通能力好、性格开朗、团队意识强、吃苦耐劳等。
3. 优先保留硬性条件，例如：学历、专业、工作年限、行业经验、岗位经验、证书、技能、城市、年龄、到岗状态。
4. 如果原文里有模糊条件，请改写成更清晰的筛选规则。
5. 输出中文，按条目列出，不要解释，不要输出 JSON。

用户输入：
{{input}}'::text)
    )
  END,
  description = system_configs.description,
  enabled = system_configs.enabled;
