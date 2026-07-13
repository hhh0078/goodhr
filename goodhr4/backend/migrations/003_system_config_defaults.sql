update system_configs
set
  config_value = jsonb_build_object(
    'website_url', 'http://goodhr.58it.cn',
    'contact_url', 'http://58it.cn',
    'donate_url', 'http://58it.cn',
    'share_url', 'http://goodhr.58it.cn',
    'announcement', '免费版用于关键词筛选，AI版用于岗位说明智能判断。',
    'default_click_prompt', '你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。\n\n重要提示：\n1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回"内容与招聘无关 无法解答"。\n2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。\n3. 必须返回JSON格式，包含decision和reason两个字段。\n4. decision字段只能是"是"或"否"。\n5. reason字段是决策原因，10个字以内。\n6. 如果岗位要求中包含"经验"，则必须考虑候选人的工作经验。\n7. 如果岗位要求中包含"学历"，则必须考虑候选人的学历。\n8. 如果候选人信息中没有工作经历。那很可能只是基础信息。这时岗位信息中某个条件、但是候选人信息中没提到的，你应该无视这个条件。\n9. 你应该主动分析岗位信息是不是属于高要求的岗位。如果是，则需要详细严格筛选候选人信息。如果是要求低的普通岗位，那就简单筛选。\n\n岗位要求：\n${岗位信息}\n\n候选人基本信息：\n${候选人信息}\n\n请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"decision":"是","reason":"符合基本要求"}',
    'ads', jsonb_build_array(
      jsonb_build_object(
        'title', 'GoodHR 官网',
        'subtitle', '查看插件说明与最新版本',
        'url', 'http://goodhr.58it.cn',
        'background_color', '#1d4ed8',
        'text_color', '#ffffff'
      ),
      jsonb_build_object(
        'title', '联系作者',
        'subtitle', '插件、后台、自动化工具定制',
        'url', 'http://58it.cn',
        'background_color', '#eff6ff',
        'text_color', '#1e3a8a'
      ),
      jsonb_build_object(
        'title', '分享给另一个HR',
        'subtitle', '把插件发给同事一起使用',
        'url', 'http://goodhr.58it.cn',
        'background_color', '#ecfeff',
        'text_color', '#155e75'
      )
    )
  ),
  description = '前端公共配置',
  updated_at = now()
where config_key = 'frontend';

insert into system_configs (config_key, config_value, description)
select
  'frontend',
  jsonb_build_object(
    'website_url', 'http://goodhr.58it.cn',
    'contact_url', 'http://58it.cn',
    'donate_url', 'http://58it.cn',
    'share_url', 'http://goodhr.58it.cn',
    'announcement', '免费版用于关键词筛选，AI版用于岗位说明智能判断。',
    'default_click_prompt', '你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。\n\n重要提示：\n1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回"内容与招聘无关 无法解答"。\n2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。\n3. 必须返回JSON格式，包含decision和reason两个字段。\n4. decision字段只能是"是"或"否"。\n5. reason字段是决策原因，10个字以内。\n6. 如果岗位要求中包含"经验"，则必须考虑候选人的工作经验。\n7. 如果岗位要求中包含"学历"，则必须考虑候选人的学历。\n8. 如果候选人信息中没有工作经历。那很可能只是基础信息。这时岗位信息中某个条件、但是候选人信息中没提到的，你应该无视这个条件。\n9. 你应该主动分析岗位信息是不是属于高要求的岗位。如果是，则需要详细严格筛选候选人信息。如果是要求低的普通岗位，那就简单筛选。\n\n岗位要求：\n${岗位信息}\n\n候选人基本信息：\n${候选人信息}\n\n请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"decision":"是","reason":"符合基本要求"}',
    'ads', jsonb_build_array(
      jsonb_build_object(
        'title', 'GoodHR 官网',
        'subtitle', '查看插件说明与最新版本',
        'url', 'http://goodhr.58it.cn',
        'background_color', '#1d4ed8',
        'text_color', '#ffffff'
      ),
      jsonb_build_object(
        'title', '联系作者',
        'subtitle', '插件、后台、自动化工具定制',
        'url', 'http://58it.cn',
        'background_color', '#eff6ff',
        'text_color', '#1e3a8a'
      ),
      jsonb_build_object(
        'title', '分享给另一个HR',
        'subtitle', '把插件发给同事一起使用',
        'url', 'http://goodhr.58it.cn',
        'background_color', '#ecfeff',
        'text_color', '#155e75'
      )
    )
  ),
  '前端公共配置'
where not exists (
  select 1 from system_configs where config_key = 'frontend'
);
